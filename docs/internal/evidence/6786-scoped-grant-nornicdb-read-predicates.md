# Scoped-grant read-predicate fix evidence (#6786)

## Defect

`GetEntityContext` (`go/internal/query/entity/handler.go`) and the scoped
Workload lookups (`go/internal/query/entity/workload_context.go`'s
`FetchWorkloadContextForOperation`, `go/internal/query/impacttrace/workload_selection.go`'s
`ResolveWorkloadSelector`) rendered a scoped caller's repository grant as
a Cypher-embedded predicate: a multi-line `AND EXISTS { MATCH ... WHERE
<grant> }` block for the entity route, and a multi-line
`AND ( ... OR EXISTS {...} )` group for the two workload lookups (the retired
`querycontract.ScopedWorkloadWhereClause`).

On the pinned NornicDB v1.3.3 image that multi-line grouped form is
unreliable: it can silently drop the WHOLE `WHERE` clause, including an
unrelated `e.id = $entity_id` / `w.id = $workload_id` anchor on the SAME
`MATCH`. A scoped caller's request for one entity or workload could read back
an unrelated one it never asked for, or (for the Workload selector, which had
no defense-in-depth check on the returned row) an ungranted caller could read
a workload it has no relationship to at all. Neo4j 2026 evaluates the
identical statement correctly; the defect is NornicDB-specific.

**Precise mechanism (proven live, schema applied, Go driver):** the character
immediately before an `AND` or `OR` keyword must be a space. When `AND`/`OR`
instead directly follows a newline or a tab -- the shape Go's raw-string
template literals produce for a gofmt-indented continuation line, e.g.
`"\n\t\t\tAND ("` -- NornicDB v1.3.3 mis-evaluates the WHOLE `WHERE` clause,
not just the `AND`/`OR` term. `"\n  AND"` and `"\n\t AND"` (a space directly
before the keyword, however the line got there) are unaffected. This is the
exact shape both retired predicates had. `go/internal/query/querytestutil`'s
`AssertCypherHasNoBrokenAndOr` (new) is a regression guard: every unit test
that captures cypher from a scoped-grant call site in this fix now asserts
the rendered text matches no `[\n\t](AND|OR)\b`.

Reproduced live against fresh, schema-applied containers on both pinned
backends (NornicDB v1.3.3 `bolt://127.0.0.1:27880`, Neo4j 2026
`bolt://127.0.0.1:27890`) via the real production handlers, seeded with a
granted repository, an ungranted repository, and a name-collision workload
whose own `repo_id` names the ungranted repository but which the granted
repository `DEFINES` too:

- `go/internal/query/entity/scoped_grant_live_test.go` (`TestLiveScoped*`)
- `go/internal/query/impacttrace/scoped_selector_live_test.go`
  (`TestLiveResolveWorkloadSelector*`)

Both select the backend/database via `ESHU_LIVE_GRAPH_BACKEND`
(`nornicdb`|`neo4j`) and `ESHU_LIVE_GRAPH_DATABASE`, the shared env contract
#6784 uses to run this package's and `entity`'s live tests together. Because
`go test` schedules different packages' tests concurrently by default, both
packages' schema-apply and seed/cleanup writes can land on the same live
database at once; `retryLiveWrite` (entity) / `retrySelectorLiveWrite`
(impacttrace) retry a Neo4j/NornicDB-classified transient error (e.g.
`Neo.TransientError.Transaction.Outdated`, observed live under
`go test ./internal/query/entity ./internal/query/impacttrace` without
`-p 1`) up to 5 times with a short backoff, rather than failing the whole
fixture on a conflict that a NornicDB write's own commit path resolves.
Proved with 5 consecutive green runs of that exact parallel command against
NornicDB (P3-c review follow-up: this reconciles an earlier draft of this
note, which said 3; the executor actually ran 5).

Pre-fix, 6 of 9 live assertions failed on NornicDB (0 of 9 on Neo4j):
`TestLiveScopedWorkloadContextGrant/direct_grant_admits` and
`.../no_grant_relationship_returns_not_found` both returned the SAME
name-collision workload (`wl-collision`) regardless of the requested id;
`TestLiveScopedEntityContextGrant/out_of_grant_returns_not_found_not_another_entity`
returned 200 instead of 404; `TestLiveResolveWorkloadSelector*
InGrantByID`/`OutOfGrantByIDReturnsEmpty` both resolved to
`wl-collision` regardless of grant; the by-name ambiguity test found no
ambiguity (the dropped `WHERE` collapsed the two-workload result to one
arbitrary row). Post-fix, all 9 pass on both backends.

## Fix

The scoped grant is now decided in Go instead of rendered as query text:

- `GetEntityContext` drops the `EXISTS` block; the existing single-line
  `OPTIONAL MATCH ... WHERE <grant on r>` plus the pre-existing
  `access.AllowsRepositoryID(repo_id)` post-check (unchanged) enforce the
  grant, and a new defense-in-depth guard treats a row whose `id` does not
  equal the requested `entity_id` as not found.
- `FetchWorkloadContextForOperation` drops the appended grant clause from its
  initial match; admission is decided from the row's own `repo_id`
  (`access.AllowsRepositoryID`) or from `FetchWorkloadRepositoryForAccess`'s
  existing DEFINES-scoped read (a single-line `WHERE`, unaffected by the
  defect) finding at least one granted repository.
- `ResolveWorkloadSelector` drops the appended grant clause; each
  candidate row now carries its `repo_id` and DEFINES-linked repository ids,
  and `querycontract.WorkloadGrantAdmitted` (new, `workload_grant.go`,
  replacing the retired `ScopedWorkloadWhereClause`) decides admission in Go.
  The id-lookup stays a `RunSingle` (Workload.id is unique); the name-lookup
  moved from two sequential `RunSingle` calls (SKIP/LIMIT 1) to one bounded
  `Run` (`workloadSelectorCandidateBound = 50`), because deciding the
  grant in Go means the raw row order is no longer pre-filtered to granted
  rows only -- a plain first/second-row compare could miss a granted
  duplicate sitting behind ungranted rows. Exceeding the bound fails closed
  (`querycontract.ErrWorkloadSelectorCandidatesExceedBound`) rather than silently
  deciding ambiguity from a possibly-truncated page.

`querycontract.WorkloadScopePredicate` (the SHAPE-A single-line `IN`
disjunction used by `entity.QueryServiceWorkloadCandidates` /
`queryServiceInstanceCandidates`) was proven correct in its production,
single-line-rendered form and is unchanged.

## Performance Evidence:

Measured on NornicDB v1.3.3 (`bolt://127.0.0.1:27880`, schema applied via
`graph.EnsureSchemaWithBackendStrict`, same seed as the live tests above),
median of 50 sequential in-process calls per case, single-threaded, local
container, before (HEAD before this change) vs. after (this change) (absolute
numbers are dominated by local Bolt round-trip overhead, not representative of
network latency at scale; the before/after comparison on identical input is
what matters):

| Call | Before | After | Delta |
| --- | --- | --- | --- |
| `GetEntityContext` (in-grant entity) | 1.00ms | 0.70ms | -30% (dropping the EXISTS subquery) |
| `GetWorkloadContext` -> `FetchWorkloadContextForOperation` (in-grant workload) | 2.99ms | 0.56ms | -81% (dropping the OR/EXISTS group) |
| `ResolveWorkloadSelector` (id lookup) | 0.32ms | 0.27ms | -16% |
| `ResolveWorkloadSelector` (name lookup) | 0.32ms | 0.49ms | +53% (bounded batch read + `OPTIONAL MATCH`/`collect` replaces an unbounded-but-incorrect single-row read; still sub-millisecond, and this path only runs when the id lookup misses) |

No index or schema change. The one measurable regression (name-lookup path,
+0.17ms) is accepted: it is the direct, necessary cost of closing the P0
grant bypass on that path (see Defect above) and stays well within the
existing per-request budget for a rarely-hit fallback stage.

**R2-2 follow-up: `HydrateResolvedEntityRepoIdentity` (F2's reshaped query).**
Measured with the same methodology (schema applied, single Workload entity
with one DEFINES-linked repository, median of 30 sequential in-process calls,
single-threaded, local container), before = `899691961~1` (pre-F2), after =
this change:

| Backend | Before | After | Delta | Note |
| --- | --- | --- | --- | --- |
| NornicDB v1.3.3 | 297.5µs (`repo_id=""`, `repo_name=""`) | 316.5µs (`repo_id`/`repo_name` correct) | not comparable | Before is the alias-collision defect (F2): the call returned in similar wall time but with garbage/empty data, never a real repo_id. Comparing its timing to the after number would compare two different amounts of real work done. |
| Neo4j 2026 | 587.8µs (`repo_id`/`repo_name` correct) | 499.0µs (`repo_id`/`repo_name` correct) | -15% | Real before/after comparison: same correct data both sides, Neo4j evaluated the pre-F2 shape correctly. The after shape (`e.id AS entity_id` instead of a bare passthrough column, `(repo)-[:DEFINES]->(e)` instead of a node-equality comparison) is measurably not slower. |

No index or schema change for this reshape either.

## Review follow-up (F1-F5)

An independent review of the initial fix (b5c6bac81) blocked on 5 findings,
all fixed here, TDD, with live re-proof:

- **F1 (P1, false-green-proof):** `FetchWorkloadRepositoryForAccess`'s
  `<-[:DEFINES]-` MATCH carries its own inner grant `WHERE` -- itself a
  backward-pattern shape this PR's Defect section already distrusts on
  NornicDB -- and Go never re-checked its returned `repo_id` against the
  grant. The original live seed's ungranted workload had NO DEFINES edge at
  all, so the read returning zero rows proved nothing about whether the
  WHERE actually filters. Fixed: every DEFINES candidate is now re-checked
  with `access.AllowsRepositoryID` before admission (workload_context.go).
  Both live seeds (`scopedGrantLiveSeed`, `selectorLiveSeed`) now give every
  workload a DEFINES edge from its own repository, including the ungranted
  ones, so the read has something real to filter. Unit regression:
  `TestFetchWorkloadRepositoryForAccessDropsUngrantedRowDespiteBackendWhere`
  and `TestGetWorkloadContextUngrantedDefinesRowReturnsNotFound`
  (workload_repository_selection_test.go), each proven RED against the
  pre-fix code, then GREEN.
- **F2 (P1, live NornicDB defect in UNCHANGED code):** removing
  GetEntityContext's EXISTS block (this PR's main fix) newly exposes scoped
  Workload/WorkloadInstance entity-context reads to
  `queryselector.HydrateResolvedEntityRepoIdentity`'s DEFINES-based repo
  backfill (entity_repo_identity.go), which this PR had not touched or
  proven. Live-probed and confirmed broken on NornicDB v1.3.3, correct on
  Neo4j: the query's `UNWIND $entity_ids AS entity_id` loop variable
  collided with the `RETURN entity_id` column alias, so NornicDB returned
  the literal UNWIND value text and the literal strings `repo.id`/`repo.name`
  instead of real data -- hydration silently never worked on that backend
  (failed closed, no leak, but also no legitimate scoped access). Fixed:
  renamed the loop variable to `requested_id`, project `e.id AS entity_id`
  from the matched node, and replaced the `(repo)-[:DEFINES]->(direct:
  Workload) WHERE direct = e` node-equality comparison with
  `(repo)-[:DEFINES]->(e)` directly. **Intentional behavior change:** a
  scoped caller's `GET /api/v0/entities/{id}/context` for a Workload or
  WorkloadInstance id now returns 200 with the correct repo when in-grant
  (previously always 404, on both backends, because the retired EXISTS block
  never matched a non-File-contained entity for a scoped caller at all) and
  404 when out-of-grant. Live regression added to `TestLiveScopedEntityContextGrant`
  (`workload_entity_id_*`, `workload_instance_entity_id_*`), proven RED on
  NornicDB / GREEN on Neo4j pre-fix, GREEN on both post-fix. Unit regression:
  `TestHydrateResolvedEntityRepoIdentityPinsCypherAndSplicesAccessPredicate`
  (queryselector/entity_repo_identity_test.go) updated to pin the new shape.
- **F3 (P2-blocking, defense-in-depth parity):** the entity route's row-id
  equality guard had no equivalent on the workload selector or workload
  context paths. Added: `ResolveWorkloadSelector`'s id-lookup now
  requires the row's own `id` to equal the selector;
  `admittedWorkloadCandidates` (its name-lookup path) drops any row whose
  `name` differs from the selector; `FetchWorkloadContextForOperation`
  rejects a base row whose anchored `id`/`name` does not equal the resolved
  selector (including the OR-combined `w.name = $service_name OR
  w.id = $service_name` shape `serviceLookupWhereClause` exercises). Unit
  regressions with mismatched fake rows in workload_selection_test.go
  and workload_repository_selection_test.go (via `FetchWorkloadContextForOperation`
  callers).
- **F4 (P2-blocking, repo MUST rule):** `AssertCypherHasNoBrokenAndOr` had no
  seeded-violation RED/GREEN pair. Added
  `querytestutil.TestAssertCypherHasNoBrokenAndOrSeededViolations`, table-driven
  over the exact RED (`\n\tAND`, `\nOR`, `\n \tAND`) and GREEN (`\n\t AND`,
  `\n  AND`, `\n\tORDER BY`, `\n\tOPTIONAL MATCH`) strings the review named,
  using a `cypherAssertionT` recording double (`testing.TB` cannot be
  implemented outside package `testing`).
- **F5 (telemetry):** the Go-side grant-decision and row-id-mismatch guards
  produced no operator signal. Added a `Warn` log at the two id/anchor-mismatch
  guards that signal backend anchor drift specifically (`GetEntityContext`,
  `FetchWorkloadContextForOperation`): `requested_entity_id`/
  `returned_entity_id` and `requested_selector`/`returned_id`/`returned_name`
  respectively, both tagged `reason=backend_anchor_mismatch`. Ordinary grant
  denials (the common, expected case) still log nothing, to avoid noise. The
  originally-deferred counter and `ResolveWorkloadSelector` logger are
  closed in R2-4 below.
- **R2-4 (telemetry, round-2 follow-up):** closed F5's deferral.
  `ResolveWorkloadSelector` now takes `logger *slog.Logger` and
  `instruments *telemetry.Instruments` (both nil-tolerant); its F3 guards
  (id-lookup row-identity mismatch, name-lookup row-identity mismatch) emit
  the same `reason=backend_anchor_mismatch` `Warn` the entity-family guards
  do. Threading `instruments` through required widening
  `impact.DeploymentTraceContextProvider.FetchServiceTraceContext` (and its
  seam/shim forwarders in `impact/seam.go` and `family_impact_shim.go`) by one
  parameter, since `fetchServiceTraceContext`
  (`family_impact_trace_deployment.go`) is the only place that can reach both
  a `*telemetry.Instruments` (off `impact.Handler.Instruments`, already wired
  at both HTTP call sites) and this selector; the interface has exactly one
  production implementation, so this was a same-shape extension of the
  existing logger-threading pattern, not a new design.

  Added `telemetry.Instruments.QueryScopedGrantDenied`
  (`eshu_dp_query_scoped_grant_denied_total`, registered in
  `go/internal/telemetry/instruments.go` per `telemetry-coverage-discipline`),
  a counter over bounded `operation` and `reason`
  (`grant_denied` | `backend_anchor_mismatch`) labels. `operation` is a
  verbatim passthrough of whatever operation string reaches
  `FetchWorkloadContextForOperation` (proven by R3-2's
  `TestQueryScopedGrantDeniedOperationValues`), not a fixed per-function
  constant; the full closed set actually emitted is `entity_context`
  (`GetEntityContext`, always this literal), `workload_context`
  (`fetchWorkloadContext`), `service_context` (`GetServiceContext`),
  `service_story` (`GetServiceStory`), `service_investigation`
  (`InvestigateService`), `deployment_trace` (`fetchServiceTraceContext`),
  and `deployment_trace_selector` (`ResolveWorkloadSelector`'s own
  internal constant, pinned by R3-2's
  `TestResolveWorkloadSelectorOperationLabel`). Emission helpers:
  `entity.Handler.recordScopedGrantDenied`
  (`entity/scoped_grant_telemetry.go`) and the package-level
  `impacttrace.recordScopedGrantDenied`
  (`impacttrace/scoped_grant_telemetry.go`), both nil-tolerant. Wired at all
  three Go-side grant-decision seams this PR owns: `GetEntityContext`'s
  anchor-mismatch guard and final grant check, `FetchWorkloadContextForOperation`'s
  anchor-mismatch guard and fail-closed check, and
  `ResolveWorkloadSelector`'s id-lookup and name-lookup stages (the
  latter split into `backend_anchor_mismatch` when any row's own name
  disagreed with the selector, vs. `grant_denied` when every row's name
  matched but none was grant-admitted).

  Proof: `entity/scoped_grant_telemetry_test.go` and
  `impacttrace/scoped_grant_telemetry_test.go`, each asserting the actual wire
  metric (via an OTEL `sdkmetric.ManualReader`, not a mock) and the emitted
  `Warn` text, for both reasons at each seam; one, mutation-proven RED/GREEN
  by temporarily removing the emission call
  (`TestResolveWorkloadSelectorIDMismatchEmitsAnchorMismatchTelemetry`).
  Documented in `go/internal/telemetry/README.md`, as a compact entry in
  the Data-Plane Core table in `docs/public/reference/telemetry/metrics.md`
  (not grandfathered, had headroom; `index.md` did not, see below), and
  in `docs/public/observability/telemetry-coverage.md` (see R3-1 below).

  **R3-1 (resolved): the X1 coverage-doc row.**
  `docs/public/observability/telemetry-coverage.md` and
  `docs/public/reference/telemetry/index.md` are both pinned at their exact
  Markdown 500-line-cap grandfathered ceiling with zero headroom (`1109` /
  `1270`, `scripts/lib/markdown-line-cap-grandfather.tsv`); the pre-commit
  hook (`scripts/lib/markdown-line-cap-core.sh`) refuses any growth, and
  explicitly refuses re-pinning the ceiling upward too. `index.md`'s content
  had a sanctioned alternative (moved to `metrics.md`, above). An earlier
  revision of this doc treated `telemetry-coverage.md` as having no
  sanctioned path either, because `scripts/verify-telemetry-coverage.sh`
  (the X2 gate) reads only that one file and requires a row mentioning the
  metric -- true, but that overstated the blocker: an EXISTING row can be
  widened IN PLACE (net zero line growth) rather than a new row added.
  Precedent: commit `9666a5c7e` edited a row of this same capped file
  in place, 1 insertion(+)/1 deletion(-). Fixed the same way: the adjacent
  `query infra` row "Scoped-token SHAPE-A grant-cap degradation disclosure
  (#5403 follow-up #5408)" (line 893) widened in place to also name the
  three #6786 emitting files and the new counter, file stays at exactly
  1109 lines. `ESHU_TELEMETRY_COVERAGE_BASE=origin/main
  scripts/verify-telemetry-coverage.sh` now exits 0.

  **R3-2 (resolved): the doc comment's operation-value list.**
  `QueryScopedGrantDenied`'s doc comment previously listed
  `resolve_trace_workload_selector` (never emitted; the real constant is
  `deployment_trace_selector`) and omitted `service_context`,
  `service_story`, and `service_investigation` (all three real,
  passed through by `FetchWorkloadContextForOperation`'s callers). Corrected
  to the actual closed set above, with two new pinning tests
  (`TestQueryScopedGrantDeniedOperationValues`,
  `TestResolveWorkloadSelectorOperationLabel`).

## Review round 4 (service context by name, telemetry, overflow, naming)

- **P1: service context by name denied granted callers.** The name lookup in
  `FetchWorkloadContextForOperation` read `MATCH (w:Workload) WHERE
  w.name = $service_name ... LIMIT 1` with no grant and no `ORDER BY`, then
  decided the grant. With two workloads named `api` (repo-a granted, repo-b
  not), a repo-a caller could get repo-b's row and a 404. Fixed in
  `entity/workload_lookup.go`: a name-keyed lookup reads
  `ORDER BY w.id LIMIT 51` with `collect(DISTINCT dr.id)`, filters in Go with
  `WorkloadGrantAdmitted`, and returns the lowest admitted id. Id-only lookups
  keep the exact single-row read. Unit RED:
  `TestGetServiceContextNameCollisionReturnsGrantedWorkload` failed with
  `status = 404, want 200`. Live RED on the pre-fix code (956f6521c) with the
  new `TestLiveScopedServiceContextNameCollision` (`-count=3`): NornicDB
  failed 3 of 3 runs and Neo4j 3 of 3, each with a 404 for a granted caller
  or the other same-name workload's id, depending on which row the unordered
  read returned. Live GREEN on both backends after the fix.
- **P2: `grant_denied` counted before a fallback admitted.** Both
  `ResolveWorkloadSelector` (id, then name) and `fetchServiceWorkloadContext`
  (name, then id, then read model) now count `grant_denied` once, and only
  when no lookup admitted a workload. RED:
  `TestResolveWorkloadSelectorIDDenialThenNameAdmitCountsNoDenial` and
  `TestFetchServiceWorkloadContextNameDenialThenIDAdmitCountsNoDenial` saw
  one denial on a successful request; the `DeniedBy*` pair saw `Value:2` for
  one denied request.
- **P2: overflow 500 leaked a count.** The selector returned
  `...candidates exceed bound: 51` and the handlers wrote it as a 500. The
  typed `querycontract.ErrWorkloadSelectorCandidatesExceedBound` now maps to
  409 through `querycontract.WriteWorkloadSelectorOverflow`, with fixed text
  and no count, on trace-deployment-chain, deployment-config-influence, and
  service context. 409 matches the ambiguous-selector convention these
  routes already use. RED: both impact handler tests saw
  `status = 500 ... exceed bound: 51`; service context returned 200 because
  its single-row read never saw the other 50 rows. OpenAPI gains `409` on `getServiceContext`.
- **P3: ambiguity compared only rows 0 and 1.** Now decided from distinct
  admitted ids. RED: `TestResolveWorkloadSelectorAmbiguityUsesDistinctIDs`
  (rows `a, a, b`) returned no error.
- **P3: hydration admission rule.** Kept `AllowsRepositoryID` on the attached
  repository, documented in `queryselector/README.md`, pinned by
  `TestHydrateResolvedEntityRepoIdentityDoesNotUseWorkloadAdmission`
  (mutation-checked: swapping in `WorkloadGrantAdmitted` fails it).
- **Naming.** `impact_trace_workload_selection{,_test}.go` became
  `workload_selection{,_test}.go`; `ResolveTraceWorkloadSelector` became
  `ResolveWorkloadSelector`; `ErrAmbiguousTraceWorkloadSelector` became
  `impacttrace.ErrAmbiguousWorkloadSelector`; `entity.ResolveEntityRequest`
  and `entity.BuildResolveEntityGraphQuery` became `entity.ResolveRequest`
  and `entity.BuildResolveGraphQuery`.

No-Regression Evidence: the changed read is the service-context name lookup.
Harness: median and p90 of 50 sequential `fetchServiceWorkloadContext` calls
after 5 warm-up calls, scoped caller granted repo-a, the scoped-grant live
seed plus 500 filler workloads with distinct names, schema applied with
`graph.EnsureSchemaWithBackendStrict`, fresh containers per side. Before is
956f6521c (pre-fix); after is this change. Images:
`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf4...` on
`bolt://127.0.0.1:27960` and `neo4j:2026-community@sha256:eabfbb04...` on
`bolt://127.0.0.1:27970`, both local.

| Backend | Case | Before median (p90) | After median (p90) | Correct before / after |
| --- | --- | --- | --- | --- |
| NornicDB v1.3.3 | unique name | 1.333ms (1.454ms) | 1.394ms (1.539ms) | 50/50 / 50/50 |
| NornicDB v1.3.3 | same-name pair | 0.738ms (0.857ms) | 1.285ms (1.495ms) | 0/50 / 50/50 |
| Neo4j 2026 | unique name | 6.981ms (7.730ms) | 6.997ms (8.175ms) | 50/50 / 50/50 |
| Neo4j 2026 | same-name pair | 3.701ms (4.385ms) | 6.198ms (7.335ms) | 0/50 / 50/50 |

The unique-name path moves by +0.06ms on NornicDB and +0.02ms on Neo4j. The
same-name "before" timings are not comparable: every before call returned a
404 or the other tenant's row, which skips the repository, topology, and
dependency reads the correct answer needs. Neo4j plans the candidate read as
`NodeIndexSeek` on `Workload.name`, then `OptionalExpand(All)` and
`OrderedAggregation`, with no `AllNodesScan`. Input cardinality is one name
key; output is at most 51 rows, then one. No index or schema change.

Observability for this round: the overflow logs a `Warn` with
`reason=candidate_bound_exceeded` and the bound, never the row count or the
selector. Anchor mismatches in the candidate read keep the existing
`backend_anchor_mismatch` `Warn` and counter.

## Review round 3: bound granted rows only (#6801 review F-R5-1)

Round 2's candidate reads applied the 51-row bound before the grant. With 55
ungranted workloads and one granted workload sharing a name (the ungranted
ones sort first), a granted caller got 409. A caller with no grant also got
409 rather than 404, which signalled that 51 or more same-name workloads
exist outside their grant. For a scoped caller, both name reads
(`entity.lookupWorkloadRow` for service context,
`impacttrace.ResolveWorkloadSelector` for the deployment trace) now append
`querycontract.WorkloadScopePredicate("w", access)` to the WHERE line with a
space-led `AND`. That is the SHAPE-A single-line predicate
`QueryServiceWorkloadCandidates` already uses in production. The Go
`WorkloadGrantAdmitted` re-check stays as defense in depth. Unscoped reads are
unchanged.

- RED (NornicDB v1.3.3, pre-fix): `TestLiveScopedServiceContextBoundCountsGrantedRowsOnly`
  got `status = 409, want 200` (granted caller) and `status = 409, want 404`
  (no-grant caller). `TestLiveResolveWorkloadSelectorBoundCountsGrantedRowsOnly`
  got the overflow error for the granted caller.
- GREEN: both pass on NornicDB v1.3.3 and Neo4j 2026, together with every
  other `TestLive*` in `entity`, `impacttrace`, and `queryselector`. The unit
  guards `TestGetServiceContextScopedNameReadBoundsGrantedRows` and
  `TestResolveWorkloadSelectorScopedNameReadBoundsGrantedRows` fail on the
  pre-fix code and pass after it.
- Telemetry: a workload the backend excludes through the grant predicate
  never reaches Go. So for a scoped name miss, `grant_denied` now counts only
  rows the Go re-check rejects (a backend that ignored the predicate). The
  counter's documented meaning, reads decided closed in Go, is unchanged.

No-Regression Evidence: the changed read is the scoped service-context name
lookup. The harness is the same as round 2, run as three interleaved
before/after rounds of 50 calls each after 5 warm-ups. The live scoped-grant
seed plus 500 filler workloads was used, schema applied, on the shared local
containers `bolt://127.0.0.1:27920` (NornicDB v1.3.3) and `:27930` (Neo4j
2026). Before is the round-2 head; after is this change. Medians in ms:

| Backend | Case | Before r1 / r2 / r3 | After r1 / r2 / r3 |
| --- | --- | --- | --- |
| NornicDB v1.3.3 | unique name | 3.032 / 2.984 / 2.796 | 3.001 / 2.941 / 2.906 |
| NornicDB v1.3.3 | same-name pair | 2.974 / 2.946 / 2.934 | 2.973 / 3.241 / 2.955 |
| Neo4j 2026 | unique name | 4.064 / 3.712 / 3.580 | 3.968 / 3.496 / 3.531 |
| Neo4j 2026 | same-name pair | 3.834 / 3.582 / 3.572 | 4.092 / 3.527 / 3.494 |

All calls returned 200 (50/50) on both sides. The deltas range from -0.25 to
+0.29 ms and change sign between rounds, so there is no measurable
regression. For a scoped caller the predicate also shrinks the candidate
rows to granted ones. Absolute figures differ from round 2's table because
the containers and host load differ; only the paired deltas are the claim.

## Review round 4 (#6801 review F-R6-1..4)

- **F-R6-1.** The package READMEs (`entity`, `impacttrace`, `querycontract`)
  and the godoc on `WorkloadSelectorCandidateBound`, `ResolveWorkloadSelector`
  and `QueryScopedGrantDenied` now describe the scoped Cypher prefilter
  instead of an unfiltered read.
- **F-R6-2.** Past the 128-term SHAPE-A cap, the prefilter drops the overflow
  grants' `DEFINES` terms and fails closed. Both name reads now emit
  `eshu_dp_query_scope_grant_inline_capped_total`: reason
  `workload_context_name` for the entity name read and
  `deployment_trace_selector` for the selector. They also log a `Warn` with
  the grant-set sizes, never ids.
  `TestGetServiceContextGrantCapEmitsInlineCappedTelemetry` and
  `TestResolveWorkloadSelectorGrantCapEmitsInlineCappedTelemetry` failed
  before the change (no data point at 129 grants) and pass after it. They
  also assert that nothing is emitted under the cap. The predicate is kept
  past the cap, because dropping it would bring back the F-R5-1 existence
  signal for large tokens.
- **F-R6-3.** The entity name read parenthesizes the caller's clause:
  `(w.name = $service_name) AND (…)`.
- **F-R6-4.** Paired selector measurement, run the same way as round 3 (three
  interleaved rounds, 50 calls after 5 warm-ups, live seed plus 500 fillers,
  scoped caller). Before is the round-2 `workload_selection.go`; after is
  this head. Medians in ms, as unique name / ambiguous name:

| Backend | Before r1 / r2 / r3 | After r1 / r2 / r3 |
| --- | --- | --- |
| NornicDB v1.3.3 | 0.500/0.500, 0.451/0.449, 0.473/0.435 | 0.541/0.542, 0.515/0.518, 0.561/0.544 |
| Neo4j 2026 | 0.605/0.617, 0.536/0.569, 0.557/0.588 | 17.235/0.642, 0.613/0.610, 0.541/0.676 |

On NornicDB the selector name read costs a consistent +0.04 to +0.11 ms,
about +12% on a sub-millisecond read. The cost is the grant predicate's
`OR` terms, which the F-R5-1 correctness fix requires. Neo4j is flat,
except for a single 17 ms unique-name median in round 1 that did not
repeat in rounds 2 or 3. It is recorded here, not explained. For scale:
the full service-context request measured in round 3 is about 3 ms on
NornicDB, so this adds roughly 2% of such a request.

## Observability Evidence:

`service.StartServiceQueryStage`/`timer.Done` call sites and their attributes
are unchanged; an operator sees the same `workload_lookup`/
`repository_lookup`/`instance_lookup` stage timing as before, just against
the corrected query shape. On top of that unchanged baseline, this PR adds
new signal: the `Warn` logs from F5 and the
`eshu_dp_query_scoped_grant_denied_total` counter from R2-4 above (see that
section for the full label/emission-site contract).
