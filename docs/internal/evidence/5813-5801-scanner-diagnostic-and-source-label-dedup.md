# #5813 security-scan pipefail diagnostic + #5801 OCI source-label dedup

Two independent fixes landed together: a CI diagnostic bug (#5813) and an
accuracy bug in the container-image identity reducer (#5801). Neither depends
on the other; documented together per the PR that closes both.

## #5813 — `set -euo pipefail` suppressed the PIPESTATUS diagnostic

`.github/workflows/security-scan.yml`'s nancy step (~line 389) and govulncheck
step (~line 267) ran:

```bash
set -euo pipefail
<scanner> 2>&1 | tee out.txt
status=${PIPESTATUS[0]}
if [ "${status}" -ne 0 ]; then printf '...' >&2; exit "${status}"; fi
```

With `pipefail`, a non-zero exit from `<scanner>` terminates the script AT the
pipeline — bash's `errexit` fires before the `${PIPESTATUS[0]}` check ever
runs, so the framing `printf` diagnostic is unreachable. The raw scanner output
still reaches the CI log via `tee`'s stdout and the uploaded artifact, and both
steps are `continue-on-error: true`/non-blocking on this branch, so nothing
was hidden or unblocked by the bug — only the human-readable framing line was
lost, cosmetic until #5806 made nancy receive real dependency input (before
that, nancy always exited 0 on an empty stdin scan, so the non-zero branch was
unreachable in practice; see #5804).

Fix: guard the whole pipeline with `if ! ... | tee ...; then`, so `errexit`
never fires mid-pipeline and the `${PIPESTATUS[0]}` framing block always runs
on a non-zero scanner exit. Applied identically to both steps.

### Proof: diagnostic reachability

Reproduced the exact bug and fix shape in isolation (a throwaway script using
a `return 3` stand-in scanner and `tee`, mirroring both workflow steps
byte-for-byte apart from the scanner invocation):

```
--- OLD (broken) shape ---
exit code: 3
--- NEW (fixed) shape ---
DIAGNOSTIC PRINTED: status=3
exit code: 3
```

The OLD shape exits 3 with NO diagnostic printed (the bug); the NEW shape
prints `DIAGNOSTIC PRINTED: status=3` before exiting 3 (same exit code,
diagnostic now reachable). `bash -n` on both extracted snippets from the
actual workflow file confirms no syntax regression.

This is a CI workflow YAML change, not a Go file — it does not touch the
hot-path evidence gate's tracked surfaces.

## #5801 — OCI config source-label identity tier was dead in any corpus with duplicate repository facts

### Root cause

`matchOCIConfigSourceRepository`
(`go/internal/reducer/container_image_identity_provenance.go:103`, pre-fix)
counted raw repository **fact** matches and required exactly one:

```go
active, _ := matchPackageSourceRepositories(hint, repositories)
if len(active) != 1 {
    return packageSourceRepository{}, false
}
```

A repository legitimately carries several active `repository` facts (more
than one scope or collector observing the same repo), so a second fact for
the SAME repository made an unambiguous `org.opencontainers.image.source`
label look ambiguous and the label was discarded — the
`oci_config_source_label_with_digest` identity tier was covered by unit tests
(which construct exactly one repository fact) but unreachable in any real
corpus.

`matchOCIConfigSourceRepositoryByDistinctRepository` (added by #5460 for the
narrower base-image-lineage build-provenance reader) already implemented the
correct dedupe-by-`RepositoryID` rule, but was deliberately not applied to the
shared identity-tier matcher because widening it changes
`SourceRepositoryIDs` for every labelled image, with downstream blast radius
into supply-chain-impact repository anchoring (documented in the removed
function's own comment and in issue #5801).

### Fix

`matchOCIConfigSourceRepository` now dedupes by `RepositoryID` before applying
the exactly-one rule (the same logic `matchOCIConfigSourceRepositoryByDistinctRepository`
had). Since both matchers now do identical work, the duplicate function was
collapsed: `matchOCIConfigSourceRepositoryByDistinctRepository` is deleted and
`extractOCIConfigBuildProvenanceRefs` now calls `matchOCIConfigSourceRepository`
directly (`go/internal/reducer/container_image_identity_provenance.go`).

### Reconciliation: `singleSupplyChainImageSourceRepositoryID`

Activating the label tier repo-wide means `SourceRepositoryIDs` can now carry
a label-derived repository ALONGSIDE an unrelated, weaker scope/deploy
reference to a DIFFERENT repository for the same image (e.g. a Kubernetes
manifest in repo B merely referencing an image whose OCI config label names
repo A as the builder). Before this PR, `singleSupplyChainImageSourceRepositoryID`
(`go/internal/reducer/supply_chain_impact_match.go`) required
`len(SourceRepositoryIDs) == 1`, so this disagreement would blank out
`RepositoryID` — and the finding's entire workload/service/environment
context with it — even though the label unambiguously named the image's
build repository.

Decision: rank by evidence strength, not raw disagreement. The reducer
already persists `BuildProvenanceRepositoryIDs`
(`ContainerImageIdentityDecision.BuildProvenanceRepositoryIDs`) as the
strong-evidence-only subset of `SourceRepositoryIDs` — an OCI config source
label, a CI run, or verified SLSA provenance, never a mere deploy/scope
reference — but it was never persisted to the `reducer_container_image_identity`
fact payload, so the supply-chain-impact consumer could not see it.

- `container_image_identity_writer.go`: `containerImageIdentityPayload` now
  also writes `build_provenance_repository_ids`.
- `supply_chain_impact_index.go`: `supplyChainImageIdentity` gains a
  `buildProvenanceRepositoryIDs` field.
- `supply_chain_impact_match.go`: `supplyChainImageIdentityFromEnvelope`
  decodes the new field; `singleSupplyChainImageSourceRepositoryID` checks
  `buildProvenanceRepositoryIDs` FIRST and returns its sole entry when
  unambiguous, falling back to the broader `sourceRepositoryIDs` only when
  build evidence is absent or itself ambiguous. Two distinct repositories
  that are both genuine build evidence (or both distinct scope anchors) still
  resolve to neither — the writer already de-duplicates both fields, so a
  length check alone is sufficient at each tier.

**Why the existing CI-run/SLSA cross-decision merges are not disturbed:**
`applyCIRunDigestRevision` (the mechanism that lets a CI-run's repository
reach a DIFFERENT decision resolving the same digest, e.g. the golden
corpus's 11-rows-for-one-digest rc-165 scenario) only ever appends to
`decision.SourceRepositoryIDs`, never to `decision.BuildProvenanceRepositoryIDs`
— so a decision that gains an extra repository purely through that
cross-decision digest join stays exactly as ambiguous under the new ranking
as it was before. `applySLSADigestRevision` does merge into both fields by
design (#5808 comment: verified SLSA is documented as the strongest evidence
tier in this domain, stronger than an OCI label or a CI-run join), so the new
ranking is consistent with, not a departure from, the pre-existing evidence
hierarchy.

### Behavior-change proof (failing-then-green)

- `TestBuildContainerImageIdentityDecisionsSourceLabelSurvivesDuplicateRepositoryFacts`
  (`container_image_identity_provenance_test.go`) — BEFORE: `SourceRepositoryIDs
  = []string(nil)`, `IdentityStrength` not `oci_config_source_label_with_digest`
  (two active repository facts for the same repo made the label look
  ambiguous). AFTER: passes — the label resolves and
  `IdentityStrength = "oci_config_source_label_with_digest"`.
- `TestBuildContainerImageIdentityDecisionsSourceLabelStaysAmbiguousForTwoDistinctRepositories`
  — passed both before and after (already-correct negative case), confirming
  genuine ambiguity (two DISTINCT repositories claiming the same remote) still
  resolves to neither.
- `TestSupplyChainImpactHandlerPrefersLabelDerivedRepositoryOverConflictingScopeAnchor`
  (`supply_chain_impact_repository_anchor_label_test.go`) — BEFORE:
  `RepositoryID = ""` (the label-derived repository and an unrelated
  scope-anchor repository made `SourceRepositoryIDs` len 2, blanking the
  anchor and losing workload/service/environment reachability). AFTER:
  `RepositoryID` resolves to the label-derived repository and its workload
  context is retained.
- Pre-existing regression guards re-verified green, unchanged behavior:
  `TestBuildProvenanceSurvivesDuplicateRepositoryFacts`,
  `TestTwoDifferentRepositoriesClaimingOneRemoteStayAmbiguous`,
  `TestSupplyChainImpactIndexContainerImageIdentityDeterministicAcrossEnvelopeOrder`,
  `TestSupplyChainImpactHandlerResolvesRepositoryFromContainerImageIdentityDigest`,
  `TestSupplyChainImpactHandlerLeavesRepositoryBlankWhenImageIdentitySourceRepositoriesAmbiguous`,
  `TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity`.

Full command: `cd go && go test ./internal/reducer/ ./internal/mcp/
./internal/storage/cypher/ -count=1` — all green
(`reducer 3.5s`, `mcp 1.9s`, `storage/cypher 1.8s`).

## No-Regression Evidence

No-Regression Evidence: the #5801 change is in-process reducer classification
and index-building over already-loaded facts (a map-based dedupe in
`matchOCIConfigSourceRepository`, one additional persisted string-slice field,
one additional decode read, and a two-tier length check replacing a one-tier
length check in `singleSupplyChainImageSourceRepositoryID`). No new query,
graph read, batch, lease, or worker path; issues no Cypher and no Postgres
query. `go test ./internal/reducer/ -count=1` stayed in the same ~3-3.5s band
observed for this package before this change (see #5808's own evidence note
for the same package's baseline). This PR's live-gate proof (B-7
golden-corpus, `mcp:list_supply_chain_impact_findings`, rc-165, rc-167) is
owed by the orchestrator per this repo's shared-host live-gate singleton
policy — not run from this worktree. Unit-level failing-then-green proof
above is what this local evidence can certify; the orchestrator's live run is
the authoritative accuracy proof for the corpus-scale reconciliation
(label-vs-scope-anchor ranking) described above.

## No-Observability-Change

No-Observability-Change: no metric, span, log, or status field is added or
changed. The `container_image_identity` domain remains covered by
`eshu_dp_container_image_identity_decisions_total`,
`eshu_dp_reducer_executions_total`, and `eshu_dp_reducer_run_duration_seconds`;
the `supply_chain_impact` domain's existing counters/summaries are unchanged.
This fix only widens which of two already-computed, already-persisted fields
(`SourceRepositoryIDs` disambiguation) the existing repository-anchor join
prefers, and persists one additional field
(`build_provenance_repository_ids`) that was already computed in-memory but
previously discarded before durable write.
