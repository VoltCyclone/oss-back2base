---
name: git-ops
model: sonnet
allowed-tools: Bash Read
description: "Git operations orchestrator with safety tiers - read-only ops run inline, safe writes gather context, destructive ops require confirmation. Use for: git, commit, push, pull, branch, merge, rebase, cherry-pick, stash, tag, release, PR, pull request, worktree, bisect."
---

# Git Operations

## Safety Tiers

### Tier 1: Read-Only (Execute Inline)
- `git status`, `git log`, `git diff`, `git show`
- `git branch -l`, `git remote -v`, `git stash list`
- `git blame`, `git shortlog`, `git describe`

### Tier 2: Safe Writes (Gather Context First)
- `git commit` - check staged files, run pre-commit hooks
- `git push` - verify remote, check if force needed
- `git branch -c` - create new branch
- `git stash` - save work in progress
- `git tag` - create version tag
- `gh pr create` - create pull request

### Tier 3: Destructive (Require Confirmation)
Show preflight report with recovery options before:
- `git rebase` - show commit range and conflicts risk
- `git push --force` - show what will be overwritten
- `git reset --hard` - show what will be lost
- `git branch -D` - confirm branch has been merged or backed up
- `git clean -fd` - show what files will be deleted

## Common Workflows

### Feature Branch
```bash
git checkout -b feature/name main
# ... work ...
git add -p                         # Stage interactively
git commit -m "feat: description"
git push -u origin feature/name
gh pr create --fill
```

### Commit Message Convention
```
type(scope): description

Types: feat, fix, docs, style, refactor, perf, test, chore, ci
```

### Stash Workflow
```bash
git stash push -m "WIP: feature X"  # Save with description
git stash list                       # See all stashes
git stash pop                        # Apply and remove latest
git stash apply stash@{2}           # Apply specific stash
git stash drop stash@{0}            # Remove specific stash
```

### Interactive Rebase (Cleanup Before PR)
```bash
git rebase -i HEAD~5               # Squash/reorder last 5 commits
# Change 'pick' to 'squash' for commits to combine
# Change 'pick' to 'reword' to edit message
```

### Cherry-Pick
```bash
git cherry-pick abc123             # Apply single commit
git cherry-pick abc123..def456     # Apply range
git cherry-pick --no-commit abc123 # Stage without committing
```

### Worktree (Parallel Development)
```bash
git worktree add ../feature-branch feature/name
# Work in ../feature-branch independently
git worktree remove ../feature-branch
```

### Bisect (Find Bug Introduction)
```bash
git bisect start
git bisect bad                     # Current is broken
git bisect good v1.0.0             # This was working
# Test each checkout, mark good/bad
git bisect run npm test            # Automated
git bisect reset                   # Done
```

## Useful Aliases

```bash
git log --oneline --graph --all    # Visual branch history
git diff --stat                    # Summary of changes
git log --author="name" --since="1 week ago"
git shortlog -sn                   # Contributor stats
```

## Recovery

| Situation | Recovery |
|-----------|----------|
| Undo last commit (keep changes) | `git reset --soft HEAD~1` |
| Undo staged files | `git restore --staged .` |
| Recover deleted branch | `git reflog` then `git checkout -b name SHA` |
| Undo a rebase | `git reflog` find pre-rebase HEAD, `git reset --hard SHA` |
| Recover stash after drop | `git fsck --unreachable \| grep commit` |
