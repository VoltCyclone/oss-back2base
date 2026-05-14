---
name: debug-ops
model: opus
context: fork
agent: general-purpose
allowed-tools: Read Grep Glob Bash
description: "Systematic debugging methodology with language-specific debuggers, isolation techniques, root cause analysis, and common gotchas. Use for: debugging, errors, crashes, exceptions, stack traces, breakpoints, stepping, watchpoints, core dumps, segfaults, memory leaks, race conditions, deadlocks."
---

# Debug Operations

Systematic debugging methodology covering root cause analysis, isolation techniques, and language-specific debugger workflows.

## Debugging Decision Tree

```
What kind of problem?
│
├─ Runtime error / exception
│  ├─ Read the full stack trace (bottom frame = root cause)
│  ├─ Reproduce with minimal input
│  └─ Bisect: comment out code until error disappears
│
├─ Wrong output / logic bug
│  ├─ Add assertions at key checkpoints
│  ├─ Print intermediate values (or use debugger watch)
│  ├─ Compare expected vs actual at each step
│  └─ Binary search: which step first diverges?
│
├─ Performance issue
│  ├─ Profile first (don't guess)
│  ├─ Check algorithmic complexity
│  └─ See perf-ops skill for profiling workflows
│
├─ Intermittent / flaky
│  ├─ Race condition? Add logging with timestamps
│  ├─ Resource exhaustion? Monitor handles/connections/memory
│  ├─ Timing-dependent? Try adding delays or removing them
│  └─ Run in loop: `for i in $(seq 100); do npm test || break; done`
│
├─ Works locally, fails in CI/prod
│  ├─ Environment diff: OS, versions, env vars, filesystem
│  ├─ Network: DNS, firewalls, timeouts, proxy
│  ├─ Permissions: file modes, user accounts, capabilities
│  └─ Concurrency: single-core CI runner vs multi-core local
│
└─ Crash / segfault / core dump
   ├─ Get stack trace from core dump
   ├─ Check for null pointer dereference
   ├─ Check for buffer overflow / use-after-free
   └─ Run under AddressSanitizer / Valgrind
```

## Root Cause Analysis (5 Whys)

1. **What happened?** (symptom)
2. **What was the immediate cause?** (proximate)
3. **Why did that happen?** (contributing factor)
4. **Why wasn't it caught?** (missing safeguard)
5. **What systemic issue allowed this?** (root cause)

## Isolation Techniques

| Technique | When to Use | How |
|-----------|-------------|-----|
| **Binary search** | Bug in large codebase | Comment out half the code, check if bug persists |
| **Minimal repro** | Complex test case | Strip away everything not needed to trigger |
| **Git bisect** | "This used to work" | `git bisect start BAD GOOD` then test each commit |
| **Dependency isolation** | External library suspected | Mock/stub the dependency |
| **Environment isolation** | "Works on my machine" | Docker container with clean env |
| **Input isolation** | Bad data suspected | Test with minimal valid input, add complexity |

## Git Bisect Workflow

```bash
git bisect start
git bisect bad                    # Current commit is broken
git bisect good v1.2.0            # This version was working
# Git checks out a middle commit
# Test it, then:
git bisect good                   # or git bisect bad
# Repeat until git finds the first bad commit
git bisect reset                  # Return to original branch
```

Automated bisect:
```bash
git bisect start HEAD v1.2.0
git bisect run npm test           # Automatically test each commit
```

## Language-Specific Debuggers

### Node.js
```bash
node --inspect-brk script.js      # Break on first line
node --inspect script.js          # Attach later
# Open chrome://inspect in Chrome
```

### Python
```python
import pdb; pdb.set_trace()       # Classic
breakpoint()                       # Python 3.7+ (respects PYTHONBREAKPOINT)
import ipdb; ipdb.set_trace()     # Enhanced (pip install ipdb)
```

### Go
```bash
dlv debug ./cmd/server            # Start delve debugger
dlv test ./pkg/auth               # Debug tests
dlv attach <pid>                  # Attach to running process
```

### Rust
```bash
rust-gdb target/debug/myapp       # GDB with Rust pretty-printers
rust-lldb target/debug/myapp      # LLDB alternative
```

## Common Debugging Gotchas

| Gotcha | Symptom | Fix |
|--------|---------|-----|
| Stale build | Fix doesn't take effect | Clean rebuild |
| Wrong branch | Code looks correct but fails | Check `git branch` |
| Cached data | Old behavior persists | Clear caches (browser, Redis, DB) |
| Environment variable | Works locally, fails elsewhere | Print env vars at startup |
| Timezone | Off-by-N-hours errors | Log timestamps in UTC |
| Floating point | `0.1 + 0.2 !== 0.3` | Use epsilon comparison or integers |
| Async ordering | Intermittent wrong order | Add explicit ordering/await |
| Connection pool exhaustion | Timeout after N requests | Check for leaked connections |
| DNS caching | Config change not taking effect | Check TTL, restart resolver |
| File descriptor leak | "Too many open files" | Check for unclosed handles |

## Logging Best Practices for Debugging

```
# Add structured context
logger.error("Payment failed", {
  userId: user.id,
  amount: order.total,
  gateway: "stripe",
  errorCode: err.code,
  requestId: req.id
})

# Correlation IDs for distributed tracing
# Pass request ID through all service calls
```

## MCP-Augmented Debugging

Before diving into code, use MCPs to establish ground truth:

| Situation | MCP | What to ask |
|---|---|---|
| Production error / incident | `datadog` | Error rate, logs, traces for the service in the failure window |
| TypeScript runtime error | `lsmcp` | `lsp_get_diagnostics` — often reveals root cause statically |
| Go panic / build failure | `godevmcp` | Build check + vet before reading code |
| "Works locally, fails in CI" | `buildkite` | Fetch the failing build log directly |
| Unknown library behavior | `context7` | Authoritative docs for the exact API |
| Kubernetes pod crash | `kubernetes` | `describe pod`, `logs --previous` |

Always pull Datadog data first for production issues — metrics and traces narrow the search space faster than reading code.

## When You're Stuck

1. Explain the bug to someone (rubber duck debugging)
2. Take a break - fresh eyes find bugs faster
3. Read the docs for the exact function/API that's misbehaving
4. Search for the exact error message (include quotes)
5. Check if there's a known issue in the library's GitHub
6. Simplify until it works, then add complexity back
