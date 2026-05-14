#!/usr/bin/env python3
"""Datadog LLM Observability evaluators for back2base.

Submits four custom evaluations per Claude Code turn:
  - skill_choice      (categorical: hit | miss | wrong | not_applicable)
  - memory_hygiene    (score 0..1, with per-check tags)
  - efficiency        (score 0..1, with tokens/tool-call tags)
  - answer_quality    (categorical: pass | fail, heuristic verification check)

Wired in as Claude Code hooks; the shell wrapper enqueues the event JSON
and spawns this module in the background so the hook itself stays <1s.

Usage:
  python3 -m ddog_llm_evals process <event-json-file>
  python3 -m ddog_llm_evals tooluse  <event-json-file>
"""

from __future__ import annotations

import json
import os
import re
import sys
import time
from pathlib import Path
from typing import Any

QUEUE_DIR = Path(os.environ.get(
    "B2B_DDOG_QUEUE",
    str(Path.home() / ".claude" / ".back2base-hooks" / "ddog-llm-evals"),
))
COUNTERS_PATH = QUEUE_DIR / "tool-counters.jsonl"

# Heuristic skill-trigger registry. Keys are regex patterns over the user prompt;
# value is the canonical skill slug that *should* fire. Extend via
# ~/.config/back2base/ddog-skill-triggers.json (same shape).
DEFAULT_SKILL_TRIGGERS: dict[str, str] = {
    r"\b(brainstorm|design|new feature|add (a )?feature|build (a |an )?\w+)\b":
        "superpowers:brainstorming",
    r"\b(bug|broken|failing test|test fail|crash|stack ?trace|panic)\b":
        "superpowers:systematic-debugging",
    r"\b(write tests?|tdd|red-green|test[- ]driven)\b":
        "superpowers:test-driven-development",
    r"\b(plan|implementation plan|spec out)\b":
        "superpowers:writing-plans",
    r"\b(remember|recall|memory|last time|before)\b":
        "remember:remember",
    r"\b(verify|verified|confirm (it )?works|before (i )?(merge|ship))\b":
        "superpowers:verification-before-completion",
    r"\b(refactor|clean up|simplify)\b":
        "simplify",
}

# Completion-claim regexes that should be backed by verification evidence.
COMPLETION_CLAIMS = re.compile(
    r"\b(all (tests )?pass(ing|ed)?|fixed|done|complete[d]?|"
    r"ready to (merge|ship|deploy)|works now|good to go)\b",
    re.IGNORECASE,
)

# Bash invocations we accept as verification.
VERIFICATION_BASH = re.compile(
    r"\b(npm test|pnpm test|yarn test|pytest|go test|cargo test|"
    r"vitest|jest|bats|make test|tox)\b",
    re.IGNORECASE,
)


# ── Datadog LLMObs bootstrap ────────────────────────────────────────────────

def _llmobs():
    """Return the LLMObs class if ddtrace is installed and DD_LLMOBS_ENABLED is on.

    Returns None when disabled so the rest of the code can no-op cheaply.
    """
    if os.environ.get("DD_LLMOBS_ENABLED", "0").lower() not in {"1", "true", "yes", "on"}:
        return None
    try:
        from ddtrace.llmobs import LLMObs  # type: ignore
    except ImportError:
        return None

    if not getattr(_llmobs, "_inited", False):
        LLMObs.enable(
            ml_app=os.environ.get("DD_LLMOBS_ML_APP", "back2base"),
            api_key=os.environ.get("DD_API_KEY"),
            site=os.environ.get("DD_SITE", "datadoghq.com"),
            agentless_enabled=True,
        )
        _llmobs._inited = True  # type: ignore[attr-defined]
    return LLMObs


# ── Transcript parsing ──────────────────────────────────────────────────────

def _read_transcript(path: str) -> list[dict[str, Any]]:
    """Read a Claude Code transcript JSONL into a list of records."""
    if not path or not os.path.exists(path):
        return []
    out: list[dict[str, Any]] = []
    with open(path, "r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except json.JSONDecodeError:
                continue
    return out


def _current_turn(records: list[dict[str, Any]]) -> dict[str, Any]:
    """Slice the transcript down to the most recent user→assistant turn."""
    last_user_idx = max(
        (i for i, r in enumerate(records) if r.get("role") == "user"),
        default=-1,
    )
    if last_user_idx < 0:
        return {"user_text": "", "assistant_text": "", "tool_uses": [], "records": records}

    turn = records[last_user_idx:]
    user_text = _flatten_content(turn[0].get("content", ""))
    assistant_text_parts: list[str] = []
    tool_uses: list[dict[str, Any]] = []
    for rec in turn[1:]:
        if rec.get("role") != "assistant":
            continue
        content = rec.get("content", [])
        if isinstance(content, str):
            assistant_text_parts.append(content)
            continue
        for block in content:
            if block.get("type") == "text":
                assistant_text_parts.append(block.get("text", ""))
            elif block.get("type") == "tool_use":
                tool_uses.append({
                    "name": block.get("name", ""),
                    "input": block.get("input", {}),
                })
    return {
        "user_text": user_text,
        "assistant_text": "\n".join(assistant_text_parts),
        "tool_uses": tool_uses,
        "records": turn,
    }


def _flatten_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(
            b.get("text", "") for b in content if isinstance(b, dict) and b.get("type") == "text"
        )
    return ""


# ── Evaluator: skill choice ─────────────────────────────────────────────────

def eval_skill_choice(turn: dict[str, Any]) -> dict[str, Any]:
    triggers = _load_skill_triggers()
    user_text = turn["user_text"] or ""
    invoked = [
        (t["input"] or {}).get("skill", "")
        for t in turn["tool_uses"]
        if t["name"] == "Skill"
    ]
    expected = [slug for pat, slug in triggers.items() if re.search(pat, user_text, re.IGNORECASE)]

    if not expected and not invoked:
        verdict, assess = "not_applicable", "pass"
        reason = "No trigger keywords detected; no skill invoked."
    elif expected and any(slug in invoked for slug in expected):
        verdict, assess = "hit", "pass"
        reason = f"Expected one of {expected}; invoked {invoked}."
    elif expected and not invoked:
        verdict, assess = "miss", "fail"
        reason = f"Triggers suggested {expected} but no skill was invoked."
    elif invoked and not expected:
        verdict, assess = "wrong", "fail"
        reason = f"Invoked {invoked} but no trigger keywords were present."
    else:
        verdict, assess = "wrong", "fail"
        reason = f"Expected {expected}; invoked {invoked} instead."

    return {
        "label": "skill_choice",
        "metric_type": "categorical",
        "value": verdict,
        "assessment": assess,
        "reasoning": reason,
        "tags": {"invoked": ",".join(invoked) or "none",
                 "expected": ",".join(expected) or "none"},
    }


def _load_skill_triggers() -> dict[str, str]:
    override = Path.home() / ".config" / "back2base" / "ddog-skill-triggers.json"
    if override.exists():
        try:
            return json.loads(override.read_text())
        except (OSError, json.JSONDecodeError):
            pass
    return DEFAULT_SKILL_TRIGGERS


# ── Evaluator: memory hygiene ───────────────────────────────────────────────

def eval_memory_hygiene(turn: dict[str, Any], memory_root: Path) -> dict[str, Any]:
    checks: dict[str, bool] = {}
    notes: list[str] = []
    user_text = turn["user_text"] or ""

    # 1. If user referenced past context, assistant should have read MEMORY.md.
    referenced_past = bool(re.search(
        r"\b(remember|last time|before|previously|you said)\b",
        user_text, re.IGNORECASE,
    ))
    if referenced_past:
        read_memory = any(
            t["name"] == "Read" and "MEMORY.md" in str((t["input"] or {}).get("file_path", ""))
            for t in turn["tool_uses"]
        )
        checks["read_memory_on_recall"] = read_memory
        if not read_memory:
            notes.append("user invoked memory but MEMORY.md was not read")

    # 2. Any memory write must look well-formed.
    memory_writes = [
        t for t in turn["tool_uses"]
        if t["name"] in {"Write", "Edit"} and "memory/" in str((t["input"] or {}).get("file_path", ""))
    ]
    for w in memory_writes:
        body = (w["input"] or {}).get("content") or (w["input"] or {}).get("new_string", "")
        path = (w["input"] or {}).get("file_path", "")
        if path.endswith("MEMORY.md"):
            continue  # index file, different rules
        ok_frontmatter = "---" in body and "type:" in body and "name:" in body
        type_match = re.search(r"type:\s*(\w+)", body or "")
        ok_type = bool(type_match and type_match.group(1) in {"user", "feedback", "project", "reference"})
        checks[f"frontmatter:{Path(path).name}"] = ok_frontmatter and ok_type
        if not (ok_frontmatter and ok_type):
            notes.append(f"malformed memory frontmatter in {path}")

    # 3. MEMORY.md stays under 200 lines (index hygiene).
    index = memory_root / "MEMORY.md"
    if index.exists():
        n = sum(1 for _ in index.open())
        checks["memory_index_under_200"] = n <= 200
        if n > 200:
            notes.append(f"MEMORY.md has {n} lines (>200)")

    if not checks:
        score = 1.0
    else:
        score = sum(1 for v in checks.values() if v) / len(checks)

    return {
        "label": "memory_hygiene",
        "metric_type": "score",
        "value": round(score, 3),
        "assessment": "pass" if score >= 0.8 else "fail",
        "reasoning": "; ".join(notes) or "all memory checks passed",
        "tags": {k: str(v).lower() for k, v in checks.items()},
        "metadata": {"checks": checks},
    }


# ── Evaluator: efficiency ───────────────────────────────────────────────────

def eval_efficiency(turn: dict[str, Any], counters: dict[str, int]) -> dict[str, Any]:
    tokens_in = tokens_out = 0
    parallel_msgs = total_tool_msgs = 0
    files_read: list[str] = []
    for rec in turn["records"]:
        usage = rec.get("usage") or rec.get("message", {}).get("usage") or {}
        tokens_in += int(usage.get("input_tokens", 0) or 0)
        tokens_out += int(usage.get("output_tokens", 0) or 0)
        if rec.get("role") == "assistant":
            content = rec.get("content", [])
            if isinstance(content, list):
                tool_blocks = [b for b in content if b.get("type") == "tool_use"]
                if tool_blocks:
                    total_tool_msgs += 1
                    if len(tool_blocks) >= 2:
                        parallel_msgs += 1
                    for b in tool_blocks:
                        if b.get("name") == "Read":
                            fp = (b.get("input") or {}).get("file_path")
                            if fp:
                                files_read.append(fp)

    tool_calls = counters.get("count", 0) or sum(
        1 for r in turn["records"]
        if r.get("role") == "assistant"
        for b in (r.get("content") or [])
        if isinstance(b, dict) and b.get("type") == "tool_use"
    )
    redundant_reads = len(files_read) - len(set(files_read))
    parallel_ratio = parallel_msgs / total_tool_msgs if total_tool_msgs else 1.0

    # Composite: penalize redundancy, reward parallelism, soft-cap tokens.
    token_score = max(0.0, 1.0 - max(0, tokens_in + tokens_out - 20_000) / 80_000)
    redundancy_score = max(0.0, 1.0 - redundant_reads * 0.1)
    score = round(0.4 * token_score + 0.3 * parallel_ratio + 0.3 * redundancy_score, 3)

    return {
        "label": "efficiency",
        "metric_type": "score",
        "value": score,
        "assessment": "pass" if score >= 0.7 else "fail",
        "reasoning": (
            f"tokens={tokens_in+tokens_out}, tool_calls={tool_calls}, "
            f"parallel_ratio={parallel_ratio:.2f}, redundant_reads={redundant_reads}"
        ),
        "tags": {
            "tokens_in": str(tokens_in),
            "tokens_out": str(tokens_out),
            "tool_calls": str(tool_calls),
            "redundant_reads": str(redundant_reads),
        },
        "metadata": {
            "parallel_msgs": parallel_msgs,
            "total_tool_msgs": total_tool_msgs,
        },
    }


# ── Evaluator: answer quality (verification-before-completion) ──────────────

def eval_answer_quality(turn: dict[str, Any]) -> dict[str, Any]:
    text = turn["assistant_text"] or ""
    claimed_done = bool(COMPLETION_CLAIMS.search(text))
    if not claimed_done:
        return {
            "label": "answer_quality",
            "metric_type": "categorical",
            "value": "pass",
            "assessment": "pass",
            "reasoning": "no completion claim made",
            "tags": {"claimed_done": "false"},
        }

    ran_verification = any(
        t["name"] == "Bash" and VERIFICATION_BASH.search(str((t["input"] or {}).get("command", "")))
        for t in turn["tool_uses"]
    )
    verdict = "pass" if ran_verification else "fail"
    return {
        "label": "answer_quality",
        "metric_type": "categorical",
        "value": verdict,
        "assessment": verdict,
        "reasoning": (
            "claimed completion and ran a test command"
            if ran_verification
            else "claimed completion without running a verification command"
        ),
        "tags": {"claimed_done": "true", "verified": str(ran_verification).lower()},
    }


# ── Orchestration ───────────────────────────────────────────────────────────

def _submit_all(turn: dict[str, Any], counters: dict[str, int], memory_root: Path) -> None:
    llmobs = _llmobs()
    if llmobs is None:
        # Still write a local JSONL so users can inspect evals without Datadog.
        _write_local(turn, counters, memory_root)
        return

    evals = [
        eval_skill_choice(turn),
        eval_memory_hygiene(turn, memory_root),
        eval_efficiency(turn, counters),
        eval_answer_quality(turn),
    ]

    # Wrap the turn in a synthetic agent workflow span so evals have a target.
    with llmobs.agent(name="claude-code.turn") as span:
        llmobs.annotate(
            span=span,
            input_data=turn["user_text"],
            output_data=turn["assistant_text"][:8000],
            tags={
                "session": os.environ.get("CLAUDE_SESSION_ID", "unknown"),
                "project": os.environ.get("CLAUDE_PROJECT_DIR", "unknown"),
            },
        )
        span_context = llmobs.export_span(span=span)
        for e in evals:
            llmobs.submit_evaluation(
                span=span_context,
                ml_app=os.environ.get("DD_LLMOBS_ML_APP", "back2base"),
                label=e["label"],
                metric_type=e["metric_type"],
                value=e["value"],
                assessment=e.get("assessment"),
                reasoning=e.get("reasoning"),
                tags=e.get("tags"),
                metadata=e.get("metadata"),
            )
    llmobs.flush()


def _write_local(turn: dict[str, Any], counters: dict[str, int], memory_root: Path) -> None:
    out = QUEUE_DIR / "evals.jsonl"
    QUEUE_DIR.mkdir(parents=True, exist_ok=True)
    payload = {
        "ts": int(time.time() * 1000),
        "evals": [
            eval_skill_choice(turn),
            eval_memory_hygiene(turn, memory_root),
            eval_efficiency(turn, counters),
            eval_answer_quality(turn),
        ],
    }
    with out.open("a", encoding="utf-8") as fh:
        fh.write(json.dumps(payload) + "\n")


def _load_counters() -> dict[str, int]:
    if not COUNTERS_PATH.exists():
        return {}
    count = 0
    for _ in COUNTERS_PATH.open():
        count += 1
    return {"count": count}


def _reset_counters() -> None:
    if COUNTERS_PATH.exists():
        COUNTERS_PATH.unlink()


# ── CLI entrypoints (invoked by the shell hook) ────────────────────────────

def cmd_process(event_path: str) -> int:
    try:
        event = json.loads(Path(event_path).read_text())
    except (OSError, json.JSONDecodeError):
        return 0
    transcript = event.get("transcript_path") or event.get("transcriptPath") or ""
    records = _read_transcript(transcript)
    turn = _current_turn(records)
    counters = _load_counters()
    cwd = event.get("cwd") or os.getcwd()
    memory_root = Path(cwd) / "memory"
    if not memory_root.exists():
        memory_root = Path.home() / ".claude" / "projects" / Path(cwd).name / "memory"
    _submit_all(turn, counters, memory_root)
    _reset_counters()
    return 0


def cmd_tooluse(event_path: str) -> int:
    QUEUE_DIR.mkdir(parents=True, exist_ok=True)
    try:
        event = json.loads(Path(event_path).read_text())
    except (OSError, json.JSONDecodeError):
        event = {}
    with COUNTERS_PATH.open("a", encoding="utf-8") as fh:
        fh.write(json.dumps({
            "ts": int(time.time() * 1000),
            "tool": event.get("tool_name") or event.get("toolName") or "",
        }) + "\n")
    return 0


def main(argv: list[str]) -> int:
    if len(argv) < 3:
        return 0
    cmd, event_path = argv[1], argv[2]
    if cmd == "process":
        return cmd_process(event_path)
    if cmd == "tooluse":
        return cmd_tooluse(event_path)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
