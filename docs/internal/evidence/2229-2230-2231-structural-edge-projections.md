# DOCUMENTS, INSTANTIATES, IMPLEMENTS, And EXPLAINS Edge Projection Evidence (#2229, #2230, #2231)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## DOCUMENTS edges (#2231)

`DocumentationEdgeMaterializationHandler` (domain
`documentation_materialization`) projects `DOCUMENTS` edges from documentation
entity mentions to the code entities or workloads they resolve to, into the new
`documentation_edges` shared-projection domain. It is correlation-truth strict:
only a mention whose `resolution_status` is `exact` and that carries exactly one
candidate ref produces an edge; ambiguous, unmatched, and multi-candidate
mentions do not. A candidate whose kind is `service` is dropped because no
Service graph node exists — provenance never fabricates a node. The source
`DocumentationSection` node is identity-only (uid derived from the logical
document/section pair, plus a bounded excerpt handle); section bodies stay in
the Postgres content/fact store (design 430). Documentation is scope-scoped, so
full-refresh retracts anchor on `section.scope_id`, not a repository id. Delta
generation retracts are narrower: changed and deleted git documentation paths
are mapped to stable document ids, while storage also supports section-uid scoped
cleanup for future sources that emit explicit section deltas. The repository
delta input is file-granular, so changed documentation files use document-id
cleanup to remove stale edges from sections deleted inside the same file.
External documentation sources such as Confluence are not matched by repository
path metadata.

No-Regression Evidence: `go test ./internal/reducer -run 'DocumentationEdge' -count=1`,
`go test ./internal/storage/cypher -run 'Documentation' -count=1`, and
`go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer -count=1`
fail before the domain exists and pass after. The handler runs one bounded
fact-kind scan (`documentation_entity_mention`) per scope generation, builds
edge rows with no new graph read, and writes through the existing batched
`UNWIND … MERGE` shared-projection edge path; cardinality is bounded by exact
mention count. The domain-list guard tests were updated to include the new
domain. For #2321 delta retraction, `go test ./internal/reducer -run
'TestDocumentationMaterializationHandler(ScopesDeltaRetractToDocuments|DeletedOnlyDeltaRetractsWithoutWrites)|TestBuildDocumentation(RetractRowsKeepsMalformedDeltaScoped|DeltaScopeIgnoresExternalDocumentPathMetadata)'
-count=1` and `go test ./internal/storage/cypher -run
'Test(BuildRetractDocumentationEdgesBy(DocumentID|SectionUID)|EdgeWriterRetractEdgesDocumentation(DeltaUses(Document|Section)Scope|RejectsDeltaWithoutIdentity))'
-count=1` prove identity-scoped cleanup, deleted-document cleanup without
writes, external-doc path isolation, and fail-closed malformed delta rows.

No-Observability-Change: the new domain reuses the shared-projection edge writer
and its existing batch counters, statement summaries, and graph query-duration
metrics, plus a `documentation materialization started`/`completed` log pair in
the existing reducer log style. Delta documentation retraction uses the same
executor path, retry classification, timeout handling, and query-duration
metrics. No new metric instrument or label is added.

## INSTANTIATES edges (#2229)

`extractGenericCodeCallRows` now additionally emits an `INSTANTIATES` edge
`(Function|File)-[:INSTANTIATES]->(Class|Struct|Enum)` when a `constructor_call`
resolves to a concrete type. The edge is additive: the existing `CALLS` edges to
the type and its constructor are unchanged, so call-graph reachability is
preserved while construction becomes separately queryable. It carries
`type_inferred` provenance (ADR #2222) because it rides the constructor
resolution, and is deduplicated per caller→type pair regardless of how many
construction sites exist.

No-Regression Evidence: `go test ./internal/reducer -run 'Instantiates|CodeCall' -count=1`
and `go test ./internal/storage/cypher -run 'Instantiates|CodeCall|RetractCodeCall' -count=1`
fail before the edge exists and pass after. The change adds one bounded helper
call per constructor-call row inside the existing code-call pass (one endpoint
type lookup, no new resolution, scan, or graph query); the INSTANTIATES template
is the same batched `UNWIND … MERGE` keyed on `uid` as the code-call templates,
and the code-call retract was extended so re-projection stays idempotent.
Cardinality is bounded by constructor-call sites (gap analysis #2228).

No-Observability-Change: INSTANTIATES rides the existing code-call edge-write
path; the per-statement edge summary now reports `relationship=INSTANTIATES`, and
the existing code-call materialization completion log, edge batch counters, and
graph query-duration metrics expose the writes with no new metric or label.

## IMPLEMENTS edges (#2229)

`ExtractInheritanceRows` now also emits `IMPLEMENTS` edges
`(Class|Struct|Enum)-[:IMPLEMENTS]->(Interface)` from the parser-emitted
`implemented_interfaces` metadata, reusing the same content-entity scan, the same
name-resolution `entityIndex`, and the same inheritance edge-write domain as
`INHERITS`/`OVERRIDES`/`ALIASES`. An implemented interface that does not resolve
to a known `Interface`/`Protocol` entity creates no edge. The inheritance retract
(`buildInheritanceRetractStatements`) was extended to clean stale `IMPLEMENTS` edges
so re-projection stays idempotent.

No-Regression Evidence: `go test ./internal/reducer -run 'Inheritance|Implements' -count=1`,
`go test ./internal/storage/cypher -run 'Inheritance|Implements|RetractInheritance' -count=1`,
and `go test ./internal/parser/... -run 'EmitsImplementedInterfaces' -count=1` fail
before the IMPLEMENTS field/edge exist and pass after. The change adds one
metadata read and a bounded inner loop over a type's declared interfaces inside
the existing per-entity pass; it adds no new content-entity scan, no new graph
query shape (the IMPLEMENTS template is the same batched `UNWIND … MERGE` keyed on
`uid` as the inheritance templates), and no new traversal. Edge volume is bounded
by interface count per type, far below call volume (gap analysis #2228).

No-Observability-Change: IMPLEMENTS rides the existing inheritance edge-write
path. The per-statement edge summary now reports `relationship=IMPLEMENTS`, and
the existing inheritance materialization spans, edge batch counters, and graph
query-duration metrics expose the writes with no new metric instrument or label.

Promotion note (#2867): inheritance edges (INHERITS/IMPLEMENTS/OVERRIDES/ALIASES)
are no longer written directly by the handler. `InheritanceMaterializationHandler`
now emits durable shared-projection intents — one per-repo whole-scope refresh
intent that owns the retract plus one write-only per-edge intent under an
edge-unique file-scoped partition key — and the partitioned shared-projection
runner projects them through the #2898 refresh fence. The retract Cypher and edge
templates are unchanged; only the path that reaches them moved. See
`shared-projection.md` (SQL and inheritance domains).

The refresh fence is generation-local (#5554). Exact same-generation retries
reuse deterministic intent IDs and remain completed; if a later generation
reuses `source_run_id`, its per-edge rows wait for that generation's own refresh
completion. The N=1/2/4 Ifá matrix drives the SQL gen-2 delta after gen 1 and
asserts the accumulated exact seven-edge set before comparing graph digests.

## EXPLAINS edges (#2230)

`RationaleEdgeMaterializationHandler` (domain `rationale_materialization`)
projects `EXPLAINS` edges from intent-comment rationale to the code entities the
comments precede, into the new `rationale_edges` shared-projection domain. The
parser attaches `rationale_comments` (WHY/HACK/NOTE/TODO/FIXME, line-adjacent
only) to functions/classes; the field flows through the content-entity metadata
passthrough. Each distinct (entity, comment kind, comment text) yields one
identity-stable `Rationale` node (`rationale:<entity>:<kind>:<excerpt_hash>`,
identity-only) and one EXPLAINS edge. The comment text stays in the Postgres
content/fact store (design 430); the graph node carries identity and a bounded
`excerpt_hash` only. Rationale is repo-scoped for full refreshes, so those
retracts anchor on `rationale.repo_id`; delta generation retracts instead
anchor on target code-entity `path` values carried from the repository delta
fact so one changed file cannot delete another file's EXPLAINS truth.

No-Regression Evidence: `go test ./internal/reducer -run 'RationaleEdge' -count=1`,
`go test ./internal/storage/cypher -run 'Rationale' -count=1`,
`go test ./internal/parser -run 'PythonEmitsRationale' -count=1`, and
`go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer -count=1`
fail before the domain exists and pass after. For #2257 delta retraction,
`go test ./internal/reducer -run
'TestRationaleMaterializationHandler(ScopesDeltaRetractToFiles|DeletedOnlyDeltaRetractsWithoutWrites)|TestBuildRationaleRetractRowsKeepsMalformedDeltaScoped|TestLoadRationaleMaterializationFactsUsesSingleLegacyFallback'
-count=1` and `go test ./internal/storage/cypher -run
'Test(BuildRetractRationaleEdgeStatementsByFilePath|RationaleRetractCoversEveryWriteTargetLabel|EdgeWriterRetractEdgesRationale(DeltaRunsPerLabelStatementsSequentially|RejectsDeltaWithoutFilePaths))'
-count=1` prove deleted-only delta cleanup, file-path-scoped graph retraction,
and fail-closed malformed delta rows. The handler runs one repository plus
content-entity fact-kind load when the backing store supports kind filters, or
one legacy full-generation fallback load otherwise, builds edge rows with no
new graph read, and writes through the existing batched `UNWIND … MERGE`
shared-projection edge path; cardinality is bounded by intent-comment count
and, for delta retracts, by the changed/deleted file-path count.

No-Observability-Change: the new domain reuses the shared-projection edge writer
and its existing batch counters, statement summaries, and graph query-duration
metrics, plus a `rationale materialization started`/`completed` log pair in the
existing reducer log style. No new metric instrument or label is added.
