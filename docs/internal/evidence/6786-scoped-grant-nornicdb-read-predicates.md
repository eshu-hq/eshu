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
  (`errWorkloadSelectorCandidatesExceedBound`) rather than silently
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

## Observability Evidence:

`service.StartServiceQueryStage`/`timer.Done` call sites and their attributes
are unchanged; an operator sees the same `workload_lookup`/
`repository_lookup`/`instance_lookup` stage timing as before, just against
the corrected query shape. On top of that unchanged baseline, this PR adds
new signal: the `Warn` logs from F5 and the
`eshu_dp_query_scoped_grant_denied_total` counter from R2-4 above (see that
section for the full label/emission-site contract).
