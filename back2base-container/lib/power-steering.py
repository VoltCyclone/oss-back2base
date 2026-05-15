#!/usr/bin/env python3
"""back2base power-steering watcher daemon.

Polls the active Claude Code session JSONL under
~/.claude/projects/$MEMORY_NAMESPACE. When N new tool calls have landed
since the last check, calls the Anthropic Messages API with a fixed
supervisor prompt against a sliding window of recent transcript turns.
If the supervisor reports drift, writes the finding to
/run/back2base/power-steering/pending.md; the UserPromptSubmit hook injects
that content as a system-reminder on the next user turn and truncates
the file.

Designed as a sibling to session-snapshot — same shape, same log
convention. Stdlib only (no anthropic SDK dependency).

Configuration (env vars; sensible defaults):
  BACK2BASE_POWER_STEERING  "off" / "0" / "false" disables the daemon
  POWER_STEERING_POLL_SEC   poll interval in seconds (default 10)
  POWER_STEERING_EVERY_N_TOOLS  trigger every N new tool calls (default 5)
  POWER_STEERING_WINDOW_TURNS   transcript entries to send (default 20)
  POWER_STEERING_MODEL      Anthropic model id (default claude-haiku-4-5)
  POWER_STEERING_MAX_TOKENS supervisor max output (default 4096). Must
                            exceed POWER_STEERING_THINKING_BUDGET when
                            extended thinking is on; the visible output
                            budget is (max_tokens - thinking_budget).
  POWER_STEERING_THINKING_BUDGET  extended-thinking budget in tokens
                            (default 2048; 0 disables). When > 0 and
                            strictly less than MAX_TOKENS, the daemon
                            sends a `thinking` block so the supervisor
                            can reason before issuing its verdict.
  POWER_STEERING_PROMPT     path to prompt template
                            (default /opt/back2base/power-steering-prompt.md)
  POWER_STEERING_MIN_SLEEP  floor on supervisor-suggested sleep (default 60)
  POWER_STEERING_MAX_SLEEP  ceiling on supervisor-suggested sleep (default 1800)
  POWER_STEERING_CACHE_TTL  local-cache TTL seconds (default 300; matches
                            Anthropic's ephemeral prompt-cache TTL)
  POWER_STEERING_CACHE_MAX  max local-cache entries (default 20)
  POWER_STEERING_CACHE      "off" disables the local cache entirely

Auth (mirrors what Claude Code itself sends; daemon idles if no creds):

  Anthropic credential (forwarded directly, since the OSS build has no
  proxy in front of api.anthropic.com):
  pass-through mode, or used directly when no proxy):
    ANTHROPIC_API_KEY        → x-api-key: <key>
    ANTHROPIC_AUTH_TOKEN     → Authorization: Bearer <token>
    CLAUDE_CODE_OAUTH_TOKEN  → Authorization: Bearer <token>
  Each name is forwarded into the container from a BACK2BASE_<NAME>
  host-side variable by docker-compose; the daemon reads the
  unprefixed name as it appears in its own process env.

Routing:
  ANTHROPIC_BASE_URL is honored. The host-side
  BACK2BASE_ANTHROPIC_BASE_URL is the only source — compose forwards
  it as the unprefixed name. Defaults to api.anthropic.com when unset.

Runtime overrides (hot-reloaded each poll):
  ~/.claude/power-steering/config.json may set any of {model,
  max_tokens, thinking_budget, every_n_tools, window_turns, min_sleep,
  max_sleep}. File values win over env defaults; the /power-steering
  slash command edits this file. Missing/malformed file falls back to
  env silently so a typo can't take the daemon offline.

Required env: MEMORY_NAMESPACE (daemon idles cleanly if absent).
Optional env: CLAUDE_PROJECT_DIR — absolute path to Claude Code's
  project dir (where session JSONLs live). Exported by the entrypoint;
  the daemon falls back to ~/.claude/projects/<MEMORY_NAMESPACE> when
  unset, which only finds JSONLs if the user has built scaffolding to
  bridge the namespace and the cwd-slug Claude Code actually uses.
"""

import hashlib
import json
import os
import re
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

# Identifies this daemon in OTel spans (when an endpoint is configured).
# Bumped only when the daemon's request shape or telemetry schema changes.
TELEMETRY_SOURCE = "power-steering"
TELEMETRY_VERSION = "1"

# One UUID per daemon process. Lets us correlate every drift check from
# a single power-steering invocation in observability — even across
# transcript boundaries within the same container lifetime. Tests pin
# this via BACK2BASE_TELEMETRY_SESSION_ID for deterministic assertions.
SESSION_ID = (os.environ.get("BACK2BASE_TELEMETRY_SESSION_ID", "").strip()
              or uuid.uuid4().hex)

# OTel: optional. Set BACK2BASE_OTEL_ENDPOINT to an OTLP/HTTP collector URL
# to enable. Optional mTLS via BACK2BASE_OTEL_CERT / BACK2BASE_OTEL_KEY.
# Disabled gracefully when opentelemetry packages aren't installed, when
# no endpoint is set, or when BACK2BASE_OTEL=off.
OTEL_CERT_PATH = Path(os.environ.get("BACK2BASE_OTEL_CERT", ""))
OTEL_KEY_PATH = Path(os.environ.get("BACK2BASE_OTEL_KEY", ""))
OTEL_ENDPOINT = os.environ.get("BACK2BASE_OTEL_ENDPOINT", "")
_TELEMETRY_INITIALIZED = False


# Stub tracer/span objects used when the SDK isn't available. The OTel
# API ships its own NoOp implementations, but using ours keeps this
# daemon importable on a base image that doesn't have opentelemetry-api
# installed yet (which is the case until the v0.23.5 base-image rebuild
# rolls out).
class _NoOpSpan:
    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False

    def set_attribute(self, *a, **kw):
        pass

    def set_attributes(self, *a, **kw):
        pass

    def add_event(self, *a, **kw):
        pass

    def record_exception(self, *a, **kw):
        pass


class _NoOpTracer:
    def start_as_current_span(self, *a, **kw):
        return _NoOpSpan()


_NOOP_TRACER = _NoOpTracer()


def _telemetry_disabled():
    return os.environ.get("BACK2BASE_OTEL", "").lower() in ("off", "0", "false", "no")


def init_telemetry():
    """Configure the OTel tracer provider once. Idempotent.

    Returns True if telemetry was successfully wired up, False otherwise.
    All failure modes (kill switch, missing certs, missing SDK, exporter
    init error) log to LOG_FILE and return False — the daemon proceeds
    with no-op tracing.
    """
    global _TELEMETRY_INITIALIZED
    if _TELEMETRY_INITIALIZED:
        return True
    if _telemetry_disabled():
        log("otel: disabled via BACK2BASE_OTEL env")
        return False
    if not OTEL_ENDPOINT:
        log("otel: no BACK2BASE_OTEL_ENDPOINT set; telemetry disabled")
        return False
    mtls_enabled = bool(str(OTEL_CERT_PATH)) and bool(str(OTEL_KEY_PATH)) \
        and OTEL_CERT_PATH.exists() and OTEL_KEY_PATH.exists()
    try:
        from opentelemetry import trace as otel_trace
        from opentelemetry.sdk.resources import Resource
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import BatchSpanProcessor
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
            OTLPSpanExporter,
        )
    except ImportError as e:
        log(f"otel: SDK import failed ({e}); telemetry disabled")
        return False
    try:
        exporter_kwargs = {"endpoint": f"{OTEL_ENDPOINT.rstrip('/')}/v1/traces"}
        if mtls_enabled:
            exporter_kwargs["client_certificate_file"] = str(OTEL_CERT_PATH)
            exporter_kwargs["client_key_file"] = str(OTEL_KEY_PATH)
        exporter = OTLPSpanExporter(**exporter_kwargs)
        resource = Resource.create({
            "service.name": TELEMETRY_SOURCE,
            "service.version": TELEMETRY_VERSION,
            "back2base.session": SESSION_ID,
            "back2base.user_id": os.environ.get("MEMORY_USER_ID", ""),
        })
        provider = TracerProvider(resource=resource)
        provider.add_span_processor(BatchSpanProcessor(exporter))
        otel_trace.set_tracer_provider(provider)
    except Exception as e:  # never let a telemetry hiccup take the daemon offline
        log(f"otel: init failed ({e}); telemetry disabled")
        return False
    _TELEMETRY_INITIALIZED = True
    log(f"otel: tracer initialized; endpoint={OTEL_ENDPOINT}")
    return True


def get_tracer():
    """Return a tracer (real if init_telemetry succeeded, no-op otherwise).

    Safe to call before init_telemetry — returns a no-op tracer.
    """
    if not _TELEMETRY_INITIALIZED:
        return _NOOP_TRACER
    try:
        from opentelemetry import trace as otel_trace
    except ImportError:
        return _NOOP_TRACER
    return otel_trace.get_tracer(TELEMETRY_SOURCE)


def inject_traceparent(headers):
    """Inject W3C traceparent (and tracestate) into a headers dict from
    the active span context. No-op when OTel isn't initialized or the
    SDK isn't installed.

    Mutates headers in place; returns headers for chaining.
    """
    if not _TELEMETRY_INITIALIZED:
        return headers
    try:
        from opentelemetry.propagate import inject
    except ImportError:
        return headers
    try:
        inject(headers)
    except Exception as e:
        log(f"otel: traceparent inject failed ({e})")
    return headers

# --- config ----------------------------------------------------------

HOME = Path(os.environ.get("HOME", "/home/node"))
NS = os.environ.get("MEMORY_NAMESPACE", "")
NS_DIR = (HOME / ".claude" / "projects" / NS) if NS else None

# Where Claude Code actually writes session JSONLs. The project-dir slug
# is PWD with '/' replaced by '-' (e.g. /workspace → -workspace), and
# JSONLs live directly under it at depth 1. The entrypoint exports
# CLAUDE_PROJECT_DIR pointing at this absolute path so we don't have to
# re-derive it (the daemon's CWD may differ from the user's PWD).
# Falls back to NS_DIR for old containers without the export — the glob
# pattern in find_active_session_jsonl handles either layout, so a stale
# entrypoint just degrades gracefully (statusline stays dim) instead of
# breaking outright.
_CC_DIR = os.environ.get("CLAUDE_PROJECT_DIR", "").strip()
SESSIONS_DIR = Path(_CC_DIR) if _CC_DIR else NS_DIR

# Per-container ephemeral runtime state lives off the ~/.claude bind
# mount so concurrent containers don't contend on these files and a
# fresh container starts with no stale values. /run/back2base is
# created in the Dockerfile and cleared by the entrypoint at phase 8.
# BACK2BASE_RUNTIME_DIR override is for bats — production runs unset.
RUNTIME_DIR = Path(os.environ.get("BACK2BASE_RUNTIME_DIR", "/run/back2base/power-steering"))
# OUT_DIR kept as an alias for in-tree call sites that still reference
# it; do not introduce new uses — write RUNTIME_DIR in new code.
OUT_DIR = RUNTIME_DIR

PENDING = RUNTIME_DIR / "pending.md"
CACHE_FILE = RUNTIME_DIR / ".cache.json"
METRICS_FILE = RUNTIME_DIR / "metrics.json"
TICK_FILE = RUNTIME_DIR / ".tick"
EVENTS_FILE = RUNTIME_DIR / "api-events.jsonl"
LOG_FILE = RUNTIME_DIR / "power-steering.log"
LOCKED_SESSION_FILE = RUNTIME_DIR / ".locked-session.json"

# Shared user-edited config (model / min_sleep / max_sleep). Stays on
# the bind mount so /power-steering edits apply across all containers.
RUNTIME_CONFIG_FILE = HOME / ".claude" / "power-steering" / "config.json"

# Daemon start time, used by find_active_session_jsonl to filter out
# JSONLs from prior containers' sessions sharing this project dir.
DAEMON_START = time.time()

CACHE_TTL = float(os.environ.get("POWER_STEERING_CACHE_TTL", "300"))
CACHE_MAX = int(os.environ.get("POWER_STEERING_CACHE_MAX", "20"))
CACHE_DISABLED = os.environ.get("POWER_STEERING_CACHE", "").lower() in (
    "off", "0", "false", "no",
)

POLL_SEC = float(os.environ.get("POWER_STEERING_POLL_SEC", "10"))
EVERY_N_TOOLS = int(os.environ.get("POWER_STEERING_EVERY_N_TOOLS", "5"))
WINDOW_TURNS = int(os.environ.get("POWER_STEERING_WINDOW_TURNS", "20"))
MODEL = os.environ.get("POWER_STEERING_MODEL", "claude-haiku-4-5")
MAX_TOKENS = int(os.environ.get("POWER_STEERING_MAX_TOKENS", "4096"))
THINKING_BUDGET = int(os.environ.get("POWER_STEERING_THINKING_BUDGET", "2048"))
PROMPT_PATH = Path(os.environ.get(
    "POWER_STEERING_PROMPT",
    "/opt/back2base/power-steering-prompt.md",
))
MIN_SLEEP = float(os.environ.get("POWER_STEERING_MIN_SLEEP", "60"))
MAX_SLEEP = float(os.environ.get("POWER_STEERING_MAX_SLEEP", "1800"))
DEFAULT_API_BASE = "https://api.anthropic.com"


def _env_with_prefix(name):
    """Read <NAME> from the container env; strip and return.

    All host-side auth values are forwarded into the container under
    their unprefixed name by docker-compose (`CLAUDE_CODE_OAUTH_TOKEN`,
    `ANTHROPIC_API_KEY`, ...). The BACK2BASE_<NAME> override is enforced
    host-side at the compose-interpolation boundary, so inside the
    container only the unprefixed name is ever populated. Kept as a
    helper for call-site readability and to preserve consistent
    whitespace-stripping.
    """
    return os.environ.get(name, "").strip()


# --- runtime config (hot-reload, edited by /power-steering) ---------
#
# The poll loop calls effective_config() every iteration so the slash
# command can adjust knobs without restarting the daemon. File overrides
# win over env defaults; missing or malformed files fall back silently
# (we never want a typo to take the daemon offline).

_RUNTIME_KEYS = (
    "model",
    "max_tokens",
    "thinking_budget",
    "every_n_tools",
    "window_turns",
    "min_sleep",
    "max_sleep",
)


def load_runtime_config():
    """Read RUNTIME_CONFIG_FILE; return whitelisted overrides as a dict.

    Anything outside _RUNTIME_KEYS is dropped — we don't let the file
    inject arbitrary state into the daemon. Missing file or malformed
    JSON returns an empty dict.
    """
    if not RUNTIME_CONFIG_FILE.exists():
        return {}
    try:
        with RUNTIME_CONFIG_FILE.open() as f:
            data = json.load(f)
    except (json.JSONDecodeError, OSError) as e:
        log(f"runtime-config error: {e}; falling back to env defaults")
        return {}
    if not isinstance(data, dict):
        return {}
    return {k: data[k] for k in _RUNTIME_KEYS if k in data}


def effective_config():
    """Merge env-default constants with runtime overrides.

    Returns the same shape every time so callers can index by key without
    KeyError handling. Numeric coercion happens here so the rest of the
    daemon doesn't have to repeat it.
    """
    o = load_runtime_config()
    return {
        "model": str(o.get("model", MODEL)),
        "max_tokens": int(o.get("max_tokens", MAX_TOKENS)),
        "thinking_budget": int(o.get("thinking_budget", THINKING_BUDGET)),
        "every_n_tools": int(o.get("every_n_tools", EVERY_N_TOOLS)),
        "window_turns": int(o.get("window_turns", WINDOW_TURNS)),
        "min_sleep": float(o.get("min_sleep", MIN_SLEEP)),
        "max_sleep": float(o.get("max_sleep", MAX_SLEEP)),
    }


def build_request_target():
    """Build the (url, headers, mode) target for /v1/messages.

    Mirrors the request shape Claude Code itself sends so the daemon
    rides the exact same auth path the user's interactive session uses.
    Two layers, stacked when both are available:

    Anthropic credential probed in order:
      ANTHROPIC_API_KEY         → x-api-key
      ANTHROPIC_AUTH_TOKEN      → Authorization: Bearer
      CLAUDE_CODE_OAUTH_TOKEN   → Authorization: Bearer
    Each name is read from the container's process env (the unprefixed
    form), populated by docker-compose from the matching BACK2BASE_<NAME>
    host-side variable.

    Returns (url, headers, mode) on success or (None, None, None) if no
    auth is configured. `mode` is always "direct" in the OSS build.
    """
    base = (_env_with_prefix("ANTHROPIC_BASE_URL")
            or DEFAULT_API_BASE).rstrip("/")
    url = f"{base}/v1/messages"
    headers = {}

    api_key = _env_with_prefix("ANTHROPIC_API_KEY")
    if api_key:
        headers["x-api-key"] = api_key
    else:
        bearer = (_env_with_prefix("ANTHROPIC_AUTH_TOKEN")
                  or _env_with_prefix("CLAUDE_CODE_OAUTH_TOKEN"))
        if bearer:
            headers["Authorization"] = f"Bearer {bearer}"

    if not headers:
        return None, None, None

    return url, headers, "direct"


# --- logging ---------------------------------------------------------

def log(msg):
    LOG_FILE.parent.mkdir(parents=True, exist_ok=True)
    ts = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    try:
        with LOG_FILE.open("a") as f:
            f.write(f"{ts} {msg}\n")
    except OSError:
        pass


# --- session discovery -----------------------------------------------

def _first_session_id(jsonl_path):
    """Return the sessionId from the first JSONL line that has one, or None.

    Tolerates malformed lines and partial writes — every line is parsed
    independently and a JSONDecodeError just falls through to the next.
    Reads at most the first 32 lines to bound work on a large transcript.
    """
    try:
        with jsonl_path.open() as f:
            for _ in range(32):
                line = f.readline()
                if not line:
                    return None
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                sid = obj.get("sessionId") if isinstance(obj, dict) else None
                if sid:
                    return sid
    except OSError:
        return None
    return None


def find_active_session_jsonl(sessions_dir, daemon_start=None):
    """Return this container's active session JSONL, or None.

    Find-then-stick: on first call we record the (sessionId, path) of
    the candidate we pick into LOCKED_SESSION_FILE. Subsequent calls
    re-validate that the recorded sessionId still appears in the same
    file (or any file in the dir) and return that path, ignoring
    newer JSONLs from concurrent containers writing to the same shared
    ~/.claude/projects/<slug>/ dir.

    Candidate filter on first lock:
      1. file is under sessions_dir at depth 1 or 2
      2. not under a .snapshots/ subdir
      3. first ~32 lines yield a sessionId
      4. mtime >= daemon_start - 60s (60s slack covers JSONLs created
         a few seconds before the daemon launched)

    daemon_start defaults to the module-level DAEMON_START. Tests pass
    daemon_start=0 to disable the timing filter.

    Falls back to the legacy "most-recently-modified" behavior only
    when no candidate satisfies the timing filter, so a brand-new
    daemon can still latch onto a session whose mtimes line up oddly.
    """
    if sessions_dir is None or not sessions_dir.exists():
        return None

    # 1) Honor existing lock if it still points to a file with the same sid.
    locked = _read_lock()
    if locked is not None:
        sid, path_str = locked
        # Direct hit on the recorded path.
        path = Path(path_str)
        if path.exists() and _first_session_id(path) == sid:
            return path
        # Path moved or was rewritten — scan the dir for any JSONL
        # carrying the locked sid.
        for p in _iter_jsonl(sessions_dir):
            if _first_session_id(p) == sid:
                _write_lock(sid, p)
                return p
        # Lock target is gone; fall through to acquire a new one.

    # 2) No lock (or stale lock). Acquire a fresh one.
    use_default_start = daemon_start is None
    if use_default_start:
        daemon_start = DAEMON_START
    cutoff = float(daemon_start) - 60.0

    candidates = []   # list of (mtime, path, sid)
    fallback = []     # legacy "most recently modified, has sid" pool
    for p in _iter_jsonl(sessions_dir):
        try:
            mtime = p.stat().st_mtime
        except OSError:
            continue
        sid = _first_session_id(p)
        if sid is None:
            continue
        if mtime >= cutoff:
            candidates.append((mtime, p, sid))
        else:
            fallback.append((mtime, p, sid))

    # Only use fallback when daemon_start was not explicitly supplied
    # (i.e. we defaulted to the module-level DAEMON_START). An explicit
    # daemon_start means the caller wants strict timing semantics.
    pool = candidates if candidates else (fallback if use_default_start else [])
    if not pool:
        return None
    pool.sort(reverse=True)
    _, path, sid = pool[0]
    _write_lock(sid, path)
    return path


def _iter_jsonl(sessions_dir):
    """Yield JSONL paths under sessions_dir at depth 1 or 2, skipping snapshots."""
    seen = set()
    for pattern in ("*.jsonl", "*/*.jsonl"):
        for jsonl in sessions_dir.glob(pattern):
            if ".snapshots" in jsonl.parts:
                continue
            key = str(jsonl)
            if key in seen:
                continue
            seen.add(key)
            yield jsonl


def _read_lock():
    """Return (session_id, path_str) from LOCKED_SESSION_FILE, or None."""
    try:
        data = json.loads(LOCKED_SESSION_FILE.read_text())
    except (OSError, json.JSONDecodeError):
        return None
    sid = data.get("session_id")
    path = data.get("path")
    if sid and path:
        return sid, path
    return None


def _write_lock(session_id, path):
    """Atomic write of LOCKED_SESSION_FILE."""
    try:
        LOCKED_SESSION_FILE.parent.mkdir(parents=True, exist_ok=True)
        tmp = LOCKED_SESSION_FILE.with_suffix(LOCKED_SESSION_FILE.suffix + ".tmp")
        tmp.write_text(json.dumps({"session_id": session_id, "path": str(path)}))
        tmp.rename(LOCKED_SESSION_FILE)
    except OSError as e:
        log(f"lock write failed: {e}")


# --- transcript parsing ----------------------------------------------

def read_jsonl(path):
    """Yield parsed JSON entries, tolerating a partially-written last line."""
    try:
        with path.open() as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    yield json.loads(line)
                except json.JSONDecodeError:
                    continue
    except OSError:
        return


def _entry_content(e):
    """Pull `message.content` from an entry, tolerating malformed shapes."""
    if not isinstance(e, dict):
        return None
    msg = e.get("message")
    if not isinstance(msg, dict):
        return None
    return msg.get("content")


def count_tool_uses(entries):
    """Number of assistant tool_use blocks across the entry list."""
    n = 0
    for e in entries:
        content = _entry_content(e)
        if not isinstance(content, list):
            continue
        for block in content:
            if isinstance(block, dict) and block.get("type") == "tool_use":
                n += 1
    return n


def first_user_text(entries):
    """First user-typed message (skipping tool_result-only user entries)."""
    for e in entries:
        if not isinstance(e, dict) or e.get("type") != "user":
            continue
        content = _entry_content(e)
        if isinstance(content, str) and content.strip():
            return content
        if isinstance(content, list):
            text_blocks = [
                b.get("text", "")
                for b in content
                if isinstance(b, dict) and b.get("type") == "text"
            ]
            joined = "\n".join(t for t in text_blocks if t).strip()
            if joined:
                return joined
    return ""


def recent_window(entries, n):
    """Last n entries, summarized to a JSON-friendly shape for the supervisor."""
    tail = entries[-n:]
    out = []
    for e in tail:
        if not isinstance(e, dict):
            continue
        kind = e.get("type", "?")
        content = _entry_content(e)
        if isinstance(content, str):
            out.append({"role": kind, "text": content[:2000]})
            continue
        if not isinstance(content, list):
            continue
        blocks = []
        for b in content:
            if not isinstance(b, dict):
                continue
            bt = b.get("type")
            if bt == "text":
                blocks.append({"text": b.get("text", "")[:2000]})
            elif bt == "tool_use":
                blocks.append({
                    "tool_use": b.get("name", "?"),
                    "input": str(b.get("input", ""))[:500],
                })
            elif bt == "tool_result":
                raw = b.get("content")
                if isinstance(raw, list):
                    raw = " ".join(
                        x.get("text", "") for x in raw
                        if isinstance(x, dict) and x.get("type") == "text"
                    )
                blocks.append({"tool_result": str(raw)[:1000]})
        if blocks:
            out.append({"role": kind, "blocks": blocks})
    return out


# --- API call --------------------------------------------------------

_NEXT_CHECK_RE = re.compile(r"^NEXT_CHECK_SECONDS:\s*(\d+)\s*$", re.MULTILINE)


def parse_supervisor_output(text):
    """Split supervisor output into (verdict_body, next_check_seconds).

    verdict_body is the text without the NEXT_CHECK_SECONDS line, stripped.
    next_check_seconds is the parsed integer or None if missing/malformed.
    The caller is responsible for clamping next_check_seconds to its
    configured min/max bounds.
    """
    if not text:
        return "", None
    m = _NEXT_CHECK_RE.search(text)
    next_check = None
    body = text
    if m:
        try:
            next_check = int(m.group(1))
        except ValueError:
            next_check = None
        body = _NEXT_CHECK_RE.sub("", text)
    return body.strip(), next_check


def build_request_body(supervisor_prompt, original_task, window, cfg=None):
    """Construct the Messages-API request body.

    Includes a `thinking` block when thinking_budget > 0 and strictly
    less than max_tokens — the API rejects requests where the budget
    meets or exceeds max_tokens, so we silently drop thinking in that
    case rather than fail the call.

    cfg defaults to effective_config() so callers (and tests) can pass
    an explicit override without hitting the file system.
    """
    if cfg is None:
        cfg = effective_config()
    body = {
        "model": cfg["model"],
        "max_tokens": cfg["max_tokens"],
        "system": [
            {
                "type": "text",
                "text": supervisor_prompt,
                "cache_control": {"type": "ephemeral", "ttl": "1h"},
            }
        ],
        "messages": [
            {
                "role": "user",
                "content": (
                    f"ORIGINAL TASK:\n{original_task}\n\n"
                    f"RECENT TURNS:\n{json.dumps(window, indent=2)[:30000]}"
                ),
            }
        ],
    }
    if 0 < cfg["thinking_budget"] < cfg["max_tokens"]:
        body["thinking"] = {
            "type": "enabled",
            "budget_tokens": cfg["thinking_budget"],
        }
    return body


def supervisor_check(supervisor_prompt, original_task, window, url, headers, cfg=None):
    """Call Anthropic API; return the supervisor's text response or None on failure."""
    if cfg is None:
        cfg = effective_config()
    body = build_request_body(supervisor_prompt, original_task, window, cfg)
    req_headers = {
        "Content-Type": "application/json",
        "anthropic-version": "2023-06-01",
        **headers,
    }
    # Opt the supervisor prompt into Anthropic's 1h extended ephemeral
    # cache (extended-cache-ttl-2025-04-11). The supervisor prompt is
    # ~17 KB and changes only on release; defaulting to the 5m TTL
    # forced a fresh prompt-cache write on every check past 5m.
    existing_beta = req_headers.get("anthropic-beta", "")
    req_headers["anthropic-beta"] = f"{existing_beta},extended-cache-ttl-2025-04-11" if existing_beta else "extended-cache-ttl-2025-04-11"  # noqa: E501
    # W3C traceparent: ties this /v1/messages call to the daemon's
    # supervisor-check span and gives the gateway-proxy worker a parent
    # context to hang its own span off. No-op if OTel isn't initialized.
    inject_traceparent(req_headers)
    req = urllib.request.Request(
        url,
        data=json.dumps(body).encode("utf-8"),
        headers=req_headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = json.loads(resp.read())
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        log(f"api error: {e}")
        return None
    # Record the supervisor's own API spend in the metrics sidecar so the
    # statusline tally includes it (true cost across all back2base calls).
    record_api_event("power-steering", cfg["model"], data)
    blocks = data.get("content", [])
    parts = [
        b.get("text", "")
        for b in blocks
        if isinstance(b, dict) and b.get("type") == "text"
    ]
    return "".join(parts).strip()


# --- local cache -----------------------------------------------------

def cache_input_hash(prompt, task, window):
    """Stable hash of the inputs that determine the supervisor verdict.

    Two checks with the same prompt/task/window produce the same hash;
    any change in the window invalidates the hash. We sort_keys when
    serializing the window so dict-key order doesn't shift the hash.
    """
    h = hashlib.sha256()
    h.update(prompt.encode("utf-8"))
    h.update(b"\x00")
    h.update(task.encode("utf-8"))
    h.update(b"\x00")
    h.update(json.dumps(window, sort_keys=True).encode("utf-8"))
    return h.hexdigest()


def cache_load():
    """Read the cache file. Returns a list of entries, oldest first."""
    if CACHE_DISABLED:
        return []
    try:
        with CACHE_FILE.open() as f:
            data = json.load(f)
    except (OSError, json.JSONDecodeError):
        return []
    entries = data.get("entries") if isinstance(data, dict) else None
    return entries if isinstance(entries, list) else []


def cache_save(entries):
    """Atomically persist the cache list."""
    if CACHE_DISABLED:
        return
    try:
        OUT_DIR.mkdir(parents=True, exist_ok=True)
        tmp = OUT_DIR / ".cache.json.tmp"
        with tmp.open("w") as f:
            json.dump({"entries": entries}, f)
        tmp.rename(CACHE_FILE)
    except OSError as e:
        log(f"cache save failed: {e}")


def cache_lookup(input_hash, now):
    """Return (verdict, next_check) for a fresh cache entry, or None."""
    if CACHE_DISABLED:
        return None
    for entry in cache_load():
        if not isinstance(entry, dict):
            continue
        if entry.get("hash") != input_hash:
            continue
        expires_at = entry.get("expires_at", 0)
        if expires_at <= now:
            continue
        return entry.get("verdict", ""), entry.get("next_check")
    return None


def cache_store(input_hash, verdict, next_check, now):
    """Insert or refresh an entry; evict oldest expired/excess entries."""
    if CACHE_DISABLED:
        return
    entries = cache_load()
    # Drop any prior entry for this hash and any expired entries.
    fresh = [
        e for e in entries
        if isinstance(e, dict)
        and e.get("hash") != input_hash
        and e.get("expires_at", 0) > now
    ]
    fresh.append({
        "hash": input_hash,
        "verdict": verdict,
        "next_check": next_check,
        "expires_at": now + CACHE_TTL,
    })
    # Bound: keep the most recently added CACHE_MAX (sorted by expires_at).
    if len(fresh) > CACHE_MAX:
        fresh.sort(key=lambda e: e.get("expires_at", 0))
        fresh = fresh[-CACHE_MAX:]
    cache_save(fresh)


# --- metrics ---------------------------------------------------------

# $/M token rates from public Anthropic pricing as of 2026-05.
# Rates rot when Anthropic updates pricing — refresh this table when
# the cost segment looks wrong. Keys are Claude Code's bare model ids;
# we longest-prefix-match to absorb variant suffixes like "[1m]".
_RATES = {
    # model id           : (input $/M, output $/M)
    "claude-opus-4-7":     (5.00, 25.00),
    "claude-opus-4-6":     (5.00, 25.00),
    "claude-opus-4-5":     (5.00, 25.00),
    "claude-opus-4-1":     (15.00, 75.00),
    "claude-opus-4":       (15.00, 75.00),
    "claude-sonnet-4-6":   (3.00, 15.00),
    "claude-sonnet-4-5":   (3.00, 15.00),
    "claude-sonnet-4":     (3.00, 15.00),
    "claude-haiku-4-5":    (1.00, 5.00),
    "claude-haiku-3-5":    (0.80, 4.00),
}
# Cache write premium and read discount, applied to input rate.
# Approximation: uses the 5-min TTL multiplier uniformly. The supervisor
# prompt opts into the 1h extended TTL (cache_control.ttl = "1h"), whose
# real write multiplier is 2.0×, so supervisor cache-write costs are
# under-reported. Tune if/when supervisor spend dominates the bill.
_CACHE_WRITE_MULT = 1.25
_CACHE_READ_MULT = 0.10


def _rate_for(model):
    """Longest-prefix match against the rate table, or (0, 0) for unknown."""
    if not model:
        return (0.0, 0.0)
    best = None
    for k in _RATES:
        if model.startswith(k) and (best is None or len(k) > len(best)):
            best = k
    return _RATES[best] if best else (0.0, 0.0)


def _usage_cost(model, usage):
    """Per-call USD cost from a usage dict. Cache-aware."""
    in_rate, out_rate = _rate_for(model)
    inp = usage.get("input_tokens", 0) or 0
    out = usage.get("output_tokens", 0) or 0
    cw = usage.get("cache_creation_input_tokens", 0) or 0
    cr = usage.get("cache_read_input_tokens", 0) or 0
    return (
        inp * in_rate
        + cw * in_rate * _CACHE_WRITE_MULT
        + cr * in_rate * _CACHE_READ_MULT
        + out * out_rate
    ) / 1_000_000


def _zero_metrics():
    return {
        "input_tokens": 0,
        "output_tokens": 0,
        "cache_creation_input_tokens": 0,
        "cache_read_input_tokens": 0,
        "request_count": 0,
        "cost_usd": 0.0,
        "by_source": {},  # source → {input, output, cache_*, request_count, cost_usd}
    }


def _add_usage(totals, source, model, usage):
    """Fold a single usage dict into the totals object in place."""
    inp = usage.get("input_tokens", 0) or 0
    out = usage.get("output_tokens", 0) or 0
    cw = usage.get("cache_creation_input_tokens", 0) or 0
    cr = usage.get("cache_read_input_tokens", 0) or 0
    cost = _usage_cost(model, usage)
    totals["input_tokens"] += inp
    totals["output_tokens"] += out
    totals["cache_creation_input_tokens"] += cw
    totals["cache_read_input_tokens"] += cr
    totals["request_count"] += 1
    totals["cost_usd"] += cost
    bs = totals["by_source"].setdefault(source, {
        "input_tokens": 0,
        "output_tokens": 0,
        "cache_creation_input_tokens": 0,
        "cache_read_input_tokens": 0,
        "request_count": 0,
        "cost_usd": 0.0,
    })
    bs["input_tokens"] += inp
    bs["output_tokens"] += out
    bs["cache_creation_input_tokens"] += cw
    bs["cache_read_input_tokens"] += cr
    bs["request_count"] += 1
    bs["cost_usd"] += cost


def _session_usages(entries):
    """Yield (model, usage_dict) for each assistant message in the session JSONL."""
    for e in entries:
        if not isinstance(e, dict) or e.get("type") != "assistant":
            continue
        msg = e.get("message")
        if not isinstance(msg, dict):
            continue
        usage = msg.get("usage")
        if not isinstance(usage, dict):
            continue
        yield msg.get("model", ""), usage


def _read_events(path):
    """Yield event dicts from the sidecar JSONL, tolerating partial lines."""
    try:
        with path.open() as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    yield json.loads(line)
                except json.JSONDecodeError:
                    continue
    except OSError:
        return


def compute_metrics(session_entries):
    """Aggregate usage from session JSONL + sidecar events file. Returns dict."""
    totals = _zero_metrics()
    for model, usage in _session_usages(session_entries):
        _add_usage(totals, "main", model, usage)
    for ev in _read_events(EVENTS_FILE):
        if not isinstance(ev, dict):
            continue
        usage = {
            "input_tokens": ev.get("input_tokens", 0),
            "output_tokens": ev.get("output_tokens", 0),
            "cache_creation_input_tokens": ev.get("cache_creation_input_tokens", 0),
            "cache_read_input_tokens": ev.get("cache_read_input_tokens", 0),
        }
        _add_usage(totals, ev.get("source", "unknown"), ev.get("model", ""), usage)
    return totals


def write_metrics(metrics):
    """Atomic write of metrics.json."""
    try:
        OUT_DIR.mkdir(parents=True, exist_ok=True)
        tmp = OUT_DIR / ".metrics.json.tmp"
        with tmp.open("w") as f:
            json.dump(metrics, f)
        tmp.rename(METRICS_FILE)
    except OSError as e:
        log(f"metrics write failed: {e}")


def record_api_event(source, model, response_data):
    """Append a usage event for a non-session API call to the sidecar.

    response_data is the parsed Anthropic API response — its `usage`
    object has the token fields we want. Best-effort: failures are
    logged and swallowed so the daemon never crashes on a write hiccup.
    """
    usage = (response_data or {}).get("usage") or {}
    if not usage:
        return
    event = {
        "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "source": source,
        "model": model,
        "input_tokens": usage.get("input_tokens", 0),
        "output_tokens": usage.get("output_tokens", 0),
        "cache_creation_input_tokens": usage.get("cache_creation_input_tokens", 0),
        "cache_read_input_tokens": usage.get("cache_read_input_tokens", 0),
    }
    try:
        EVENTS_FILE.parent.mkdir(parents=True, exist_ok=True)
        with EVENTS_FILE.open("a") as f:
            f.write(json.dumps(event) + "\n")
    except OSError as e:
        log(f"event write failed: {e}")


# --- pending file ----------------------------------------------------

def write_pending(text):
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    tmp = OUT_DIR / ".pending.md.tmp"
    tmp.write_text(text)
    tmp.rename(PENDING)
    # Record drift timestamp so the CLAUDE.md audit hook can defer
    # while drift is being handled.
    state_path = OUT_DIR / "drift-state.json"
    state_tmp = OUT_DIR / ".drift-state.json.tmp"
    state_tmp.write_text(json.dumps({"last_drift_ts": int(time.time())}, separators=(",", ":")))
    state_tmp.rename(state_path)


# --- main loop -------------------------------------------------------

def clamp_sleep(seconds, cfg=None):
    """Bound the supervisor's suggested sleep to [min_sleep, max_sleep].

    Reads runtime config when cfg is None; tests and the poll loop pass
    an explicit cfg so the file system isn't re-read mid-iteration.
    """
    if seconds is None:
        return None
    if cfg is None:
        cfg = effective_config()
    return max(cfg["min_sleep"], min(cfg["max_sleep"], float(seconds)))


def wait_for_tick(path, max_wait, mtime_floor=0.0):
    """Block until the tick file's mtime exceeds mtime_floor or max_wait elapses.

    Returns the new mtime on activity, or None on timeout. Tolerates a
    missing file — treated as no activity (returns None at max_wait).

    Polls at 0.5s intervals; the cadence is internal and unrelated to
    the supervisor's NEXT_CHECK_SECONDS hint (which clamp_sleep handles).
    """
    deadline = time.time() + max_wait
    while True:
        try:
            mtime = path.stat().st_mtime
        except (OSError, FileNotFoundError):
            mtime = mtime_floor
        if mtime > mtime_floor:
            return mtime
        if time.time() >= deadline:
            return None
        time.sleep(min(0.5, max(0.05, deadline - time.time())))


def main():
    if os.environ.get("BACK2BASE_POWER_STEERING", "").lower() in ("off", "0", "false", "no"):
        log("disabled via BACK2BASE_POWER_STEERING env; exiting")
        return
    url, headers, mode = build_request_target()
    if headers is None:
        log("no auth env var set (ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN / "
            "CLAUDE_CODE_OAUTH_TOKEN); daemon idle")
        return
    if not NS:
        log("MEMORY_NAMESPACE not set; daemon idle")
        return
    try:
        supervisor_prompt = PROMPT_PATH.read_text()
    except OSError as e:
        log(f"prompt template missing: {e}")
        return

    # Best-effort OTel init. Failure modes log to LOG_FILE; daemon
    # continues with no-op tracing.
    init_telemetry()
    tracer = get_tracer()

    last_path = None
    last_tool_count = 0
    last_tick_mtime = 0.0
    fallback_sleep = POLL_SEC
    log(f"daemon up; ns={NS} model={MODEL} mode={mode} url={url} "
        f"every-n={EVERY_N_TOOLS}")

    while True:
        next_sleep = fallback_sleep
        # Re-read runtime config every iteration so /power-steering edits
        # take effect on the next tick (no daemon restart required).
        cfg = effective_config()
        with tracer.start_as_current_span("power-steering.iteration") as iter_span:
            iter_span.set_attribute("back2base.source", TELEMETRY_SOURCE)
            iter_span.set_attribute("back2base.session", SESSION_ID)
            try:
                with tracer.start_as_current_span("power-steering.find_session") as fs_span:
                    path = find_active_session_jsonl(SESSIONS_DIR)
                    fs_span.set_attribute("session.found", path is not None)
                    if path is not None:
                        fs_span.set_attribute("session.path", str(path))
                if path is None:
                    iter_span.add_event("no_session")
                    new_mtime = wait_for_tick(TICK_FILE, max_wait=next_sleep, mtime_floor=last_tick_mtime)
                    if new_mtime is not None:
                        last_tick_mtime = new_mtime
                    continue
                if path != last_path:
                    last_path = path
                    last_tool_count = 0
                    log(f"tracking session: {path.name}")

                entries = list(read_jsonl(path))
                if not entries:
                    iter_span.add_event("empty_transcript")
                    new_mtime = wait_for_tick(TICK_FILE, max_wait=next_sleep, mtime_floor=last_tick_mtime)
                    if new_mtime is not None:
                        last_tick_mtime = new_mtime
                    continue

                # Refresh metrics every poll (cheap: O(n) sum over already-loaded
                # entries plus a tail of the sidecar events file). The statusline
                # reads metrics.json on its own ~5s cadence.
                try:
                    write_metrics(compute_metrics(entries))
                except Exception as e:  # never let a metrics hiccup crash the daemon
                    log(f"metrics error: {e}")

                with tracer.start_as_current_span("power-steering.gate_check") as gate_span:
                    tool_count = count_tool_uses(entries)
                    delta = tool_count - last_tool_count
                    threshold = cfg["every_n_tools"]
                    gate_span.set_attribute("gate.tool_count", tool_count)
                    gate_span.set_attribute("gate.delta", delta)
                    gate_span.set_attribute("gate.threshold", threshold)
                    gate_span.set_attribute("gate.passed", delta >= threshold)
                if delta < threshold:
                    iter_span.add_event("gate_skip")
                    new_mtime = wait_for_tick(TICK_FILE, max_wait=next_sleep, mtime_floor=last_tick_mtime)
                    if new_mtime is not None:
                        last_tick_mtime = new_mtime
                    continue

                task = first_user_text(entries)
                window = recent_window(entries, cfg["window_turns"])

                # Local cache: byte-identical (prompt + task + window) ⇒ reuse
                # the prior verdict and skip the API call entirely. Hits are
                # rare in steady state (the trigger gates on N new tool calls)
                # but real on daemon restart, transcript rewinds, and bursty
                # tool-use patterns.
                now = time.time()
                input_hash = cache_input_hash(supervisor_prompt, task, window)
                with tracer.start_as_current_span("power-steering.cache_lookup") as cache_span:
                    cached = cache_lookup(input_hash, now)
                    cache_span.set_attribute("cache.hit", cached is not None)
                if cached is not None:
                    verdict, suggested = cached
                    log(f"cache hit ({tool_count} tools)")
                    iter_span.set_attribute("verdict.cached", True)
                else:
                    with tracer.start_as_current_span("power-steering.supervisor_check") as check_span:
                        check_span.set_attribute("gen_ai.system", "anthropic")
                        check_span.set_attribute("gen_ai.request.model", cfg["model"])
                        check_span.set_attribute("gen_ai.request.max_tokens", cfg["max_tokens"])
                        check_span.set_attribute("auth.mode", mode)
                        raw = supervisor_check(
                            supervisor_prompt, task, window, url, headers, cfg,
                        )
                        check_span.set_attribute("api.success", raw is not None)
                    if raw is None:
                        iter_span.add_event("api_error")
                        new_mtime = wait_for_tick(TICK_FILE, max_wait=next_sleep, mtime_floor=last_tick_mtime)
                        if new_mtime is not None:
                            last_tick_mtime = new_mtime
                        continue
                    verdict, suggested = parse_supervisor_output(raw)
                    cache_store(input_hash, verdict, suggested, now)
                    iter_span.set_attribute("verdict.cached", False)

                last_tool_count = tool_count

                v = verdict.strip()
                iter_span.set_attribute("verdict.kind",
                                        "ok" if v == "OK" else ("drift" if v else "empty"))
                if v == "OK":
                    if PENDING.exists():
                        PENDING.write_text("")
                    log(f"check ok ({tool_count} tools, next={suggested}s)")
                elif v:
                    write_pending(verdict)
                    log(f"check drift ({tool_count} tools, next={suggested}s): "
                        f"{verdict[:120]}")
                else:
                    log(f"check empty (raw[:80]={raw[:80]!r})")

                clamped = clamp_sleep(suggested, cfg)
                if clamped is not None:
                    next_sleep = clamped
            except Exception as e:  # daemon must not crash on a transient hiccup
                log(f"loop error: {e}")
                iter_span.record_exception(e)
        # End of iteration span. Wait happens outside the span so the
        # span duration reflects work, not idle time.
        new_mtime = wait_for_tick(TICK_FILE, max_wait=next_sleep, mtime_floor=last_tick_mtime)
        if new_mtime is not None:
            last_tick_mtime = new_mtime


if __name__ == "__main__":
    main()
