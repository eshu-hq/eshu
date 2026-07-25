# #5796 BUILT_FROM build-provenance narrowing: correctness evidence

`containerImageBuiltFromRows` (`go/internal/reducer/container_image_provenance_edges.go`)
projected one `ContainerImage-[:BUILT_FROM]->Repository` row per entry in
`decision.SourceRepositoryIDs`. That field is broader than "repositories that
built this image": `containerImageSourceRepositoryIDs`
(`container_image_identity_evidence.go`) also anchors on
`repositoryIDFromReducerScope(envelope.ScopeID)`, so a repository whose
Kubernetes manifest merely REFERENCES a digest-pinned third-party image (a
`postgres@sha256:...` in a deployment manifest, say) lands in
`SourceRepositoryIDs` exactly like a genuinely built image. `BUILT_FROM` could
therefore assert a build relationship that does not exist for any repository
that only deploys an image it did not build. This is the same false-positive
class codex flagged as a P1 against the sibling `DERIVED_FROM` projection on
PR #5793 (#5460); that PR fixed its own side by adding
`ContainerImageIdentityDecision.BuildProvenanceRepositoryIDs`, gated on genuine
build evidence only, and left `BUILT_FROM` for this issue (#5796) to keep that
diff scoped.

## Fix

PR #5793 (#5460) has since merged to `main` (`9d957d0e9`), bringing
`BuildProvenanceRepositoryIDs` and its two build-evidence population sites
with it:

- `extractOCIConfigBuildProvenanceRefs` (`container_image_identity_provenance.go`):
  an OCI config source label the image itself carries, matched to exactly one
  DISTINCT repository via `matchOCIConfigSourceRepositoryByDistinctRepository`.
- `addCICDArtifactImageReference` (`container_image_identity_typed_evidence.go`):
  a CI run that reported producing this artifact digest
  (`ci.run.repository_id`, joined by `ci.artifact.artifact_digest`).

While proving this PR's tests against that merge, a real gap surfaced:
`addContainerImageDigestRef` (the path used for a digest-only fact, no image
ref) dropped `buildProvenanceRepositoryIDs` on the floor -- it copied
`sourceRepositoryIDs`/`workloadIDs`/`serviceIDs` from `anchors` but not the
build-provenance field, so the CI-run tier computed by
`addCICDArtifactImageReference` never reached a decision for a digest-only
`ci.artifact` (exactly rc-165's shape). That gap is now fixed on `main` by
#5808/PR #5809 (`9b1d3c372`), found and reported during this PR's own
verification. A related gap (CI provenance still cannot reach `DERIVED_FROM`,
because that projection is owner-scoped and its fact filter admits no `ci.*`
kinds cross-scope) was scoped separately as **#5810** and is explicitly out
of scope here -- `BUILT_FROM` is unaffected by it, since `BUILT_FROM` is not
owner-scoped and fans out in whichever intent sees the evidence.

This PR's diff is therefore mostly the `BUILT_FROM` row-builder gate:
`containerImageBuiltFromRows` now iterates `decision.BuildProvenanceRepositoryIDs`
instead of `decision.SourceRepositoryIDs`. Nothing else reads
`SourceRepositoryIDs` off this decision type for the `BUILT_FROM` edge, so
narrowing loses no other intended `BUILT_FROM` consumer. (An earlier revision
of this branch also re-implemented the shared field and its population sites,
because it was cut before #5793 merged; that duplication was removed on
rebase in favor of main's reviewed implementation, now also carrying #5808's
fix.)

## Review follow-up: SLSA build provenance was not propagated (P1)

Review (codex and a human reviewer) on this PR found a third build-evidence
gap in the same family as #5808: `applySLSADigestRevision`
(`container_image_identity_registry.go`) appended a SLSA anchor's
`sourceRepositoryIDs` to `decision.SourceRepositoryIDs` but never to
`decision.BuildProvenanceRepositoryIDs`. A passed-signature SLSA attestation
(`extractSLSADigestAnchorsWithQuarantine` only ever records an anchor after
confirming `verificationStatusByStatement[...] == "passed"`) is the
STRONGEST build evidence in this domain -- stronger than an OCI config
source label or a CI-run join, both of which already reached
`BuildProvenanceRepositoryIDs`. Without the fix, an exact-digest image whose
ONLY attribution was verified SLSA would have its `BUILT_FROM` edge retracted
with no replacement under this PR's gate -- a real accuracy regression for
the strongest evidence tier.

Fixed by appending `anchor.sourceRepositoryIDs` to
`decision.BuildProvenanceRepositoryIDs` alongside the existing
`SourceRepositoryIDs` append, gated on nothing further since the anchor
itself is only ever populated from passed-verification evidence.
`TestApplySLSADigestRevisionPopulatesBuildProvenanceRepositoryIDs`
(`container_image_identity_slsa_test.go`) is the failing-then-green proof:
before the fix it failed with `BuildProvenanceRepositoryIDs = []string(nil)`;
after, the repository is present and `containerImageBuiltFromRows` emits one
row for it. `TestApplySLSADigestRevisionUnverifiedDoesNotConferBuildProvenance`
is the negative twin, mirroring the existing
`TestApplySLSADigestRevisionNoOverrideWithFailedVerification` gate exactly: a
FAILED verification must not confer build provenance either, and it never
reaches `applySLSADigestRevision` as an anchor at all.

## Review follow-up: benchmark helper broke under the new gate (P2)

`benchContainerImageIdentityDecisions` (`provenance_edges_bench_test.go`)
populated only `SourceRepositoryIDs`, so under the narrowed gate all 5,000
benchmark decisions yielded zero rows and
`BenchmarkContainerImageBuiltFromRows`'s `len(rows) != 5000` check failed
every call (the identical root cause #5460 hit on
`BenchmarkContainerImageDerivedFromRows`, already fixed there). Fixed by
populating `BuildProvenanceRepositoryIDs` alongside `SourceRepositoryIDs` in
the helper. Verified:

```
go test ./internal/reducer/ -bench 'BenchmarkContainerImageBuiltFromRows' -run '^$' -benchtime=1x -count=1
# BenchmarkContainerImageBuiltFromRows-12    1    1342333 ns/op    1960960 B/op    25001 allocs/op
# PASS
```

## Proof: failing-then-green

`TestContainerImageBuiltFromRowsExcludesRuntimeReferenceOnlyRepository`
(`container_image_provenance_edges_test.go`) constructs a decision whose only
repository anchor is a runtime/deploy reference
(`SourceRepositoryIDs: []string{"repository:deploy-only"}`,
`BuildProvenanceRepositoryIDs` empty) and asserts zero `BUILT_FROM` rows.
Before the fix (gating on `SourceRepositoryIDs`) it failed with `len(rows) = 1`;
after the fix it is green. `TestContainerImageBuiltFromRowsFansOutMultipleBuildProvenanceRepositories`
gives `SourceRepositoryIDs` and `BuildProvenanceRepositoryIDs` deliberately
different values and asserts the emitted rows come only from the latter,
proving the row builder reads the narrowed field and not the broad one.

`TestContainerImageBuiltFromRowsFromCICDRunBuildProvenanceEndToEnd`
(`container_image_provenance_edges_test.go`) proves the positive direction end
to end through the real evidence-extraction pipeline (not a hand-built
decision): a `ci.run` reporting it produced this artifact digest reaches
`BuildProvenanceRepositoryIDs` via `BuildContainerImageIdentityDecisions`, and
`containerImageBuiltFromRows` on that decision still emits one row. This is
the exact evidence shape rc-165 depends on (see below), so it is the
unit-level proof that rc-165 survives the narrowing. The OCI-config-source-label
positive case and the Kubernetes-reference negative case are already covered
by main's `container_image_build_provenance_test.go`
(`TestBuildContainerImageIdentityDecisionsBuildProvenanceFromOCISourceLabel`,
`TestBuildContainerImageIdentityDecisionsKubernetesReferenceIsNotBuildProvenance`),
so this PR does not duplicate them.

```
cd go && go test ./internal/reducer/ -run \
  'TestContainerImageBuiltFromRows|TestProjectContainerImageBuiltFromEdges' \
  -v -count=1
# all PASS; TestContainerImageBuiltFromRowsExcludesRuntimeReferenceOnlyRepository and
# TestContainerImageBuiltFromRowsFansOutMultipleBuildProvenanceRepositories were the
# two that failed before the gating change (len(rows) = 1, want 0 / want 2 respectively).

cd go && go test ./internal/reducer/ -count=1        # ok
cd go && go test ./internal/storage/cypher/ -count=1 # ok (writer path untouched, re-run for the shared BUILT_FROM writer contract)
```

## rc-165 (B-7 golden corpus, BUILT_FROM) reasoning

`testdata/golden/e2e-20repo-snapshot.json` rc-165 asserts at least one
`ContainerImage-[:BUILT_FROM]->Repository` edge, isolated by
`evidence_kinds=[CONTAINER_IMAGE_IDENTITY_EXACT_DIGEST]`. Its note states the
child comes from the `cicdrun` supply-chain-demo cassette: a `ci.run` whose
`artifact_digest` exactly matches the `ociregistry` cassette's
`OciImageManifest` digest, carrying `repository_id: repository:r_69256c06`
(confirmed in `testdata/cassettes/cicdrun/supply-chain-demo.json:38-61`).

That repository id reaches the decision through
`addCICDArtifactImageReference`'s `run.repositoryID` append, which appends to
BOTH `anchors.sourceRepositoryIDs` and `anchors.buildProvenanceRepositoryIDs`,
and (as of #5808/PR #5809, now on `main`) that build-provenance value survives
the digest-only `addContainerImageDigestRef` path rc-165's `ci.artifact` fact
takes. A CI run reporting it produced an artifact digest is genuine build
evidence (the same tier #5460 uses for `DERIVED_FROM`'s child gate), so
`repository:r_69256c06` lands in `BuildProvenanceRepositoryIDs` exactly as it
already lands in `SourceRepositoryIDs`. `containerImageBuiltFromRows`
therefore still emits the one row rc-165 requires.
`TestContainerImageBuiltFromRowsFromCICDRunBuildProvenanceEndToEnd` above
proves this exact join shape at unit level, and now passes unmodified against
`main` post-#5808 (it failed with `BuildProvenanceRepositoryIDs = nil` against
main pre-#5808, which is how the gap above was caught before this PR shipped).

**Live-gate confirmation, run by the orchestrator directly on this PR's
pre-SLSA-fix head (`768a58863`):** `edge_count_BUILT_FROM` went **2 -> 1** --
the gate change removed exactly one fabricated edge and kept the genuine
CI-anchored one, staying inside the snapshot's `[1,10]` range. rc-167
`DERIVED_FROM` unaffected, residual=0, dead_letter=0, 495 pass / 0
required-fail. That 2->1 delta is direct proof the narrowing does what
issue #5796 describes: it stops attributing a build to a repository that
only referenced the image, while preserving the real CI-anchored `BUILT_FROM`
edge (rc-165's own assertion).

That run predates the SLSA-propagation fix above. Since SLSA build
provenance now also reaches `BuildProvenanceRepositoryIDs`, an image whose
only prior attribution was verified SLSA (if any exist in the golden corpus)
could now ALSO contribute a row it previously would not have under this PR's
gate -- **the live golden-corpus gate on this PR's current (post-SLSA-fix)
HEAD is still owed by the orchestrator** before merge, to confirm the edge
count is unaffected or, if it changes, that the change is toward more
accurate coverage rather than a new false positive.

## No-Regression Evidence

No-Regression Evidence: this narrows which decisions contribute a
`BUILT_FROM` row (fewer or equal rows per generation, never more) by changing
which field of an already-computed decision the row builder reads. No new
fact-kind reads, no new registry index scans, no new Cypher, and no change to
the writer (`ContainerImageProvenanceEdgeWriter`), retract-first ordering, or
batch size -- `BuildProvenanceRepositoryIDs` is already computed on every
decision by main's #5793 evidence-extraction pipeline regardless of this
change; this PR only changes which field one existing loop iterates.
`go test ./internal/reducer/ -count=1` (green) and
`go test ./internal/storage/cypher/ -count=1` (green) on the development
machine; wall time is not distinguishable from noise at this scale, and no
wall-time claim is made -- this is a correctness change, not a performance
change, so the burden here is the exact-delta regression proof above rather
than a benchmark. `BenchmarkContainerImageBuiltFromRows` (the pre-existing
B-9 cost-budget benchmark for this row builder) is unaffected in shape --
`benchContainerImageIdentityDecisions` was updated to populate
`BuildProvenanceRepositoryIDs` so it keeps exercising the same 5,000-row
workload the committed budget was measured against, not a smaller one; see
the benchmark output above.

## No-Observability-Change

No-Observability-Change: no metric, span, log, or status field is added,
removed, or renamed. `emitProvenanceEdgeCounter` still emits the same
`ProvenanceEdges` counter, labeled by the same `containerImageBuiltFromProvenanceEvidenceSource`
domain and `"materialized"` outcome, over whatever row count
`containerImageBuiltFromRows` returns -- a narrower row count changes the
counter's VALUE for corpora with reference-only repositories, exactly as
intended, but changes no instrument, label set, or telemetry contract.
