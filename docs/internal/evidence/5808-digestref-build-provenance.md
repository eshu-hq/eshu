# #5808 CI-run build provenance was dropped for digest-only artifacts

`addContainerImageDigestRef` propagated `sourceRepositoryIDs`, `workloadIDs`, and
`serviceIDs` from its `anchors` argument but silently dropped
`buildProvenanceRepositoryIDs`. A `ci.artifact` fact carries a digest and no image
reference, so CI-run build evidence reaches identity through exactly that path:
`addCICDArtifactImageReference` deliberately sets
`anchors.buildProvenanceRepositoryIDs = append(..., run.repositoryID)` and then
calls `addContainerImageDigestRef`, which threw the value away.

`BuildProvenanceRepositoryIDs` (#5460) is documented as being populated from two
build-evidence sources — an OCI config source label, or a CI run that reported
producing the digest. The second source did not work, so #5460's own evidence doc
and field comment over-claimed: an image whose only build evidence is a
`ci.run`/`ci.artifact` join was treated as if nobody built it. rc-167 passes on the
OCI-label path, which is why the corpus never exposed the gap. Found while
rebasing PR #5807 (#5796), whose BUILT_FROM narrowing would have zeroed rc-165 —
whose only evidence is this CI path.

## Behavior-change proof (failing-then-green)

- BEFORE: `TestBuildProvenanceFromCICDRunDigestOnlyArtifact` fails —
  `BuildProvenanceRepositoryIDs = []string(nil)`, want `repository:r_69256c06`.
- AFTER: passes. Full `go test ./internal/reducer/ -count=1` green.

The test mirrors rc-165's corpus shape: a `ci.run` carrying `repository_id`, a
`ci.artifact` carrying only the digest, and the OCI manifest observation that
resolves the digest to an `exact_digest` decision.

## No-Regression Evidence

No-Regression Evidence: two appends plus one `uniqueSortedStrings` on a slice that
is empty for every fact except a CI-anchored artifact, inside the existing
per-fact extraction loop. No new query, graph read, batch, lease, or worker path.

- Baseline / after: `go test ./internal/reducer -count=1` ~2.8s → ~3.0s, within
  run-to-run noise on the development machine.
- Backend / version: none — in-process reducer extraction over already-loaded
  facts; issues no Cypher and no Postgres query.
- Input shape / corpus: B-7 golden corpus, 28-repo minimal corpus.
- Terminal row counts, proven on the live gate (not inferred):
  `rc-165` BUILT_FROM **count=2** PASS, `rc-167` DERIVED_FROM **count=1** PASS
  with `source_tool=oci` and `attribution_basis=repository_single_base`,
  `edge_count_BUILT_FROM: 2` inside the snapshot range `[1,10]`,
  `fact_work_items_residual: residual=0 (dead_letter=0)`,
  `summary: 495 pass, 0 required-fail`. No snapshot update needed: BUILT_FROM
  still gates on `SourceRepositoryIDs` on `main` (the narrowing is #5796/#5807),
  so populating this field changes no edge today — it makes the CI-run tier
  actually available to the gate that #5807 introduces.
- Flake note: an earlier run of this same gate failed with `residual=1,
  dead_letter=1` after `drains: not satisfied after 10m0s`. That is the known
  resource-starvation signature on a shared host; the clean re-run completed in
  101s with `dead_letter=0` and zero non-terminal work items in
  `fact_work_items`. Recorded rather than hidden.

## No-Observability-Change

No-Observability-Change: no metric, span, log, or status field is added or
changed. The `container_image_identity` domain remains covered by
`eshu_dp_container_image_identity_decisions_total`,
`eshu_dp_reducer_executions_total`, and `eshu_dp_reducer_run_duration_seconds`;
this fix only stops discarding a field already carried by the same decision.
