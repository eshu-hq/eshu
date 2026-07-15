# Dashboard Correctness and Bounded-Read Evidence

This note records the focused local proof for issues #5244, #5245, #5248,
#5250, #5251, and #5253. Ask Eshu's exact repository-count contract for #5246
lives with its owning package in
[`go/internal/ask/engine/README.md`](../../../go/internal/ask/engine/README.md#exact-indexed-repository-counts).
This note does not claim that #5240 is fixed. Final live-console proof remains a
separate gate.

## Incoming NornicDB relationship read (#5244)

The incoming half of `GET /api/v0/code/relationships` now anchors the exact
target entity before expanding the incoming relationship. It retains the same
relationship types, `uid`-then-`id` fallback, row behavior, and source/repository
hydration.

Performance Evidence: on the retained NornicDB v1.1.11 graph, the old incoming
shape did not finish within either a 15-second or 25-second proof timeout for a
known Function target. The target-first shape completed in 0.563 to 1.648 ms on
the same target. Both normalized result sets contained one relationship;
old-minus-new and new-minus-old were `0/0`. This is a bounded interactive read
win, not a claim against the historical #3624 bootstrap target.

The focused contract command pins exact target matching, prefix-collision
avoidance, empty and duplicate behavior, recursive self-edges, the fallback,
and hydration:

```bash
cd go && GOCACHE=/tmp/eshu-5244-gocache go test ./internal/query \
  -run 'TestNornicDB(IncomingOneHop|OneHopRelationships)' -count=1
```

No-Observability-Change: the route retains the existing graph query tracing,
duration histogram, query trace, and truth envelope. The query reversal adds no
graph write, worker, queue, metric, span name, response field, or runtime knob.

## Semantic-search scope and readiness (#5245)

The retained-data cardinality, old/new SQL, exactness matrix, measured timings,
concurrency analysis, commands, and structured markers are recorded in
[`go/internal/storage/postgres/evidence-5245-semantic-search-scope-readiness.md`](../../../go/internal/storage/postgres/evidence-5245-semantic-search-scope-readiness.md).
That proof reports a `0.101 ms` canonical-id resolver, an eight-to-zero
false-pending correction, live BM25 visibility, production pgx vector decoding,
and exact/fail-closed scope behavior. It does not claim a seconds-scale win.

## Dead-code Trait identity and scan bounds (#5248)

No-Regression Evidence: the query now preserves Trait identity instead of
mapping it to Function, keeps repository scope on content rows, and reports the
2,500-row bound per candidate label separately from the aggregate maximum.
Focused handler, content-reader, and OpenAPI tests run with:

```bash
cd go && GOCACHE=/tmp/eshu-5248-gocache go test ./internal/query \
  -run 'Test(DeadCodeCandidateEntityTypeMapsEveryAdvertisedLabel|ContentReaderDeadCodeCandidateRowsKeepsTraitTypeAndRepositoryScope|HandleDeadCodeReportsTotalAndPerLabelCandidateScanLimits|OpenAPIDeadCode)' \
  -count=1
```

The changed golden snapshot and its static contract run with:

```bash
(cd go && GOCACHE=/tmp/eshu-5248-gocache go test ./cmd/golden-corpus-gate -count=1)
bash scripts/test-verify-golden-corpus-gate.sh
```

No-Observability-Change: no metric, span, log field, queue, worker, graph write,
or runtime knob changed. Existing dead-code query spans and candidate-scan
metadata remain the diagnostic surface; the response now distinguishes the
per-label bound from the aggregate maximum.

## Operations response negotiation (#5250)

No-Regression Evidence: the operations handler now uses shared success-response
negotiation. Envelope clients receive `{data, truth, error}`, while legacy
`application/json` clients receive the same unwrapped operations object. The
focused handler and console client tests run with:

```bash
cd go && GOCACHE=/tmp/eshu-5250-gocache go test ./internal/query \
  -run '^TestGetOperationsNegotiatesEnvelopeAndPreservesLegacyRawJSON$' -count=1
npm --prefix apps/console test -- src/api/operationsBoard.test.ts
```

No-Observability-Change: the route keeps its existing status reads, HTTP route
attribution, and error reporting. No backend read, metric, span name, log field,
runtime knob, or polling interval changed.

## Vulnerability and findings empty-state truth (#5251)

No-Regression Evidence: an empty reachable-vulnerability result now says that
no affected service was proven by current impact evidence; it no longer
mislabels that result as a missing intelligence collector. Partial failure and
loading states keep available findings visible while naming the unavailable or
pending source. The focused UI tests run with:

```bash
npm --prefix apps/console test -- \
  src/pages/VulnerabilitiesPage.test.tsx src/pages/FindingsPage.test.tsx
```

No-Observability-Change: this is a render-state and copy correction over the
existing model provenance. It adds no request, collector, metric, span, log
field, runtime knob, or persisted state.

## Cloud inventory active-generation readback (#5253)

The cloud inventory list now joins reducer-owned identity facts to each scope's
active generation before pagination. It does not collapse duplicate identities
inside one active generation.

Performance Evidence: on retained local Postgres data, the old query read 5,955
rows for 3,271 identity keys; its first 50-row page contained 28 unique
`cloud_resource_uid` values. `EXPLAIN (ANALYZE, BUFFERS)` reported 6,174 shared
buffer hits, 4.146 ms planning, and 10.437 ms execution. The active-generation
query returned 3,271 rows, produced 50 unique identities on its first page, and
reported 3,237 shared-buffer hits, 1.721 ms planning, and 3.990 ms execution.
Both shapes used the same warm data and the same fact-kind, tombstone, ordering,
limit, and offset predicates.

The focused query-shape and handler command is:

```bash
cd go && GOCACHE=/tmp/eshu-5253-gocache go test ./internal/query \
  -run 'TestCloudInventory(ReadbackSelectsOnlyActiveScopeGenerations|HandlerListsCanonicalIdentities)' \
  -count=1
```

No-Observability-Change: the read keeps the existing `postgres.query` span with
`db.operation=list_cloud_inventory_identities` and the existing cloud inventory
response metadata. It adds no queue, worker, collector, graph write, metric,
span name, or runtime knob.
