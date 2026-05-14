---
name: code-review
model: sonnet
context: fork
agent: Explore
allowed-tools: Read Grep Glob Bash
description: "Comprehensive code review with severity classification, expert routing by language, and auto-fix suggestions. Use for: code review, review changes, review PR, review diff, review staged, review commit, security review, performance review."
---

# Code Review

## Workflow

1. **Determine Scope** - staged changes, all uncommitted, specific file, or PR diff
2. **Analyze Changes** - categorize as logic, style, test, docs, or config
3. **Load Project Standards** - read CLAUDE.md, linting configs, test framework
4. **Review by Domain** - apply language-specific expertise
5. **Generate Report** - structured findings with severity levels
6. **Track Issues** - create tasks for critical findings

## Scope Detection

```bash
# Default: staged changes
git diff --cached --stat

# All uncommitted changes
git diff HEAD --stat

# PR diff
gh pr diff $PR_NUMBER --stat

# Branch diff vs main
git diff main...HEAD --stat
```

## Severity System

| Level | Meaning | Action |
|-------|---------|--------|
| CRITICAL | Security bug, data loss risk, crashes | Must fix before merge |
| WARNING | Logic issues, performance problems | Should address |
| SUGGESTION | Style, minor improvements | Optional |
| PRAISE | Good patterns worth noting | Recognition |

## Review Checklist by Focus

### Security
- SQL injection, XSS, command injection
- Secrets/credentials in code
- Authentication/authorization gaps
- Input validation missing
- Insecure defaults

### Performance
- N+1 queries
- Unnecessary re-renders
- Missing indexes
- Unbounded queries
- Memory leaks

## MCP-Augmented Review

Use MCP tools to get authoritative signal before reporting findings:

| Language | Check | MCP |
|---|---|---|
| TypeScript | Type errors, diagnostics, dead references | `lsmcp` → `lsp_get_diagnostics` |
| Go | Vet errors, build failures | `godevmcp` → build/vet check |
| Any | Dependency vulnerabilities | `brave-search` for CVE lookup |
| CI | Pipeline status on the PR branch | `buildkite` for build state |
| Infra changes | Terraform resource schema validity | `terraform` provider docs |

Run `lsmcp` or `godevmcp` checks first — surface real compiler/LSP errors before reviewing style or logic.

### Logic
- Off-by-one errors
- Null/undefined handling
- Race conditions
- Error handling gaps
- Edge cases

### Style
- Naming clarity
- Dead code
- Consistency with codebase
- Comments where non-obvious

## Report Format

```markdown
# Code Review: [scope]

## Summary
| Metric | Value |
|--------|-------|
| Files reviewed | N |
| Lines changed | +X / -Y |
| Issues found | N (X critical, Y warnings) |

## Verdict
**Ready to commit?** Yes / No

## Critical Issues
### `file.ts:42`
**Issue:** Description
**Risk:** Impact
**Fix:**
\`\`\`diff
- old code
+ new code
\`\`\`

## Warnings
[...]

## Suggestions
[...]

## Praise
[Good patterns worth noting]
```

## Red Flags (Immediate Escalation)

- Removed security-related code
- Access control downgraded
- Validation removed without replacement
- External calls added without error handling
- Secrets or credentials in code
- Changes affecting 50+ callers
