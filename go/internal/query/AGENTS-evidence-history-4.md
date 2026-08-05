# AGENTS.md evidence history, part 4 (issue #5764, round-6 and round-7 review follow-ups)

Part 4 of 4 of the dated, per-issue evidence and rationale entries for
`go/internal/query`. Nothing was moved here: every entry below is new content
written for the #5764 review follow-ups, and part 4 exists because
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md) was already at 463
lines when they were written, so appending them there would have carried it
past CLAUDE.md's 500-line-per-file convention (not a repo-enforced gate; no CI
check counts Markdown lines). (An earlier version of this paragraph said
these entries were split out of part 2. They were not -- part 2 grew by 17 net
lines in the same commit that created this file. The claim is corrected here
rather than left standing, the same class of false-provenance note round 4
found in part 3.) Read [AGENTS.md](AGENTS.md) first for the invariants these
entries evidence, and see
[AGENTS-evidence-history.md](AGENTS-evidence-history.md) (part 1),
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md) (part 2), and
[AGENTS-evidence-history-3.md](AGENTS-evidence-history-3.md) (part 3, where
#5764's main entry lives).

### Infrastructure content-path bound inverted the defect it fixed (round-6 P1/P2/P3 review follow-up to #5764)

The P2-3 follow-up documented in
[AGENTS-evidence-history-3.md](AGENTS-evidence-history-3.md) switched
`queryRepoInfrastructureFromContent` (`repository_infrastructure.go`) from
silently truncating to requesting `repositoryInfrastructureEntityLimit+1`
rows and reporting `len(entities) > limit`. That fix was itself wrong: it
called the UNTYPED `ContentReader.ListRepoEntities`, which has no
`entity_type` predicate and returns every parsed content entity for the
repository -- functions, classes, structs, not only infrastructure. So
`truncated` reported whether the repository had more than 5000 content
entities of ANY type (true for nearly every real repository), not whether the
infrastructure panel itself was clipped, while every infrastructure entity
was actually present and undisclosed as complete. This propagated the same
false-claim class #5764 exists to remove -- inverted -- to `truncated`,
`answer_metadata.truncated`, `answer_metadata.limitations`, and
`partial_reasons` on `GET /repositories/{id}/story`,
`GET /repositories/{id}/context`, `GET /workloads/{id}/context`,
`GET /services/{name}/context`, and `GET /services/{name}/story`. Second-order:
the same 5001-row window, sorted `relative_path, start_line` across ALL
types, could silently drop infrastructure entities in late-sorting paths
before entity-type classification ever saw them -- the exact hazard
`content_reader_by_type.go` documents and `ListRepoEntitiesByType`/
`fetchK8sResourceCandidates` exist to avoid on the k8s SELECTS scan.

Fixed by adding `ContentStore.ListRepoEntitiesByTypes` (`ports.go`,
implemented in `content_reader_by_type.go` as `entity_type = ANY($types)` --
one query, not a per-type loop, mirroring `ListRepoEntitiesByPaths`'s
`relative_path = ANY($paths)` precedent) and switching
`queryRepoInfrastructureFromContent` to call it with the new canonical
`repositoryInfrastructureEntityTypes` list, so the LIMIT and its truncation
signal both apply to the TYPE-FILTERED set. `isRepositoryInfrastructureType`
was refactored to check membership in a set built from that same list
(`repositoryInfrastructureTypeSet`), so the content-path filter and the
graph-path defensive gate cannot drift apart again. Proven by
`TestQueryRepoInfrastructureFromContentIgnoresNonInfrastructureEntityCount`
(`repository_infrastructure_test.go`): `repositoryInfrastructureEntityLimit+1`
entities (4998 `Function`, 3 `TerraformResource`) must report
`truncated=false` with all 3 infrastructure rows present -- RED before the
fix (`truncated=true`, reproducing the defect), GREEN after.

A second, independent defect survived the same content-path bug: with the
untyped read, `fetchServiceReadModelWorkloadContext`
(`entity_workload_context.go`) could see `infrastructureTruncated=true` with
zero infrastructure rows (every entity non-infrastructure-typed, so none
converted). When the read-model path's own graph fallback ALSO failed, the
`err != nil` branch appended `infrastructureReadDegradedReason` to
`limitations` but never reset `infrastructureTruncated`, so the stale `true`
survived into `limitations` alongside the degrade reason -- "more rows may
exist" attached to an EMPTY panel, violating the mutual-exclusion invariant
asserted in `repository_infrastructure_degrade.go`,
`repository_context_helpers.go`,
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md)'s P2-2 entry,
and `docs/public/reference/telemetry/graph-read-safety.md`. The
`ListRepoEntitiesByTypes` fix above narrows how often this triggers but does
not close it structurally (a type-filtered read can still return zero rows
with `truncated=true` if a future type gets added to one list and not the
other), so it is fixed independently: the graph-error branch resets
`infrastructureTruncated = false`, mirroring `queryRepoInfrastructure`'s own
forced-false-on-error branch. (The round-7 entry below widens that reset into a
single post-attempt `len(infrastructure) == 0` guard, because a SUCCEEDING
graph read reaches the same state.) Proven by
`TestGetServiceContextReadModelResetsTruncatedOnGraphFallbackError`
(`service_read_model_workload_context_test.go`), using a
`nonFilteringInfrastructureContentStore` test double that returns raw
non-infrastructure rows regardless of the requested type set (to reach the
precondition independent of whether today's real SQL predicate can produce
it) plus a graph reader returning `ErrGraphUnavailable`: `limitations` must
contain `infrastructure_read_degraded` and must NOT contain
`infrastructure_truncated` -- RED before the fix (both reasons present),
GREEN after.

Third, `openapi_workload_context_keys_test.go`'s guard covered only
`fetchWorkloadContextForOperation`'s key set, not
`fetchServiceReadModelWorkloadContext`'s -- the other function that feeds the
same `WorkloadContext` schema on the read-model fallback path. That function
emits `materialization_status` and `query_basis`, neither declared in
`openapi_components_workload_session.go`. Fixed by adding a second literal
key list (`fetchServiceReadModelWorkloadContextResultKeys`) and a second test
(`TestOpenAPIWorkloadContextDeclaresEveryReadModelKey`), and declaring both
properties in the schema.

Fourth, two documentation-accuracy findings from the same review round:
`entity_workload_context.go`'s comment above the primary graph-materialized
path's `infrastructureReadDegradedReason` append claimed
`/services/{name}/story` "copies this limitations slice into its own
limitations field (`service_story_dossier.go`'s whitelist loop)". Both halves
were wrong: `buildServiceIdentity` (`service_story_dossier.go:57-74`) writes
it to `response["service_identity"]["limitations"]`, and the whitelist loop
(`service_story_dossier.go:23-36`) separately mirrors it onto the response's
own top-level `"limitations"` key (its `raw_context_limits` entry, when
present, carries only truncation metadata for that key, not the visible
copy). Verified directly with a throwaway test exercising
`buildServiceStoryResponse`: both `response["limitations"]` and
`response["service_identity"]["limitations"]` are populated, and
`answerMetadataLimitations` (`answer_metadata.go`) reads both
`data["limitations"]` and `mapValue(data, "service_identity")["limitations"]`
before deduping by reason -- either copy alone is sufficient, and the dedup
is why one degrade reason reaches `answer_metadata.partial_reasons` exactly
once. The comment now names this real two-copy chain instead of the
single-mechanism claim. Separately,
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md)'s row-bound
entry above said "on a repository with more than 500 workloads, the graph
read landed on the LIMIT" -- the same #5764 change added
`RETURN DISTINCT w.name`, so the bound is on distinct workload NAMES, not on
workload node count; corrected in place.

No-Regression Evidence: `go test ./internal/query -run
'TestQueryRepoInfrastructure|TestGetServiceContext|TestOpenAPIWorkloadContext'
-v -count=1` and `go test ./internal/query ./internal/queryplan ./internal/mcp
-count=1`.

No-Observability-Change: no span, metric instrument, or metric label was
added or changed; the `repo_infrastructure` stage log already carried
`infrastructureDegradeLogAttrs` (row_count, failure_class, truncated) from
the original #5764 change, unaffected by this fix.

### The new type-filtered SQL had no test, and four type enumerations had no cross-check (round-7 P1/P2/P3 review follow-up to #5764)

The round-6 fix above added `ContentReader.ListRepoEntitiesByTypes`
(`content_reader_by_type.go`) and shipped it with no query test. Its three
siblings all have one, and the round-6 test that drives the production Go
function reaches it through `fakePortContentStore` (`ports_test.go`), which
**reimplements the type filter in Go** -- so it passes identically whether or
not the SQL filters at all. Three mutations to the shipped SQL passed the full
`query + queryplan + mcp` suite: deleting the `len(entityTypes) == 0` guard,
dropping the `entity_id` ORDER BY tiebreaker, and inverting
`entity_type = ANY($2)` to `entity_type <> ALL($2)` -- the last of which returns
every NON-infrastructure entity, the round-6 defect in worse form.
`TestListRepoEntitiesByTypesIssuesTypeFilteredQuery` and
`TestListRepoEntitiesByTypesEmptyTypeListIssuesNoQuery` now drive the real
`*sql.DB` through `openContentReaderTestDB` and assert the query text via
`queryContainsInOrder`, mirroring
`TestListRepoEntitiesByTypeOrdersByEntityIDTiebreaker`. The empty-type-list case
asserts **no query is issued at all**, not that an empty `ANY()` happens to
match nothing: the fake driver is opened with no queued result, so any query
reaching it fails.

Rebuilding `isRepositoryInfrastructureType` from
`repositoryInfrastructureEntityTypes` (round 6) removed the last independent
enumeration that could catch asymmetric drift and replaced it with nothing.
Four hand-maintained enumerations of the same 20 types remain: the Go slice,
the Cypher `WHERE infra:... OR ...` disjunction, the
`repositoryInfrastructureEntryFromContent` switch, and the OpenAPI/doc prose.
Two more mutations passed the full suite: deleting `"TerraformLockProvider"`
from the canonical list (which now silently narrows BOTH the SQL filter and the
graph gate in one edit) and adding `"AnsiblePlaybook"` to the list without
adding it to the switch. The second is the dangerous one -- unclassified types
match the SQL predicate, get fetched, consume the 5001-row budget, then get
dropped at `ok=false`, narrowing the panel with no truncation signal and able
to push real infrastructure entities past `LIMIT`.
`TestRepositoryInfrastructureEntryFromContentClassifiesEveryCanonicalType` (plus
its non-member negative) and
`TestRepositoryInfrastructureGraphCypherMatchesCanonicalTypes` (which extracts
the `infra:` labels from the Cypher the production function actually issues and
asserts set equality with the Go list) now pin all three code enumerations to
each other.

The OpenAPI key guards were one-directional and their comment claimed the
opposite: they proved list ⊆ schema but never emitted ⊆ list, so adding
`result["undeclared_new_key"]` to `fetchWorkloadContextForOperation` or
`"readmodel_undeclared"` to `fetchServiceReadModelWorkloadContext` passed the
whole suite -- the latter silently, since that function issues no direct graph
`Run` and is not queryplan-digest-tracked.
`TestFetchWorkloadContextEmitsOnlyDeclaredKeys` and
`TestFetchServiceReadModelWorkloadContextEmitsOnlyDeclaredKeys` now drive both
production functions and assert exact set equality with the reviewed lists; the
first fixture is built to reach every conditional emit
(`deployment_evidence` needs the workload row to carry one, `limitations` needs
the infrastructure read to degrade), so equality is achievable rather than
containment. Both list comments now state what they actually guarantee.

Two smaller corrections in the same pass. The round-6 reset of
`infrastructureTruncated` fired only inside the graph-read error branch, but a
SUCCEEDING graph read reaches the same wrong state: it reports truncation on
its raw row count, before `isRepositoryInfrastructureType` drops rows, so an
over-returning backend can hand back a full `limit+1` window that classifies to
an empty panel and still puts `infrastructure_truncated` in `limitations`. The
reset moved to a single post-attempt `len(infrastructure) == 0` guard -- an
empty panel never carries a truncation signal. And in
`queryRepoInfrastructureRows`, the content read's `truncated` bool deliberately
does not survive into the graph fallback: it describes the content read's own
5001-row window, and the rows returned in that case come from the graph
instead, which reports its own bound.

Also in this pass: `query-source-coverage.yaml`'s `keyed_support / single_key /
max_results 5001` disposition for `queryRepoInfrastructureFromGraph` is now
justified in that function's doc comment (the YAML carries no free-text field
and no comments), stating plainly that the class asserts a single-key anchor
and a row cap and asserts nothing about the plan, that no closed non-hot class
carries a plan assertion, and that the one stronger designation -- promotion to
a hot `entry_ids` entry -- needs a measured plan this change does not have.
`docs/public/reference/http-api/repositories-ingesters-bundles.md` keeps its
stale `-ingesters-bundles` slug: the filename is the published URL, `mkdocs.yml`
configures no redirect plugin, and the nav entry and page title already read
"Repositories". The four history-part headers were corrected -- three still said
"of 3", and part 4's header claimed content was split out of part 2 when part 2
in fact grew by 17 net lines (446 to 463) in the same commit that created part 4
with 120 entirely new lines.

No-Regression Evidence: every mutation above re-run as a RED/GREEN pair against
`go test ./internal/query ./internal/queryplan ./internal/mcp -count=1` (exit 1
mutated, exit 0 reverted), each naming its killing test; plus
`go test -race ./internal/query -count=1`, `golangci-lint run
./internal/query/... ./internal/queryplan/...`, `go run
./cmd/capability-inventory -mode=verify`, `bash scripts/verify-route-coverage.sh`,
`ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash
scripts/verify-performance-evidence.sh`, and the strict mkdocs build.

No-Observability-Change: no span, metric instrument, metric label, or stage-log
attribute was added or changed. The `repo_infrastructure` stage log keeps the
`infrastructureDegradeLogAttrs` (row_count, failure_class, truncated) triple
from the original #5764 change; the empty-panel guard changes which value
`truncated` carries on a drifted read, not the attribute set.

### A fourth prose enumeration drifted, bind args were untested, and a label regex admitted whitespace (round-8 review follow-up to #5764)

Four smaller findings from a fifth review pass on the same infrastructure
panel.

First, the OpenAPI `infrastructure` field description
(`openapi_components_workload_session.go`) named seven families --
Kubernetes, Terraform, ArgoCD, Helm, Kustomize, Crossplane, CloudFormation --
missing Terragrunt, in the same commit where three other copies of this same
prose (the doc comment above `repositoryInfrastructureEntityTypes` in
`repository_infrastructure.go`, the HTTP API reference page, and the
telemetry reference page) all correctly named eight families including
Terragrunt. The round-7 entry above says the new tests pin "all three code
enumerations" -- the Go type slice, the Cypher label disjunction, and the
classification switch -- so this fourth, prose enumeration was left
deliberately unpinned, and it drifted the first chance it had. Fixed by
adding Terragrunt to the OpenAPI description.

Whether to also add a mechanical cross-check for this one was considered and
rejected. The three code enumerations round 7 pinned are extractable
one-to-one: each names the same literal Go identifiers
(`TerraformResource`, `ArgoCDApplication`, ...), so a regex or set-equality
test compares them without adding new judgment. The OpenAPI description and
the two docs pages instead use human-readable FAMILY names -- "Terraform"
covers ten different Go types in the canonical list, "ArgoCD" covers two.
Turning that into a testable assertion needs a hand-written
family-to-type-prefix map, which would be a fifth hand-maintained enumeration
of the same 20 types -- the exact pattern that has now drifted twice. Adding
it would not close this risk, it would relocate it. The residual exposure:
nothing mechanically stops a future addition to
`repositoryInfrastructureEntityTypes` from landing without a matching update
to these four prose copies (`openapi_components_workload_session.go`,
`repository_infrastructure.go`'s doc comment,
`docs/public/reference/http-api/repositories-ingesters-bundles.md`, and
`docs/public/reference/telemetry/graph-read-safety.md`). This is a known,
accepted gap, not a fixed one -- an agent or reviewer adding a type to that
list should grep all four for the family-name prose by hand.

Second, `ContentReader.ListRepoEntitiesByType` and `ListRepoEntitiesByTypes`
(`content_reader_by_type.go`) had a test on their SQL TEXT but nothing on
their bind ARGUMENTS: `contentReaderConn.QueryContext`
(`content_reader_driver_test.go`) discarded `[]driver.NamedValue` entirely.
Mutating `ListRepoEntitiesByTypes` to
`cr.db.QueryContext(ctx, query, pq.Array(entityTypes), repoID, limit)` --
same SQL string, arguments swapped -- left the whole `internal/query` package
green. In production this sends the entity-type array literal to the
`repo_id = $1` placeholder and the repo ID to `entity_type = ANY($2::text[])`,
which Postgres rejects as invalid array input, and the caller,
`queryRepoInfrastructureFromContent`, treats any error the same as "no
infrastructure entities" (`if err != nil || len(entities) == 0 { return nil,
false }`) -- the infrastructure panel would go empty with no failure
disclosed, on every call, for as long as the swap shipped. Fixed by adding an
opt-in `wantArgs []driver.Value` field to `contentReaderQueryResult` and a
`contentReaderCheckArgs` assertion in `QueryContext`
(`content_reader_driver_test.go`), tolerant of the `int`/`int64` conversion
`driver.DefaultParameterConverter` performs (mirroring `numericDriverValue`
in `content_reader_cross_repo_test.go`). `wantArgs` stays nil unless a test
sets it, so the roughly 80 other test files sharing this fake driver are
unaffected.  `TestListRepoEntitiesByTypeOrdersByEntityIDTiebreaker` and
`TestListRepoEntitiesByTypesIssuesTypeFilteredQuery` now set it. Proven by
the arg-swap mutation above: RED (`content_reader_by_type_test.go:114: ...
query bind arg $1 = "{\"K8sResource\",\"TerraformResource\"}" (string), want
"repo-1" (string)`, exit 1) before the fix reached this test, GREEN (exit 0)
after reverting.

Folded into the same fix (P3-6): `entity_type = ANY($2)` gained the
`::text[]` cast every one of the other 14 `= ANY($N)` call sites in this
package already carries. Postgres already infers `text[]` here from the
UNKNOWN-typed `pq.Array` argument against a `text` column, so the missing
cast was not a live defect -- just the one site out of step with the rest of
the package, now consistent.

Third, `TestListRepoEntitiesByTypesIssuesTypeFilteredQuery`'s doc comment
claimed its named mutations "has to fail here" without qualifying what "here"
covers. `queryContainsInOrder` is a SHAPE check on the SQL text (ordered
substring presence), not a full-text or behavioral one: appending ` OR true`
to the WHERE clause keeps all four listed fragments present in the same
order, so the assertion still passes -- confirmed directly, exit 0 both
before and after adding then removing the `OR true` mutation. The comment now
names exactly the three mutations the assertion catches instead of implying
exhaustive coverage, and points at the new `wantArgs` check as the separate,
narrower net that catches an argument-order swap the SQL-text check cannot
see. This is a known limitation of the fragment-based assertion, shared by
every test in this package using the same `queryContainsInOrder` pattern, not
a defect unique to this one test.

Fourth, `repositoryInfrastructureCypherLabelPattern`
(`repository_infrastructure_type_registry_test.go`) used
`infra:([A-Za-z0-9_]+)` to extract the `infra:X` disjuncts from the
production Cypher for the set-equality check against
`repositoryInfrastructureEntityTypes`. Cypher accepts whitespace between a
variable and its label colon, so a disjunct written `OR infra
:AnsiblePlaybook` (space before the colon) evades the regex while still
changing what the graph read matches: `cypherLabels` stays at 20 members, set
equality holds, and the test passes while the Cypher now matches an extra
label. Fixed by widening the pattern to `infra\s*:`. Proven directly: adding
`OR infra :AnsiblePlaybook` to the disjunction with the fixed regex in place
fails the test (`cypher labels [AnsiblePlaybook ArgoCDApplication ...] ...
sets differ in size`, exit 1); removing it restores a pass (exit 0).

No-Regression Evidence: `go test ./internal/query -run
'TestListRepoEntitiesByType|TestRepositoryInfrastructure' -v -count=1` (26
passed) as a baseline; the arg-swap, `OR true`, and space-before-colon
mutations each re-run as RED/GREEN (or PASS/PASS for `OR true`) pairs via
`rtk proxy go test ./internal/query -run '<test name>' -v -count=1`, exit
codes captured directly per invocation, as detailed above; plus `go build
./...`, `go test ./internal/query ./internal/queryplan ./internal/mcp
-count=1`, `go test -race ./internal/query -count=1`, `golangci-lint run
./...`, `go run ./cmd/capability-inventory -mode=verify`, `bash
scripts/verify-route-coverage.sh`,
`ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash
scripts/verify-performance-evidence.sh`, and the strict mkdocs build via
`ESHU_DOCS_BUILD_BASE=origin/main bash
scripts/verify-docs-build-changed.sh`.

No-Observability-Change: no span, metric instrument, metric label, or
stage-log attribute was added or changed. This round only touched an OpenAPI
description string, test-only fake-driver plumbing, two doc comments, a test
regex, and one query-text cast that Postgres already resolved the same way at
runtime.
