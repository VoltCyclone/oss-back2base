### Working in Go (profile: `go`)

**Save-worthy signals in Go work:** upstream API breakage, subtle `context`/goroutine behavior, `errors.Is`/`errors.As` nuances you had to debug, build-tag or cgo gotchas, generics-vs-interfaces choices, `go mod` tidy quirks, test double strategies (real vs. mock vs. `testcontainers`), pprof/benchmark findings.

- **feedback example:** *"Prefer `errors.Is` over string compare for wrapped errors. **Why:** string compare breaks the moment an upstream adds a new wrap layer. **How to apply:** any branch that inspects a returned error type."*
- **project example:** *"Service pins `k8s.io/client-go@v0.28` — do not bump without regen. **Why:** 0.29 changed CRD list response shape; reconcile tests broke silently. **How to apply:** check CRD schema diff before any `client-go` bump."*
- **reference example:** *"Shared Go style guide: wiki/engineering/go-style. Supersedes Uber guide where they conflict."*

**Conventions this profile assumes:** standard library `testing` + `testify/require` for assertions; `go.work` if multi-module; `gopls` via `lsmcp` MCP; `godevmcp` for quick doc lookups. Prefer reading whole files over grepping fragments — 1M context permits it.
