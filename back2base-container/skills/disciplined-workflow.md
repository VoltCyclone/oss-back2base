---
name: disciplined-workflow
model: sonnet
allowed-tools: Read Write Edit Bash Grep Glob
description: "Workflow discipline gate - routes any task through brainstorm/plan/debug/verify gates before code is touched, enforces evidence-before-claim, and names the failure modes (premature implementation, claimed-done-without-running). Use for: implement, build, add feature, fix bug, refactor, write tests, design, before I start, multi-step task, several files, plan this, can you, let's, I want to."
---

# Disciplined Workflow

The CLAUDE.md template tells you to classify → route → approach → execute → verify. This skill says **how to detect when each gate is non-optional**, and what the gate actually requires.

## The four gates

```
Task arrives
│
├─ Creative / open-ended ("build X", "let's add", "I want to") ─────► BRAINSTORM gate
│
├─ Multi-step / multi-file / cascading unknowns ────────────────────► PLAN gate
│
├─ Bug / test failure / unexpected behavior ────────────────────────► DEBUG gate
│
└─ Any work that will be reported as done ──────────────────────────► VERIFY gate
```

Gates compose. A bug fix that touches 4 files needs DEBUG + PLAN + VERIFY. A "let's redesign auth" needs all four.

## Brainstorm gate — when intent is fuzzy

Triggers: "let's build", "I want to add", "could we", "what if", "redesign", "rewrite", any verb without a target file.

You **do not write code**. You ask three questions, in this order, and wait between each:

1. **What is the user actually trying to accomplish?** (Surface the goal, not the proposed solution.)
2. **What does success look like, concretely?** (One observable behavior, one acceptance criterion.)
3. **What's already there that this needs to fit with?** (Constraint, existing pattern, deadline.)

Skip if the user has already answered all three in the request.

## Plan gate — when work spans steps

Triggers: ≥3 files, ≥2 layers (model + handler, hook + UI, etc.), unclear order of operations, anything calling itself "a refactor", anything where step 4 depends on step 2.

Write a plan **before** the first edit. Required structure:

```
Goal:        <one sentence — observable outcome>
Files:       <each file you intend to touch + role>
Order:       <numbered steps, each independently verifiable>
Verify:      <the exact command(s) that prove each step works>
Out of scope: <what you are deliberately NOT doing>
```

Save to `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md` (gitignored). The plan is reviewable. Implementation without one drifts.

## Debug gate — before proposing a fix

Triggers: "broken", "not working", test failure, exception, "X is wrong", "Y returns the wrong thing".

**The order is non-negotiable:**

| Step | What | Why |
|---|---|---|
| 1 | Reproduce | If you can't reproduce, you can't fix |
| 2 | Minimize | Smallest input that triggers it |
| 3 | Hypothesize | One specific cause, falsifiable |
| 4 | Verify the hypothesis | Read the code path or add a probe |
| 5 | Fix | Only after 1–4 |

The most common failure mode: skipping to step 5 because the fix "looks obvious". Resist. Obvious fixes that fixed the wrong thing are how regressions ship.

## Verify gate — before claiming done

You may **not** say "done", "fixed", "passing", "working", or "ready" without:

1. Running the verification command (test, build, lint, the actual feature in a browser if UI).
2. Reading the output. Not glancing — reading.
3. Pasting the relevant line(s) of evidence into your reply.

If you can't run the verification (no permission, no env, no display), **say so explicitly**. "I edited the file but did not run the tests" is honest. "Done!" without evidence is a lie that costs the user a debugging session.

## Failure modes — name them and stop

| Symptom | What's actually happening | Recover by |
|---|---|---|
| You're 30 lines into the edit and just read the file | Skipped Plan gate | Stop, write the plan, throw away the edit if needed |
| The fix "looks obvious" | Skipped Debug gate | Reproduce first |
| You typed "this should work" | About to skip Verify gate | Run it. If you can't, say "untested" |
| You're asking the user three clarifying questions in one message | Should be Brainstorm gate, one question at a time | Send one. Wait. |
| You're about to invoke 4 tools in sequence with the same input | Likely should be parallel | Single turn, multiple tool calls |
| The plan keeps growing as you implement | Plan was a wishlist, not a plan | Cut to the next verifiable step; defer the rest |

## Interaction with parallel tools and subagents

Discipline gates and parallelism are not opposed. The plan tells you **what** is independent; parallel tool calls and subagents are **how** you execute the independent parts. Order is: gate → plan → identify independent units → dispatch in parallel.

## What this skill does NOT replace

- The CLAUDE.md template's classify/route/approach/execute/verify loop. This skill is the *teeth* of that loop, not a substitute.
- Domain skills (`debug-ops`, `refactor-ops`, `code-review`, `testgen`). Those are the *content*; this is the *order*.
- Memory autonomy. Save what you learn during gates — corrections at the brainstorm gate are pure feedback memory.
