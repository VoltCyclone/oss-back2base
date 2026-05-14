### Working in research/exploration (profile: `research`)

**Save-worthy signals in research work:** sources that turned out reliable (or not), search strategies that surfaced the real answer after several misses, citation/credibility findings that should inform future lookups, terminology disambiguations ("X in context A ≠ X in context B"), and any conclusions the user validated so future you can skip the re-investigation.

- **feedback example:** *"For latency-related questions, check Grafana before checking docs. **Why:** the docs describe intended behavior; Grafana shows actual. **How to apply:** any 'is X slow?' question."*
- **project example:** *"We chose OpenAPI 3.1 over 3.0 after evaluating tooling support. **Why:** 3.1's JSON Schema alignment wins long-term; tooling gap was manageable. **How to apply:** reject PRs that still target 3.0 for new specs."*
- **reference example:** *"Primary source for OTEL spec: opentelemetry.io/docs/specs — secondary commentary on the GH discussions tracker."*

**Conventions this profile assumes:** Brave Search and Context7 MCP as primary lookup tools; `fetch` for deep dives on a single URL; `sequential-thinking` when the question has cascading unknowns. Favor one broad search + refine over many narrow searches.
