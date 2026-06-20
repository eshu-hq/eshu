# Console Bundle Budget

The console (`apps/console`) is a single-page operator UI built with Vite. To
keep first-load weight visible and prevent silent regressions, the build has a
documented per-chunk **bundle budget** that is checked after every build.

## How it works

1. `npm run console:build` builds the console into `apps/console/dist`.
2. The same `console:build` script then runs `npm run console:bundle-budget`,
   which executes `scripts/console-bundle-budget.mjs`, reads the emitted
   JavaScript chunks under `apps/console/dist/assets`, classifies each chunk to
   a stable budget key, and fails (exit code `1`) if any chunk exceeds its
   documented threshold.

Run the required build path locally or in CI:

```bash
npm run console:build
```

Sizes are **raw, un-gzipped minified bytes** — the same number Vite prints in
its build summary — so the budget output and the Vite log line up.

## Chunk strategy

Heavy code is kept off the critical first-load path:

- **Diagram / graph libraries** (`mermaid`, `cytoscape`, `wardley`, `katex`) are
  lazy-loaded on demand. Mermaid is imported dynamically
  (`apps/console/src/components/ask/mermaid.ts`), so it and its transitive
  dependencies (cytoscape, wardley, katex, and the per-diagram chunks) only
  download when a user actually renders a diagram artifact.
- **d3** (force simulation, scales, shapes) is used only by the workspace and
  service-relationship views. The `/workspace/:entityKind/:entityId` route is
  code-split with `React.lazy` + `Suspense` in `apps/console/src/App.tsx`, and
  d3 is given its own `manualChunk`, so d3 downloads with that route instead of
  in the main entry chunk. The Suspense fallback preserves the existing
  "Loading workspace" state.
- **Vendor code** (`react`, `react-dom`, `react-router-dom`) is split into a
  `react-vendor` chunk and `lucide-react` into an `icons` chunk via
  `manualChunks` for stable, cache-friendly downloads.

The **main entry chunk** intentionally remains the largest first-load chunk: it
holds the app shell, the router, and the eagerly imported page set (~40 routed
pages of business logic). Most pages are reachable directly from the primary
nav, so eager loading keeps navigation instant. The heavy *dependencies* listed
above have been split out; what remains is application code.

## Budgets

Budgets live in `BUDGETS` and `DEFAULT_ASYNC_BUDGET_BYTES` in
`scripts/console-bundle-budget.mjs`. Each threshold has deliberate headroom over
the current measured size so ordinary churn does not trip the gate but a large
regression does.

| Budget key     | Threshold | What it covers                                              |
| -------------- | --------- | ----------------------------------------------------------- |
| `main`         | 720 KiB   | Main entry chunk: app shell, router, eagerly loaded pages   |
| `react-vendor` | 120 KiB   | `react`, `react-dom`, `react-router-dom`                    |
| `d3`           | 200 KiB   | d3 (lazy, loaded with the workspace route)                  |
| `icons`        | 80 KiB    | `lucide-react` icon set                                     |
| `mermaid`      | 900 KiB   | Mermaid core (lazy diagram chunk)                           |
| `cytoscape`    | 700 KiB   | Cytoscape (lazy, pulled in by Mermaid)                      |
| `wardley`      | 900 KiB   | Wardley map diagram (lazy)                                  |
| `katex`        | 400 KiB   | KaTeX math rendering (lazy, pulled in by Mermaid)           |
| _(default)_    | 700 KiB   | Any other async chunk (e.g. an individual lazy diagram)     |

Vite's generic 500 kB chunk-size warning is raised to `720` (the main-chunk
budget) in `apps/console/vite.config.ts` so the build log is not noisy about the
intentionally eager main chunk and the lazy diagram libraries. The bundle budget
script is the authoritative gate, not the Vite warning.

## Changing a threshold

When a chunk legitimately needs to grow:

1. Reduce it first if you can (lazy-load or split the offending code).
2. If the growth is justified, raise the threshold in
   `scripts/console-bundle-budget.mjs`, update the table above in the same PR,
   and explain why in the PR description.

Never raise a threshold just to make a red gate green without understanding why
the chunk grew.
