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

## Scope of what this actually enables (codex review P1 on PR #5809)

This fix stops the drop; it does NOT by itself make CI-backed lineage work end to
end, and the distinction matters:

- **BUILT_FROM: reachable.** `containerImageBuiltFromRows` is not owner-scoped and
  fans out per decision in whichever intent sees the evidence, so CI provenance
  computed in the CI-scoped intent is usable. This is why rc-165 works and why
  #5796 / PR #5807 depends on this fix.
- **DERIVED_FROM: still NOT reachable from CI evidence.**
  `projectContainerImageDerivedFromEdges` is owner-scoped
  (`repositoryIDFromReducerScope(intent.ScopeID)`), so a CI scope projects
  nothing; and the repository-scoped intent cannot recover the provenance because
  `identityFactFilterSQL` admits no `ci.*` kinds, so `ci.run`/`ci.artifact` are
  never loaded cross-scope.

So #5460's evidence doc and the `BuildProvenanceRepositoryIDs` comment over-claim
when they list a CI run as a DERIVED_FROM child qualifier: today that is true only
for BUILT_FROM. Tracked in **#5810** rather than silently carried. A unit test
cannot see this, because it builds decisions from all facts in one call with no
scope separation — the same false-green shape this fix itself exposed.

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
