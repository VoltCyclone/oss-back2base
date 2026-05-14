### Working on the frontend (profile: `frontend`)

**Save-worthy signals in frontend work:** design-system decisions (why this primitive and not that), accessibility outcomes (aria-label vs. role vs. visually-hidden trade-offs), state-management choices with rationale, hydration/SSR gotchas, bundle-size findings, React Server Components vs. client decisions, testing-library vs. Playwright scope decisions.

- **feedback example:** *"Don't reach for `useMemo` unless profiling shows the re-render cost. **Why:** premature memoization adds dep-array bugs more often than it saves frames. **How to apply:** any component with static props from a parent."*
- **project example:** *"App uses Next.js App Router with `server-only` markers on all data loaders. **Why:** prevents accidental fetch() calls from leaking into client bundles after refactors. **How to apply:** new data-loading code must start with `import 'server-only'`."*
- **reference example:** *"Design tokens in `packages/tokens/` are the source of truth — Tailwind config imports from there; do not hardcode colors in component classes."*

**Conventions this profile assumes:** TypeScript strict mode; `lsmcp` MCP for component navigation; component tests via Testing Library, e2e via Playwright. Check the design system before writing a new primitive.
