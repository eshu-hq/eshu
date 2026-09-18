# Scoped-grant read-predicate fix evidence (#6786)

## Defect

`GetEntityContext` (`go/internal/query/entity/handler.go`) and the scoped
Workload lookups (`go/internal/query/entity/workload_context.go`'s
`FetchWorkloadContextForOperation`, `go/internal/query/impacttrace/impact_trace_workload_selection.go`'s
`ResolveTraceWorkloadSelector`) rendered a scoped caller's repository grant as
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
  (`TestLiveResolveTraceWorkloadSelector*`)

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
returned 200 instead of 404; `TestLiveResolveTraceWorkloadSelector*
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
- `ResolveTraceWorkloadSelector` drops the appended grant clause; each
  candidate row now carries its `repo_id` and DEFINES-linked repository ids,
  and `querycontract.WorkloadGrantAdmitted` (new, `workload_grant.go`,
  replacing the retired `ScopedWorkloadWhereClause`) decides admission in Go.
  The id-lookup stays a `RunSingle` (Workload.id is unique); the name-lookup
  moved from two sequential `RunSingle` calls (SKIP/LIMIT 1) to one bounded
  `Run` (`traceWorkloadSelectorCandidateBound = 50`), because deciding the
  grant in Go means the raw row order is no longer pre-filtered to granted
  rows only -- a plain first/second-row compare could miss a granted
  duplicate sitting behind ungranted rows. Exceeding the bound fails closed
  (`errTraceWorkloadSelectorCandidatesExceedBound`) rather than silently
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
| `ResolveTraceWorkloadSelector` (id lookup) | 0.32ms | 0.27ms | -16% |
| `ResolveTraceWorkloadSelector` (name lookup) | 0.32ms | 0.49ms | +53% (bounded batch read + `OPTIONAL MATCH`/`collect` replaces an unbounded-but-incorrect single-row read; still sub-millisecond, and this path only runs when the id lookup misses) |

No index or schema change. The one measurable regression (name-lookup path,
+0.17ms) is accepted: it is the direct, necessary cost of closing the P0
grant bypass on that path (see Defect above) and stays well within the
existing per-request budget for a rarely-hit fallback stage.

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
  context paths. Added: `ResolveTraceWorkloadSelector`'s id-lookup now
  requires the row's own `id` to equal the selector;
  `admittedWorkloadCandidates` (its name-lookup path) drops any row whose
  `name` differs from the selector; `FetchWorkloadContextForOperation`
  rejects a base row whose anchored `id`/`name` does not equal the resolved
  selector (including the OR-combined `w.name = $service_name OR
  w.id = $service_name` shape `serviceLookupWhereClause` exercises). Unit
  regressions with mismatched fake rows in impact_trace_workload_selection_test.go
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
  produced no operator signal. Added a `Warn` log (no counter -- a counter
  needs a telemetry contract entry and docs this follow-up does not include;
  flagged to the reviewer) at the two id/anchor-mismatch guards that signal
  backend anchor drift specifically (`GetEntityContext`,
  `FetchWorkloadContextForOperation`): `requested_entity_id`/
  `returned_entity_id` and `requested_selector`/`returned_id`/`returned_name`
  respectively, both tagged `reason=backend_anchor_mismatch`. Ordinary grant
  denials (the common, expected case) still log nothing, to avoid noise.
  `ResolveTraceWorkloadSelector`'s equivalent F3 guards have no logger
  threaded through their signature; adding one is a signature change onto
  `family_impact_trace_deployment.go`'s caller, deferred pending owner
  direction.

## No-Observability-Change:

`service.StartServiceQueryStage`/`timer.Done` call sites and their attributes
are unchanged; an operator sees the same `workload_lookup`/
`repository_lookup`/`instance_lookup` stage timing as before, just against
the corrected query shape. This no longer covers the whole diff: see F5
above for the two new `Warn` logs the review round added.
