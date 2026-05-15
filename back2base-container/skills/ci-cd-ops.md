---
name: ci-cd-ops
model: sonnet
context: fork
agent: Plan
paths: ".github/workflows/**,Jenkinsfile,.circleci/**,.gitlab-ci.yml"
description: "CI/CD pipeline patterns with GitHub Actions, release automation, and testing strategies. Use for: github actions, workflow, CI, CD, pipeline, deploy, release, semantic release, matrix, cache, secrets, environment, artifact, reusable workflow."
---

# CI/CD Operations

## GitHub Actions Workflow Anatomy

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read
  pull-requests: write

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 20
          cache: npm
      - run: npm ci
      - run: npm test
```

## Trigger Decision Tree

| Scenario | Trigger | Config |
|----------|---------|--------|
| Tests on PR | `pull_request` | `branches: [main]` |
| Deploy on merge | `push` | `branches: [main]` |
| Release on tag | `push` | `tags: ['v*']` |
| Nightly builds | `schedule` | `cron: '0 2 * * *'` |
| Manual deploy | `workflow_dispatch` | `inputs: { env: ... }` |

## Caching

| Ecosystem | Setup |
|-----------|-------|
| Node (npm) | `actions/setup-node` with `cache: npm` |
| Go | `actions/setup-go` with `cache: true` |
| Python/pip | `actions/setup-python` with `cache: pip` |
| Cargo | Manual `actions/cache@v4` with Cargo.lock hash |
| Docker | `docker/build-push-action` with `cache-from: type=gha` |

## Matrix Strategy

```yaml
strategy:
  fail-fast: false
  matrix:
    os: [ubuntu-latest, macos-latest]
    node-version: [18, 20, 22]
    include:
      - os: ubuntu-latest
        node-version: 22
        coverage: true
```

## Secrets Management

| Scope | Use Case |
|-------|----------|
| Repository secrets | API keys, tokens |
| Environment secrets | Production credentials (with approvals) |
| OIDC tokens | Cloud deployment (no stored secrets) |

```yaml
# OIDC for AWS (no stored secrets)
permissions:
  id-token: write
steps:
  - uses: aws-actions/configure-aws-credentials@v4
    with:
      role-to-assume: arn:aws:iam::123456789:role/github-actions
```

## Common Patterns

### Deploy on Merge
```yaml
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - uses: actions/checkout@v4
      - run: npm ci && npm run build
      - run: npx wrangler deploy
```

### Release on Tag
```yaml
on:
  push:
    tags: ['v*']
jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - run: gh release create ${{ github.ref_name }} --generate-notes
```

## MCP-Augmented CI/CD

Use MCPs to get live pipeline state before editing workflow files:

| Situation | MCP | What to fetch |
|---|---|---|
| Build failing on a branch | `buildkite` | Fetch the failing job log directly |
| Check PR status / checks | `github` | PR checks, status, review state |
| Deploy failing in prod | `datadog` | Error spike correlating with deploy time |
| Kubernetes rollout issues | `kubernetes` | Rollout status, pod events, describe deployment |
| Terraform plan in pipeline | `terraform` | Provider docs to validate resource config |

Always pull the actual build log from `buildkite` before guessing why a pipeline is failing.

## Gotchas

| Gotcha | Fix |
|--------|-----|
| Shallow clone breaks git describe | `fetch-depth: 0` |
| Secrets unavailable on fork PRs | Use `pull_request_target` carefully |
| Concurrent deploys race condition | Use `concurrency:` groups |
| GITHUB_TOKEN can't trigger workflows | Use PAT or GitHub App token |
| Path filters skip required checks | Use `paths-filter` action |
| Matrix + environment = N approvals | Single deploy job after matrix |

## Expressions Quick Reference

| Expression | Result |
|------------|--------|
| `${{ github.ref_name }}` | Branch or tag name |
| `${{ github.sha }}` | Commit SHA |
| `${{ github.actor }}` | Triggering user |
| `${{ hashFiles('**/package-lock.json') }}` | Cache key hash |
| `${{ needs.build.outputs.version }}` | Cross-job output |
