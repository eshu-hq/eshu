# Repository context helpers: read errors become named partial reasons (#6810)

Change: the repository context graph helpers (`go/internal/query/repository`:
`queryRepoConsumers`, `queryRepoRelationshipOverview` in both directions,
`queryRepoDeployableUnitRelationshipOverview`, `QueryRepoDependencies`,
`queryRepoLanguageDistribution`, `QueryRepoSourceToolBreakdown`,
`QueryRepoEntryPoints`, `QueryRepoAPISurface`) discarded a graph read error
with `if err != nil || len(rows) == 0 { return empty }`, so a deadlined or
unavailable read rendered as an authoritative empty list and
`/api/v0/repositories/{id}/context` reported `partial_reasons: []`. Each
helper now returns a degraded flag alongside its rows (an error yields an
empty result and `true`; a healthy empty read yields `false`), and every
caller names the failed read: the repository context handler appends one
reason per read (`consumers_read_degraded`,
`relationship_overview_read_degraded`, `relationships_read_degraded`,
`languages_read_degraded`, `source_tool_breakdown_read_degraded`,
`entry_points_read_degraded`, `api_surface_read_degraded`,
`deployable_unit_relationships_read_degraded`) to `partial_reasons`, the
repository story appends `relationships_read_degraded` to its limitations, the
workload/service context appends `relationships_read_degraded` and the service
enrichment `api_surface_read_degraded` to `limitations` (promoted into
`partial_reasons` by the existing `ContextPartialReasons` path), and the
service tech fingerprint returns the language and source-tool reasons for the
service context handler to record. The Cypher text of every read is
unchanged; only the error branch of each helper and the callers' bookkeeping
changed.

Not converted, with the reason: `QueryRepoInfrastructureFromContent`
(`infrastructure.go`) turns a content-store error into an empty result, but
both of its callers (`repository/infrastructure.go`,
`entity/workload_context.go`) then fall through to
`queryRepoInfrastructureFromGraph`, the authoritative read, whose own failure already degrades to
`infrastructure_read_degraded`; a content error followed by an empty graph
answer is a true empty panel, not a hidden failure. It stays as the #5764
P2-3 follow-up recorded in that file.

Callers that read the empty list: the context handler's overview fallback
(`if len(relationshipRows) == 0 { relationshipRows = result["relationships"] }`)
and the fingerprint builders still receive the same empty slices on a failed
read and behave as before; they now do so beside a named reason. No caller
distinguished a failed read from an empty one, because none could.

No-Regression Evidence: no query text, anchor, projection, ordering, limit or parameter changed in any helper; the diff on each Cypher constant is empty (`git diff origin/main..HEAD -- go/internal/query/repository ':!*_test.go' | rg '^[-+].*(MATCH|RETURN|ORDER BY|LIMIT)'` prints nothing; the only such lines in the whole diff are the test's marker literals), so the graph work per request is byte-identical and the only added work is appending a string to a slice on the failure path.

Observability Evidence: each repository context stage log (`repository query stage completed`, stages `entry_points`, `relationships`, `relationship_overview`, `consumers`, `api_surface`, `languages`, `tech_fingerprint`) and the story `relationships` stage now carry `failure_class=<reason>` when the read failed, the same key the infrastructure stage already uses; the service `graph_api_surface` and `repo_dependencies` stage logs carry it too. The response-level signal is the new `partial_reasons` / `limitations` values above. No metric or span is added.

Regression: `TestRepositoryContextReportsDegradedGraphReads` (fake reader
returning a deadline error for each of the seven context reads) failed on
origin/main with `partial_reasons = []` lacking every reason and passes with
the fix; `TestRepositoryContextEmptyReadsAreNotDegraded` (healthy empty reads)
passes on both, so a true empty answer still carries no reason.
