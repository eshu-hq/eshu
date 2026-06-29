# Issue 3987 Structural Edge Retraction Evidence

Performance Evidence: baseline is `origin/main` before this branch. After
measurement on the local NornicDB/Postgres B-7 substrate used by
`scripts/verify-golden-corpus-gate.sh`, the full 20-repo no-provider-key corpus
gate completed in 35s against the 30m ceiling with `103 pass, 0 required-fail`,
terminal queue counts `fact_work_items_residual=0` and
`shared_projection_intents_nonterminal=0`, and graph truth counts
`MANAGES=2`, `ATLANTIS_DEPENDS_ON=1`, `USES_WORKFLOW=1`, and `INHERITS=6`.
The changed hot input shape is bounded to the AtlantisProject source uids in one
canonical materialization; first-generation projections emit no retracts, and
non-Atlantis repositories still emit no Atlantis statements.

No-Regression Evidence: Atlantis canonical structural edges now mirror the
existing GitLab generation-scoped stale-edge cleanup pattern. For each
projecting `AtlantisProject` uid, the writer emits three bounded retract
statements before the MERGE upserts: `MANAGES`, `ATLANTIS_DEPENDS_ON`, and
`USES_WORKFLOW`, each anchored by `AtlantisProject.uid`, scoped to
`evidence_source='projector/canonical'`, and guarded by
`generation_id <> current_generation`. First-generation projections skip the
retract because no older source-local Atlantis edge can exist yet. The input
cardinality is the number of AtlantisProject nodes in one source-local
materialization; non-Atlantis repositories still emit no Atlantis statements.
IMPORTS, parameters, and class member containment remain on their existing
current-file or current-parent cleanup paths, covered alongside the new Atlantis
regression by
`go test ./internal/storage/cypher -run
'TestAtlantisEdgeStatements|TestCanonicalNodeWriterRetractCoversStructuralFamiliesFromIssue3987|TestCanonicalNodeWriterRefreshesOnlyStaleEntityContainmentEdges|TestCanonicalNodeRefreshStructuralEdges'
-count=1`.

Observability Evidence: the new retract statements run through the existing
`CanonicalNodeWriter.Write` structural_edges phase, statement metadata,
canonical write spans, graph query duration metrics, retry wrapping, and phase
failure logs. No metric name, metric label, worker, queue domain, runtime knob,
backend branch, or new graph-write route is added.
