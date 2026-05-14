---
name: perf-ops
model: opus
context: fork
agent: general-purpose
allowed-tools: Read Grep Glob Bash
description: "Performance profiling and optimization - diagnoses symptoms, dispatches profiling by language, manages before/after comparisons. Use for: performance, profiling, flamegraph, slow, latency, throughput, memory leak, CPU spike, bottleneck, benchmark, load test, N+1, caching, optimization."
---

# Performance Operations

## Diagnosis Decision Tree

```
What kind of performance issue?
│
├─ CPU-bound (high CPU, slow computation)
│  ├─ Profile with flamegraph
│  ├─ Look for: hot loops, redundant computation, bad algorithms
│  └─ Tools: py-spy, pprof, cargo-flamegraph, clinic flame
│
├─ Memory-bound (high memory, OOM, leaks)
│  ├─ Track allocations over time
│  ├─ Look for: growing collections, unclosed resources, caches without eviction
│  └─ Tools: memray, heaptrack, pprof heap, Chrome DevTools
│
├─ I/O-bound (waiting on network, disk, DB)
│  ├─ Check query performance (EXPLAIN ANALYZE)
│  ├─ Look for: N+1 queries, missing indexes, no connection pooling
│  └─ Tools: pg_stat_statements, strace, tcpdump
│
├─ Concurrency (deadlocks, contention)
│  ├─ Profile lock contention
│  ├─ Look for: mutex hotspots, goroutine leaks, thread pool exhaustion
│  └─ Tools: pprof mutex, go trace, py-spy --threads
│
└─ Frontend (slow page load, janky UI)
   ├─ Bundle size analysis
   ├─ Look for: large dependencies, unnecessary re-renders, layout thrashing
   └─ Tools: webpack-bundle-analyzer, Lighthouse, React DevTools Profiler
```

## Profiling Tools by Language

| Language | CPU | Memory | Tracing |
|----------|-----|--------|---------|
| Python | py-spy, scalene | memray, tracemalloc | cProfile |
| Go | pprof (CPU) | pprof (heap) | go tool trace |
| Rust | cargo-flamegraph, samply | DHAT | criterion |
| Node.js | clinic flame, 0x | clinic heap | clinic doctor |
| Frontend | Lighthouse | Chrome DevTools | Performance tab |

## Quick Profiling Commands

### Python
```bash
py-spy record -o profile.svg -- python app.py      # CPU flamegraph
py-spy top -- python app.py                          # Live top-like view
memray run -o output.bin app.py                      # Memory tracking
memray flamegraph output.bin                         # Memory flamegraph
```

### Go
```bash
go test -bench=. -cpuprofile=cpu.prof ./...
go tool pprof -http=:8080 cpu.prof                   # Web UI
curl http://localhost:6060/debug/pprof/heap > heap.prof
```

### Node.js
```bash
clinic flame -- node server.js                       # CPU flamegraph
clinic doctor -- node server.js                      # Overall health
clinic bubbleprof -- node server.js                  # Async bottlenecks
```

## Benchmarking

### CLI Benchmarks
```bash
hyperfine 'command1' 'command2'                      # Compare commands
hyperfine --warmup 3 'npm run build'                 # With warmup
```

### Load Testing
```bash
k6 run loadtest.js                                   # k6
artillery quick --count 100 -n 50 http://localhost:3000  # Artillery
echo "GET http://localhost:3000" | vegeta attack -duration=30s | vegeta report
```

## Database Performance

```sql
-- Find slow queries
EXPLAIN ANALYZE SELECT * FROM users WHERE email = 'test@test.com';

-- Check missing indexes
SELECT schemaname, tablename, indexrelname, idx_scan
FROM pg_stat_user_indexes WHERE idx_scan = 0;

-- Find N+1 patterns
-- Look for repeated similar queries in logs
```

## Optimization Checklist

- [ ] Profile first (don't guess)
- [ ] Record baseline metrics before changes
- [ ] Change one thing at a time
- [ ] Re-measure after each change
- [ ] Verify correctness wasn't broken
- [ ] Document the optimization and metrics delta

## MCP-Augmented Profiling

Use MCPs to get real data before profiling locally:

| Situation | MCP | What to fetch |
|---|---|---|
| Production latency / throughput | `datadog` | APM service map, p99 latency, slow traces |
| Production memory growth | `datadog` | Container memory metrics over time |
| Go CPU hotspots | `godevmcp` | Call graph to identify hot paths before profiling |
| TypeScript bundle size | `lsmcp` | Symbol references to find large import chains |
| Database slow queries | `datadog` | DBM query analytics for top slow queries |
| Kubernetes resource pressure | `kubernetes` | `top nodes`, `top pods` for real utilization |

Start with Datadog APM traces for production issues — they show the actual bottleneck without needing to reproduce locally.

## Safety Rules

- **Production**: Only use sampling profilers (py-spy, pprof HTTP)
- **Never**: Attach debuggers or tracing profilers to production
- **Always**: Get baseline before optimizing
- **Always**: Verify correctness after optimizing
