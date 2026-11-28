# Cypher Performance Discipline

Use this page before changing hot-path Cypher, graph schema, graph-write
batching, reducer projection, query handlers, materialization jobs, or pinned
graph backend versions. Maintainer implementation details live in
`go/internal/storage/cypher/README.md` and the `cypher-query-rigor` project
skill.

Accuracy comes first. A faster query that returns wrong graph truth is a
product failure.

## Mandatory Checks

Every hot-path Cypher change needs both checks before merge.

### 1. Research The Pinned Backend

Neo4j:

- Read the Cypher manual for the pinned major version.
- Check changelogs when planner or syntax behavior matters.
- Confirm recent features such as subqueries, dynamic labels, or vector indexes
  exist in the pinned version before using them.

NornicDB, Eshu's default backend:

- Use the current NornicDB-New checkout named by local config, repo docs, or the
  user. Do not rely on an older sibling checkout unless the run explicitly uses
  it.
- Read the relevant `pkg/cypher/` and `pkg/storage/` source for the production
  query shape.
- Check [NornicDB Pitfalls](nornicdb-pitfalls.md) and
  [NornicDB Tuning](nornicdb-tuning.md).

For both backends, prove any unfamiliar query pattern against the pinned binary
before designing production code around it. Record the backend version or
NornicDB-New commit in the PR evidence.

### 2. Measure The Same Shape Before And After

Unmeasured Cypher in a hot path is a regression risk. Capture before/after
evidence against the same inputs and pinned backend binary.

Preferred proof ladder:

| Shape | Use when | Evidence |
| --- | --- | --- |
| Focused Go benchmark | Writer code lives under `go/internal/storage/cypher` or a narrow adapter. | `go test -bench=. -benchmem`, with `ops/sec`, `ns/op`, and `B/op`. |
| Compose-stage timing | Query fires only through reducer, projector, or bootstrap flows. | Structured log duration, input size, output count, queue state, backend, and schema state. |
| Manual reproducer | Admin or one-off materialization query. | Wall time, row count, dataset shape, backend version, and schema/index state. |

Record:

- backend and version or image tag
- whether `eshu-bootstrap-data-plane` applied schema first
- input cardinality at each anchor
- indexes and constraints present
- Neo4j `PROFILE` or NornicDB statement summaries when available

Correctness-only Cypher changes still need a same-shape no-regression check.
If a benchmark is not load-bearing, say why in the tracked evidence note.

## CI Evidence Gate

`scripts/verify-performance-evidence.sh` checks changed hot-path Go files,
graph writes, collectors, workers, leases, batching, concurrency primitives,
Compose, Helm, pprof, and NornicDB knobs.

`scripts/verify-query-plan-regression.sh` validates the static hot-path
query-plan fixtures under `go/internal/queryplan/testdata/` against the NornicDB
schema statement contract. The gate also parses every non-test Go file
recursively beneath `go/internal/query` and compares each `Run` or `RunSingle` owner against
`query-source-coverage.yaml` by file, enclosing symbol, and exact call count.
Every callsite must link to registered hot entries or carry an explicit non-hot
disposition. Typed non-hot dispositions freeze the reviewed source symbol and
declare machine-checked key/result bounds. Grandfathered prose dispositions are
immutable source-digest records: new or changed source must use the typed form.
Hot dispositions also freeze the full graph-executing production symbol, so
retaining the same `Run` count while rerouting to another query fails the gate.
Handler and legacy entries contain no copied Cypher. Handler entries bind exact
query and builder-source SHA-256 values plus an anchor `query_fragment` to the
production builder; legacy entries bind exact query fingerprints to their
declared production builder or execution-path owners and freeze those owners by
source SHA-256. The regression script runs
both query-package binding tests, which fill the manifests with production bytes
before applying shape validation. New or stale execution sites, missing
dispositions, unknown entry links, source-fragment or fingerprint drift,
unbounded variable-length traversals, unlabeled anchors, unordered pagination,
missing schema evidence, and forbidden plan signatures fail the gate. The same
script provisions a pinned, isolated Neo4j container and runs the build-tagged
live proof in `go/internal/query/queryplan_profile_live_test.go`. That proof
profiles 22 handler entries and 30 legacy entries through Neo4j `PROFILE` using
production-owned bytes, plus 875 hash-frozen safe production variants: 927
shapes in total. The production-variant family includes 280 distinct
import-dependency queries mapped from all 488 valid API and MCP request shapes.
Those shapes span both caller classes: since #5167 batch 2a every
import-dependency builder renders the caller's repository grant, so a scoped
caller runs different text from a shared-key one and the family profiles both.
Cloud-resource browsing is covered separately by one UID-bounded graph
hydration plan and 64 hash-frozen Postgres page variants: 32 filter/cursor
combinations each for all-scope and scoped access. A label or relationship-type
scan is accepted only by the closed code-level operator policy; manifest data
cannot add an exception.
Static validation
does not replace live backend `EXPLAIN`,
`PROFILE`, or before/after runtime measurements for production Cypher changes.

Hot-path pull requests must include one benchmark marker in the **PR description** (consumed by `scripts/verify-performance-evidence.sh`):

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`

and one observability marker in the **PR description**:

- `Observability Evidence:`
- `No-Observability-Change:`

Do not append per-PR evidence logs to versioned docs under `docs/` or `go/`.

Good:

```text
Performance Evidence: focused writer benchmark on NornicDB v1.0.45 with
50,000 File rows moved from 820ms to 310ms; full corpus stayed drained at
896/896 repositories with 0 open queue rows.

Observability Evidence: existing eshu_dp_canonical_phase_duration_seconds and
shared-edge summaries expose phase, row count, and relationship route.
```

Bad:

```text
Performance Evidence: looks faster locally.
Observability Evidence: logs are probably enough.
```

## Backend-Specific Behavior

Prefer backend-neutral Cypher. When behavior diverges, use this order:

1. Restructure the query into a shape both backends handle the same way.
2. Add a narrow dialect seam under `go/internal/storage/cypher/` for schema DDL,
   connection/runtime settings, retry classification, query builders, or
   measured adapters.
3. Patch NornicDB only for an evidence-backed correctness fix, general backend
   performance win, or measured Eshu runtime win.

Keep backend branches out of reducers, MCP tools, and collectors.
A query handler may branch on an explicit backend value only with a fail-safe default, a byte-identity test pinning the other dialect's statements (NornicDB stays pinned), and live proof on the backend whose statements change; see [Scoped Grant Predicates](cypher-scoped-grant-predicates.md).

## Anti-Patterns

- no baseline
- Neo4j docs cited for NornicDB behavior
- unit tests used as production-cardinality performance proof
- Compose success without phase timing or queue evidence
- index changes without write-amplification discussion
- worker-count or batch-size serialization used as a concurrency fix
- CTE materialization treated as a reference-count rule instead of a measured
  plan contract (Postgres 12+ materializes side-effect-free CTEs referenced
  more than once by default, while single-reference side-effect-free CTEs are
  normally folded)
- a row-set equivalence differential treated as sufficient for a lock/claim/lease
  rewrite (it drops `FOR UPDATE`; a separate EvalPlanQual/lease-safety proof is
  required)
- a DSN-gated performance proof that skips in CI with no hermetic in-CI guard

## Quick Reference

| Need | Neo4j | NornicDB |
| --- | --- | --- |
| Cypher feature support | Cypher manual for pinned major | `pkg/cypher/*.go` in NornicDB-New |
| Storage/constraint behavior | Operations manual | `pkg/storage/*.go` in NornicDB-New |
| Known traps | Neo4j changelog | [NornicDB Pitfalls](nornicdb-pitfalls.md) |
| Runtime knobs | Neo4j config reference | [NornicDB Tuning](nornicdb-tuning.md) |
| Version pinning | `NEO4J_IMAGE` | `NORNICDB_IMAGE` |

## Related Docs

- [NornicDB Pitfalls](nornicdb-pitfalls.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [Local Testing](local-testing.md)
- [Telemetry Overview](telemetry/index.md)
- [Graph Backend Operations](graph-backend-operations.md)
- [Runtime Declaration Graph Writes](cypher-runtime-declarations.md)
