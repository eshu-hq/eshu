# 8315 probe dedup-key namespacing — no-regression note

Follow-up to `8315-du-target-probe.md`: the Endpoint probe dedup key is
namespaced with an `e` prefix so it cannot collide with a Function
`f`-prefixed uid key in the shared `seen` map.

No-Regression Evidence: the change touches only a Go-map-internal dedup
key. Every emitted Cypher statement is byte-identical before and after for
all previously non-colliding inputs (match strings unchanged in the diff);
the single behavior delta is the previously-colliding case
(`repo_id: "f"`, path equal to a Function uid), which now emits both the
Endpoint and the Function probe instead of dropping one. Baseline: new
regression test
`TestEdgeWriterHandlesRouteProbeDedupNamespacesEndpointAndFunction`
against the unpatched builder emits 1 probe (FAIL, want 2). After: 2
probes, both anchors asserted. Backend: NornicDB/Neo4j re-proof not
required — no statement text, anchor, index, or cardinality changes.
`go test ./internal/storage/cypher/edge/writer/` PASS (with `-race`);
`TestStatementBuildersManifestMatchesProduction` PASS with the re-derived
`source_sha256`.

No-Observability-Change: `checkBatchTargetsPresent`, the error classes,
and the `SharedEdgeTargetMiss` instrument are untouched; no new metric,
span, or log line is added.
