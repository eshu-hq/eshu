# Documentation Semantic Observations Readback

Target-scoped documentation finding readback carries a bounded `related_facts`
preview when raw documentation evidence references the selected repository,
service, or target but no admitted finding exists. That preview includes
`semantic.documentation_observation` rows when their evidence refs match the
target.

Semantic observation rows remain provenance only in this readback. They do not
increment `finding_count`, and callers still see `documentation_findings_absent`
until reducer-owned admission creates a `documentation_finding`.

`semantic.code_hint` rows stay outside documentation target readback. Code hints
belong to code-oriented surfaces because they are non-canonical relationship
hints, not documentation provenance.

No-Regression Evidence: `go test ./internal/query -run
'TestBuildDocumentationTargetFactsSQLIncludesSemanticObservationProvenance|TestBuildStoryTargetDocumentationKeepsSemanticObservationProvenanceOnly|TestBuildDocumentationTargetFactsSQLIsTargetScopedAndBounded'
-count=1` proves target-scoped documentation readback includes semantic
documentation observations as related provenance, excludes semantic code hints,
keeps target scoping and pagination, and does not promote observations into
documentation findings.

No-Observability-Change: this readback expansion reuses the existing
`query.documentation_findings` request span and Postgres query duration
instrumentation. It adds no route, MCP tool, queue, worker, provider call,
graph write, metric instrument, metric label, runtime flag, or deployment knob.
