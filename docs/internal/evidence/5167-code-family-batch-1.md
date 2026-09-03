# #5167 Code Family, Batch 1 — Ten Routes Off The Pending Ledger

Ten `POST /api/v0/code/*` routes leave `pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`) and join the
scoped-token allowlist. Each one now binds the caller's repository grant inside
the query it runs, and refuses a grantless scoped caller before touching a
backend.

All ten shared one root cause. `applyRepositorySelectorForCapability` only binds
a grant to a selector the caller actually supplied:
`queryselector.ResolveExactForAccess` returns `""` for an empty selector without
consulting the grant at all (`go/internal/query/queryselector/selector.go:73`).
So every route that reads "no repo_id" as "search everything" ran its downstream
query with no grant bound. Two of the ten were worse than that:
`code/complexity`'s entity_id branch carried no repository predicate at all and
ignored even a repo_id the caller did supply, and
`code/security/secrets/investigate` returns redacted secret line text.

`code/call-graph/metrics` is the one exception: `repo_id` is mandatory there and
the selector always resolves it through the grant, so it was never exploitable.
It gains the predicate as defense in depth.

## What Moved

| Route | Binding | File:line |
| --- | --- | --- |
| `POST /api/v0/code/topics/investigate` | `repo_id = ANY($n)` in `codeTopicFilters` | `go/internal/query/content_reader_code_topic.go:152` |
| `POST /api/v0/code/security/secrets/investigate` | `repo_id = ANY($n)` in `hardcodedSecretFilters` | `go/internal/query/content_reader_security_secrets.go:131` |
| `POST /api/v0/code/symbols/search` | `repo_id = ANY($n)` in `symbolSearchFilters`; per-repo fallback in `symbolNameFallbackEntities` | `go/internal/query/content_reader_symbol_search.go:109`, `go/internal/query/code_symbol.go:196` |
| `POST /api/v0/code/structure/inventory` | `repo_id = ANY($n)` in `structuralInventoryWhere` | `go/internal/query/content_reader_structural_inventory.go:168` |
| `POST /api/v0/code/dead-code` | `deadCodeCandidateRows` choke point, both backends | `go/internal/query/code_dead_code_scan.go:175` |
| `POST /api/v0/code/dead-code/investigate` | same choke point | `go/internal/query/code_dead_code_scan.go:175` |
| `POST /api/v0/code/dead-code/cross-repo` | same choke point (producer scan); consumer post-filter unchanged | `go/internal/query/code_dead_code_scan.go:175`, `go/internal/query/code_dead_code_cross_repo.go:317` |
| `POST /api/v0/code/call-graph/metrics` | grant on both `CALLS` endpoints in `callGraphMetricsEdgesCypher` | `go/internal/query/code_call_graph_metrics.go:218` |
| `POST /api/v0/code/quality/inspect` | `access.GraphCondition("repo")` in `buildCodeQualityCypher`'s WHERE | `go/internal/query/code_quality.go:193` |
| `POST /api/v0/code/complexity` | grant in all three builders; entity_id branch also gains the supplied-repo anchor | `go/internal/query/code_complexity_queries.go:24`, `:70`, `:123` |

The dead-code choke point's two backends:
`go/internal/query/content_reader_dead_code_candidates.go:56` (SQL
`AND repo_id = ANY($4)`, ahead of `LIMIT`/`OFFSET`) and
`go/internal/query/code_dead_code.go:133` (Cypher
`r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` on the
Repository anchor).

Two shared helpers keep the ten from drifting apart:
`codeContentGrantScope` (`go/internal/query/code_repository_selector.go:62`)
resolves the grant and reports the fail-closed case, and
`appendRepositoryGrantFilter`
(`go/internal/query/content_reader_code_topic.go:191`) is the single SQL grant
predicate all four content builders emit.

## The Empty-Grant Trap

An empty id list reads as *unrestricted* to every `repo_id = ANY($n)` and
`id IN $allowed_repository_ids` predicate in this package, so a grantless scoped
caller would have seen the whole corpus through the very predicate meant to
protect it. `codeContentGrantScope` returns `blocked` for that caller and each
route returns its own empty page — an empty list, a not-found, zero
candidates — without touching a backend, so an empty grant is indistinguishable
from an empty index.

## Red Then Green

Every handler proof is two-tenant: one granted repository, one out-of-grant
repository, and an assertion that the out-of-grant id never appears in the
response body. Each fake store mirrors the shipped SQL's real contract (an
explicit repo_id anchors, a non-empty grant list restricts, an empty list does
not), so removing a handler's binding makes the leak assertion fail rather than
passing against a hand-built stand-in.

| Test | Red | Green |
| --- | --- | --- |
| `TestCodeTopicInvestigation*` (3) | `AllowedRepositoryIDs = []string(nil), want [...]`; `queried = true, want false` | `ok internal/query 1.789s` |
| `TestCodeTopicFiltersBindTheGrantInTheShippedSQL` | `want a repo_id = ANY($1) grant predicate` | `ok internal/query 1.706s` |
| `TestCodeContentRoutes*` (3 × 4 route cases) | build failure: `AllowedRepositoryIDs undefined` on all three request types | `ok internal/query 1.802s` |
| `TestCodeContentFiltersBindTheGrantInTheShippedSQL` (3) | same build failure | `ok internal/query 1.802s` |
| `TestDeadCodeRoutes*`, `TestCrossRepoDeadCodeProducerScanCarriesTheGrant` | build failure: `undefined: deadCodeCandidateQuery` | `ok internal/query 2.074s` |
| `TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher` | same build failure | `ok internal/query 2.074s` |
| `TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL` (2) | `candidate SQL is missing "AND repo_id = ANY($4)"` | `ok internal/query 1.747s` |
| `TestCallGraphMetricsBindsTheGrantOnBothCallEndpoints` | `missing "(source.repo_id IN $allowed_repository_ids OR ...)" on the source endpoint` | `ok internal/query 1.826s` |
| `TestCallGraphMetricsEmptyGrantSkipsTheEdgeScan` (2) | `read` sub-test reached the graph | `ok internal/query 1.826s` |
| `TestCodeQualityAndComplexityBuildersBindTheGrant` (4) | all four builders `missing "(repo.id IN $allowed_repository_ids OR ...)"` | `ok internal/query 1.799s` |
| `TestCodeQualityAndComplexityEmptyGrantSkipTheGraphRead` (4) | all four reached the graph | `ok internal/query 1.799s` |
| `TestComplexityByEntityIDHonoursASuppliedRepoID` | `entity_id lookup ignores the supplied repo_id` | `ok internal/query 1.799s` |

Unscoped counterparts (`TestCodeTopicInvestigationSharedKeyReadIsUnchanged`,
`TestCodeContentRoutesSharedKeyReadIsUnchanged`,
`TestDeadCodeRoutesSharedKeyScanIsUnchanged`,
`TestCallGraphMetricsUnscopedCypherIsUnchanged`,
`TestCodeQualityAndComplexityUnscopedCypherCarriesNoGrant`) pin the other
direction: a shared-key caller's query text and row set are unchanged.

## BITES — Each Choke Point Proved To Bite

Each row breaks one production binding, runs the guard, restores the file, and
records the exit code directly (`cmd; echo $?`, never after a pipe).

| # | Mutation | Guard run | Exit |
| --- | --- | --- | --- |
| 1 | `appendRepositoryGrantFilter` emits `true /* $n */` instead of `repo_id = ANY($n)` | `go test ./internal/query -run BindTheGrantInTheShippedSQL -count=1` | `1` (4 failures: topic, secrets, symbol_search, structural_inventory) |
| 2 | `codeContentGrantScope` returns `blocked=false` on `access.Empty()` | `go test ./internal/query -run 'EmptyGrant' -count=1` | `1` (topic, secrets, symbols, structure ×2, dead-code ×2) |
| 3 | `buildDeadCodeGraphCypherForLabel` drops `access.GraphCondition("r")` | `go test ./internal/query -run TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher -count=1` | `1` |
| 4 | `ContentReader.DeadCodeCandidateRows` emits `AND true /* $n */` | `go test ./internal/query -run TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL -count=1` | `1` |
| 5 | `buildCodeQualityCypher` and all three complexity builders drop their grant | `go test ./internal/query -run TestCodeQualityAndComplexityBuildersBindTheGrant -count=1` | `1` (4 failures) |
| 6 | `callGraphMetricsEdgesCypher` renders an empty grant clause | `go test ./internal/query -run TestCallGraphMetricsBindsTheGrantOnBothCallEndpoints -count=1` | `1` |
| 7 | complexity and quality drop their `access.Empty()` refusal | `go test ./internal/query -run TestCodeQualityAndComplexityEmptyGrantSkipTheGraphRead -count=1` | `1` (4 failures) |

An earlier attempt at #1 deleted the whole helper body and failed as an unused
import rather than an assertion, which proves nothing. The mutation above keeps
the package compiling so the failure is the assertion's.

## Verification

Run after the last edit, exit codes captured directly:

```text
cd go && go test ./internal/query -count=1                    # 0
cd go && go test ./internal/mcp ./cmd/api -count=1            # 0
cd go && go vet ./internal/query ./internal/mcp               # 0
scripts/dev/precommit-go.sh fmt   <changed .go>               # 0
scripts/dev/precommit-go.sh lint  <changed .go>               # 0 (2 packages, 0 issues)
scripts/dev/precommit-go.sh filecap <changed .go>             # 0
scripts/verify-package-docs.sh                                # 0
scripts/verify-openapi.sh                                     # 0 (255 routes, 255 path entries)
git diff --check                                              # 0
```

`code_dead_code.go` and `code_dead_code_scan.go` were both within a dozen lines
of the 500-line cap before this change and went over it. The candidate-page
request type, the scan budget helpers, and the candidate-label predicate moved
to the sibling files that already own those families
(`code_dead_code_candidate_schedule.go`, `code_dead_code_candidate_entity.go`)
rather than to a new file, because `internal/query`'s non-test file set is
pinned by the dirgate grandfather ledger.

No-Regression Evidence: every predicate this change adds is an indexed equality
or an `ANY()`/`IN` membership test against the caller's grant, on a node or
column the query already matched. No traversal, join, `OPTIONAL MATCH`, or
anchor is added anywhere, and no `LIMIT` moves; the grant lands in the
MATCH-attached `WHERE` ahead of the existing `SKIP`/`LIMIT` (Cypher) or
`LIMIT`/`OFFSET` (SQL), so a scoped page is drawn from the granted set instead
of a cross-tenant-polluted one — strictly less scanned, never more. On the SQL
side the grant column is `content_entities.repo_id` / `content_files.repo_id`,
the same column the existing single-repository branch already filters on. On the
graph side the predicate is the
`alias.id IN $allowed_repository_ids OR alias.id IN $allowed_scope_ids` shape
`relationshipStoryRepoPredicates` already ships on both Neo4j and NornicDB, so
no new query shape reaches the pinned backend; in particular nothing here puts a
filter in a `WITH`-attached `WHERE` (not evaluated as a filter on NornicDB) or
guards a disjunct with `$param <> ''` (poisons the enclosing `OR` on NornicDB) —
see [NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md).
For an unscoped shared, admin, or local caller every predicate renders empty and
every grant parameter is unbound, so the query text those callers execute is
byte-identical to before. That is pinned for the one hot read carrying committed
plan evidence: `callGraphMetricsEdgesCypher`'s `cypher_sha256` for
`QP-CALL-GRAPH-HUBS` and `QP-CALL-GRAPH-RECURSIVE`
(`go/internal/queryplan/testdata/handler-hot-cypher.yaml`) is unchanged and its
accepted plan block (`NodeIndexSeek`, `Expand`; forbidden `AllNodesScan`,
`CartesianProduct`, `UnboundedExpand`) still describes what production emits —
only the builder's `source_sha256` moved, and
`TestCallGraphMetricsUnscopedCypherIsUnchanged` keeps the unscoped text from
drifting. For a scoped caller on that route the predicate is provably
row-set-neutral: `repo_id` is mandatory and grant-resolved, so
`source.repo_id = $repo_id` already implies the membership test. No benchmark
was run and no speedup is claimed; this is a correctness change with no latency
claim attached, and no live backend was available to this branch.

No-Observability-Change: no metric instrument, metric label, span, log event,
route, worker, queue, lease, or runtime knob is added or renamed. Operators keep
diagnosing these ten routes through the signals that already exist: the
governance-audit read-authorization events in `go/internal/query/auth_audit.go`
— `DecisionAllowed` with reason code `scoped_read_allowed`
(`recordScopedReadAuthorized`, `auth_audit.go:263`) and `DecisionDenied` with
the route's reason code (`recordScopedRouteAuthorizationDeniedWithReason`,
`auth_audit.go:212`), both stamped with tenant, workspace, actor hash, and
correlation id — plus the existing per-capability handler spans
(`SpanQueryCodeTopicInvestigation`, `SpanQueryDeadCodeInvestigation`,
`SpanQueryCallGraphMetrics`, `SpanQueryCodeStructuralInventory`,
`SpanQueryHardcodedSecretInvestigation`) and the
`eshu_dp_postgres_query_duration_seconds` /
`eshu_dp_neo4j_query_duration_seconds` histograms. A scoped caller that now
reads fewer rows shows up as a smaller `count`/`truncated` in the same response
envelope these routes already return.
