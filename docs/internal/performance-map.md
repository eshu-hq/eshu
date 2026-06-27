# Performance Map

The performance engineer's index. Load this and you know **where everything
is** — the metrics, the tuning knobs, the proof harnesses — without hunting.

This map is a **pointer index, not a fact sheet.** It names the sources of
truth and the knobs; the exact metric names, knob values, and thresholds live
in the linked docs and code so this file cannot go stale. When a number
matters, read it from the cited source — never trust a copy.

Used by the `perf-eshu` agent (see
[Agent Orchestration Model](agent-orchestration.md)). The discipline is always
the same: **measure → locate → hypothesize → tune one knob → re-measure →
prove no regression.** Accuracy, performance, concurrency — in that order.

## Pipeline → owning packages → where time goes

Flow: `sync → discover → parse → emit facts → enqueue → reducer → projection →
query`. Ownership table: `docs/internal/agent-guide.md` (service boundaries).
The performance-critical stages:

| Stage | Owning package(s) under `go/internal/` | Watch (metric family) |
| --- | --- | --- |
| parse | `parser/<lang>/` | fact emit duration, per-language counts |
| emit facts | `collector/git_source_processing.go` | `*_facts_*_total`, fact emit duration |
| enqueue / claim | `projector/`, `storage/postgres` queue | queue depth, queue wait, oldest age |
| reducer | `reducer/` | reducer run duration, queue wait |
| shared projection | `reducer/shared_projection_runner.go` | intent wait, processing/step seconds |
| graph write | `storage/cypher/` | canonical/phase duration, backpressure, batch size, deadlock retries |
| query / read | `query/`, `mcp/` | API request duration (p95/p99), errors |

## Telemetry: the metric index

**Sources of truth — read these, do not memorize metric names:**

- `docs/public/observability/telemetry-coverage.md` — the X1 contract: a
  machine-parseable `stage | file:line | metric | category` table. This is the
  authoritative stage→metric map.
- `go/internal/telemetry/instruments.go` — every `eshu_dp_*` definition and the
  histogram bucket boundaries.
- `go/internal/telemetry/contract.go` (+ `contract_*.go`) — span names, log
  keys, dimensions (closed label sets — watch cardinality).
- `docs/public/reference/telemetry/index.md` — operator interpretation (queue
  age = freshness, not completion; signal order metrics → logs → traces).

Perf-critical metric **families** (exact names in the X1 doc): `*_duration_seconds`
histograms (latency), `*_total` counters (throughput), queue depth / wait /
oldest-age, graph-write backpressure, batch sizes, deadlock retries, and the
generation-liveness / active-generations gauges.

**Dashboard:** `docs/public/observability/dashboards/eshu-operator-overview.json`
(regenerate via `scripts/generate-operator-dashboard.sh` +
`scripts/lib/operator-dashboard-*.sh`). The headline "is Eshu healthy?" signal
is stuck-generation age — the 3 AM alarm.

## The tuning stack — what to turn, where the knobs live

NornicDB is the **default** canonical backend; Neo4j is compatibility only.

- **NornicDB knobs** → `docs/public/reference/nornicdb-tuning.md`. The knobs
  (values in the doc): `ESHU_CANONICAL_WRITE_TIMEOUT`,
  `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS`,
  `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS`, `ESHU_NORNICDB_FILE_BATCH_SIZE`,
  `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY`, plus per-entity-label phase-group
  caps and batch sizes. Pitfalls (constraint recreation on a live store,
  concurrent MERGE at commit-time uniqueness) →
  `docs/public/reference/nornicdb-pitfalls.md`. Runtime mode (embedded vs
  process) → `docs/public/reference/graph-backend-installation.md`.
- **Cypher (both backends)** → `docs/public/reference/cypher-performance.md`:
  hot-path eligibility, selectivity, index/constraint presence, batch + deadline
  budgets, and the proof ladder. Query-plan guardrail:
  `go/internal/queryplan/validator.go` + the fixture
  `go/internal/queryplan/testdata/hot-cypher.yaml`. Skill: `cypher-query-rigor`.
- **Postgres** → **gap (see below).** Owning package: `go/internal/storage/postgres`
  (+ its `README.md`); design inventory: `docs/internal/design/1286-postgres-ownership-inventory.md`.
  There is no operator-facing Postgres tuning doc yet.
- **Diagnostic doctrine** → skill `eshu-diagnostic-rigor`: the evidence ladder,
  "queue wait ≠ need more concurrency", conflict-domain correctness, NornicDB
  query-shape sensitivity. Concurrency surfaces → `concurrency-deadlock-rigor`.

## Proof harnesses — prove it, no regression

| Harness | Asserts | Run |
| --- | --- | --- |
| `scripts/verify-performance-evidence.sh` | Hot-path file changes carry a `Performance`/`Benchmark`/`No-Regression Evidence:` marker + an observability marker (CI gate). | `ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash scripts/verify-performance-evidence.sh` |
| `scripts/verify-query-plan-regression.sh` | Hot Cypher fixture obeys the NornicDB schema contract (no unbounded traversals, unlabeled anchors, unordered pagination). | `cd go && go test ./internal/queryplan -count=1` |
| `scripts/verify-scale-corpus-suite.sh` | The scale-lab corpus contract (`specs/scale-lab-corpus.v1.yaml`): slots, domains, and the metric set. | `bash scripts/verify-scale-corpus-suite.sh --spec specs/scale-lab-corpus.v1.yaml` |
| `scripts/verify-scale-benchmark-artifact.sh` | A benchmark result JSON (`specs/scale-benchmark-artifact.v1.yaml`): schema, before/after, backend matrix, privacy. | `bash scripts/verify-scale-benchmark-artifact.sh --spec specs/scale-benchmark-artifact.v1.yaml --artifact <file>` |

**Go benchmarks** (start here for materialization hot spots):
`go/internal/reducer/*_bench_test.go`,
`go/internal/collector/git_snapshot_delta_bench_test.go`.

## Evidence rules (binding)

Performance work MUST have **before/after measurements on the same backend**,
name the baseline, and carry a `Performance`/`No-Regression Evidence:` marker
plus an observability marker — `verify-performance-evidence.sh` enforces this in
CI for hot-path changes. Never claim a speedup without pasted numbers. Never
"optimize" code not yet proven correct. Never serialize to hide a concurrency
defect — partition by conflict key or make the write idempotent.

## Known gaps (honest — these are where you propose new docs)

1. **No Postgres operator tuning doc** — index strategy, connection-pool knobs,
   and query-plan guidance for `storage/postgres` are undocumented. When you
   tune Postgres, propose a `docs/public/reference/postgres-tuning.md`.
2. **No published SLO / performance contract** — the scale-corpus spec defines
   *which* metrics to measure (fact rows/sec, queue-claim p95, reducer drain,
   graph-write p95, api/mcp p95) but not target thresholds. Treat "baseline or
   known-normal timing" as the bar until an SLO doc lands.

## Operator-only

Remote full-corpus validation is a **user-local** skill (`eshu-remote-validation`),
not committed to this repo — the operator triggers it. In-repo, use the proof
harnesses above; do not assume the remote path is available to a committed agent.
