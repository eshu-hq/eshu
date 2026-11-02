# Telemetry Coverage Contract

This contract enumerates every observable stage in the Eshu data plane and the metric, span, or log key it must emit. This index and the part pages it links are the single source of truth that the CI coverage script (X2) diffs against. A new pipeline stage added to the source tree without a corresponding entry here fails the coverage gate. The five evidence markers policy (`Performance Evidence:`, `Benchmark Evidence:`, `No-Regression Evidence:`, `Observability Evidence:`, `No-Observability-Change:`) at `docs/internal/agent-guide.md:120-146` remains the per-PR discipline; this doc makes that discipline machine-enforced. Metric names match `go/internal/telemetry/instruments.go`; dimensions, span names, and log keys match `go/internal/telemetry/contract.go` and the per-family files under `go/internal/telemetry/contract/` (plus its `observability/` and `thirdparty/` subpackages). The public operator contract is `docs/public/reference/telemetry/index.md`. The schema migration `bootstrap.postgres.migration.lock_wait` and `lock_recovered` events remain the operator signals; `budget_left_ms` reports the remaining shared allowance after failed-attempt time and backoff, excludes successful migration time, and is not a strict server-side deadline because PostgreSQL applies `lock_timeout` per lock acquisition. How the coverage gate reads this page (diff semantics, the `No-Observability-Change:` marker, stages, linked work, and affected flow) is in [Telemetry Coverage Notes](telemetry-coverage-notes.md).

## How To Read This Doc

- Each stage table is machine-parseable: stable column order `stage | file:line | required metric name(s) | category`, one row per stage. The metric column lists every registered name an operator needs to read at 3 AM, comma-separated. The category column names the owning sub-package or seam.
- `file:line` points at the Go file that emits the signal, not the contract. When the source spans several call sites, it points at the dispatch chokepoint.
- The script (X2) grep-parses these tables; do not rename columns or insert prose between rows in a section.
- The rows live in the part pages below. The gate reads this index and every `telemetry-coverage-*.md` page beside it except [Telemetry Coverage Notes](telemetry-coverage-notes.md), and fails when a part is not linked from this list.
- Add a new row to the part that owns the stage, in the section that matches it. Keep every page under the 500-line Markdown cap: when a part nears the cap, split it into a new `telemetry-coverage-<area>.md` part and link it here.

## Coverage Parts

- [Reducer Runtime And Code Graph](telemetry-coverage-reducer-core.md): the first half of the reducer stage table: queue claim, admission, intent enqueue, shared projection, graph projection, cross-repo, search, code-call, and value-flow stages.
- [Reducer Domain Projection And Typed Decode](telemetry-coverage-reducer-domains.md): the second half of the reducer stage table: cross-scope ownership, cloud, IAM, posture, drift, supply-chain, security, workload, graph-write, and typed-decode stages.
- [Projector And Parser Stages](telemetry-coverage-projector-parser.md): the projector stage table and the parser language sub-package table.
- [Collector Dispatch Seams](telemetry-coverage-collectors.md): the collector dispatch seam table: the claimed-service harness and every collector family's dispatch chokepoint.
- [Queues, API, Graph Writes, And Routes](telemetry-coverage-runtime-surfaces.md): the queue domain, API lifecycle, graph-write statement metadata, and MCP / API route tables.
- [Span Names, Log Keys, And Histogram Buckets](telemetry-coverage-signals.md): the OTEL span name table, the structured log key table, and the histogram bucket boundary table the gate compares with `go/internal/telemetry/instruments.go`.
