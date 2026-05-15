---
name: refactor-ops
model: sonnet
context: fork
agent: general-purpose
description: "Safe refactoring patterns - extract, rename, restructure with test-driven methodology and dead code detection. Use for: refactor, extract function, extract component, rename, move file, restructure, dead code, unused imports, code smell, duplicate code, DRY, cleanup, simplify."
---

# Refactoring Operations

## Refactoring Decision Tree

```
What kind of refactoring?
│
├─ Extracting code
│  ├─ Block with clear purpose → Extract Function/Method
│  ├─ UI element with state → Extract Component
│  ├─ Reusable stateful logic → Extract Hook/Composable
│  ├─ File >300-500 lines → Extract Module
│  ├─ Class doing too much → Extract Class/Service
│  └─ Magic numbers/hardcoded values → Extract Configuration
│
├─ Renaming
│  ├─ Variable/function → IDE rename or ast-grep
│  ├─ File/directory → git mv + update imports
│  └─ Module/package → Rename + re-export from old name temporarily
│
├─ Moving code
│  ├─ Function to different file → Move + re-export from original
│  ├─ Files to different dir → git mv + update all paths
│  └─ Directory restructure → Incremental migration, one module at a time
│
├─ Simplifying
│  ├─ Trivial wrapper → Inline Function
│  ├─ Single-use variable → Inline Variable
│  ├─ Deep nesting → Guard Clauses + Early Returns
│  └─ Complex conditionals → Decompose into named functions
│
└─ Removing dead code
   ├─ Unused imports → Lint + auto-fix (eslint, ruff, goimports)
   ├─ Unreachable code → Static analysis
   ├─ Orphaned files → knip, ts-prune, vulture
   └─ Unused exports → ts-prune or grep for imports
```

## Safety Checklist

**Before:**
- [ ] All tests pass
- [ ] Working tree is clean
- [ ] On a dedicated branch
- [ ] You understand what the code does

**During:**
- [ ] Each commit compiles and passes tests
- [ ] One refactoring per commit
- [ ] No behavior changes mixed with structural changes

**After:**
- [ ] Full test suite passes
- [ ] No new linter/type warnings
- [ ] Performance benchmarks unchanged (if applicable)

## Code Smell Detection

| Smell | Heuristic | Refactoring |
|-------|-----------|-------------|
| Long function | >20 lines or >5 indent levels | Extract Function |
| God object | >10 methods or >500 lines | Extract Class |
| Feature envy | Uses another object's data more than its own | Move Method |
| Duplicate code | Same logic in 2+ places | Extract Function/Module |
| Deep nesting | >3 levels | Guard Clauses, Early Returns |
| Long parameter list | >4 parameters | Extract Parameter Object |
| Primitive obsession | Strings/numbers where types would be safer | Value Objects, Enums |

## Test-Driven Refactoring

1. **Characterization tests** - capture current behavior as safety net
2. **Verify coverage** - ensure all paths you'll touch are covered
3. **Refactor in small steps** - run tests after every change
4. **Improve tests** - replace characterization tests with intention-revealing ones
5. **Commit separately** - tests first, then refactoring

## Dead Code Detection

| Language | Tool | Command |
|----------|------|---------|
| TypeScript/JS | knip | `npx knip --reporter compact` |
| TypeScript | ts-prune | `npx ts-prune` |
| Python | vulture | `vulture src/ --min-confidence 80` |
| Python | ruff | `ruff check --select F401` |
| Go | golangci-lint | `golangci-lint run --enable unused,deadcode` |
| Rust | Compiler | `cargo build 2>&1 \| grep 'warning.*unused'` |

## Common Gotchas

| Gotcha | Prevention |
|--------|------------|
| Behavior change mixed with refactor | Separate commits |
| Breaking public API | Re-export from old path with deprecation |
| Circular dependencies after extraction | Check dependency graph after each extraction |
| Git history lost after file move | Always use `git mv` |
| Over-abstracting (premature DRY) | Rule of three: wait for 3 duplicates |
| Tests pass but runtime breaks | Integration tests alongside unit tests |
