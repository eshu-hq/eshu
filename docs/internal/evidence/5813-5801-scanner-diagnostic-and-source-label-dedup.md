# #5813 security-scan pipefail diagnostic + #5801 OCI source-label dedup

Two independent fixes landed together: a CI diagnostic bug (#5813) and an
accuracy bug in the container-image identity reducer (#5801). Neither depends
on the other; documented together per the PR that closes both.

## #5813 — a diagnostic-only bug, then a real false-green introduced and fixed

This landed as three distinct shapes of the same handful of lines, in
sequence. Conflating any two of them is the exact mistake this section exists
to prevent from being repeated (see "Refuted-then-refined" below) — each row
was independently reproduced with the same throwaway harness (bash 3.2.57,
which is also the default `/bin/bash` on the `ubuntu-latest` GitHub Actions
runner image):

| Shape | Where | scanner 0 + tee fails | scanner N + tee ok |
| --- | --- | --- | --- |
| A — pre-PR `main` (bare pipeline, no `if !`) | `main` before this PR | exit 1, diagnostic **not** printed (`errexit` aborts AT the pipeline) | exit N, diagnostic not printed |
| B — PR commit `b93e2a078` (`if !` wrapper, reads only `PIPESTATUS[0]`) | mid-PR | exit **0** — **FALSE GREEN**, prints the misleading "vulnerabilities found" line while the scanner actually succeeded | exit N, diagnostic printed |
| C — commit `41e77ee33` (captures the full `PIPESTATUS` array) | this PR, final | non-zero, correct tee-failure diagnostic | exit N, diagnostic printed |

### Shape A (pre-PR `main`): diagnostic-only — the original #5813 framing was right, for this shape

`.github/workflows/security-scan.yml`'s nancy and govulncheck steps ran a
**bare** pipeline directly under `set -euo pipefail`:

```bash
set -euo pipefail
<scanner> 2>&1 | tee out.txt
status=${PIPESTATUS[0]}
if [ "${status}" -ne 0 ]; then printf '...' >&2; exit "${status}"; fi
```

A bare pipeline (not the condition of `if`/`while`/`until`, not negated with
`!`) IS subject to `errexit`: a non-zero pipeline result terminates the script
right there, before `${PIPESTATUS[0]}` is ever read. So whether the scanner
failed or only `tee` failed, the script aborted with the pipeline's own exit
status and the framing `printf` never ran — a lost diagnostic, not a false
green. Reproduced directly:

```
$ bash --version | head -1
GNU bash, version 3.2.57(1)-release (arm64-apple-darwin25)
$ bash shape-a.sh   # scanner exits 0, tee target directory does not exist
tee: /nonexistent-dir-xyz/govulncheck.out: No such file or directory
no vulns
shape A exit code: 1
```

No "REACHED-END-OF-SCRIPT" and no framing diagnostic printed; exit 1 (tee's
own status via `pipefail`), not 0. This is genuinely cosmetic: nothing was
hidden or unblocked, only the human-readable framing line was lost — and (for
nancy specifically) unreachable in practice anyway until #5806 made nancy
receive real dependency input; before that, nancy always exited 0 on an empty
stdin scan (#5804).

### Shape B (PR commit `b93e2a078`): the `if !` rewrite introduced a genuine false green

To make the framing diagnostic reachable, the mid-PR fix wrapped the pipeline
in a negated `if !`, which exempts it from `errexit`:

```bash
set -euo pipefail
if ! <scanner> 2>&1 | tee out.txt; then
  status=${PIPESTATUS[0]}
  printf '...' >&2
  exit "${status}"
fi
```

This does make `${PIPESTATUS[0]}` reachable — but it also means the script no
longer aborts at the pipeline on ANY pipeline failure, including one where the
scanner itself succeeded and only `tee` failed. Since only `PIPESTATUS[0]`
(the scanner's own status, `0`) was read, that case now falls all the way
through the `if` body and exits **0** — a true false green, and worse than
silent: it prints the misleading "vulnerabilities found" framing line while
reporting success. Reproduced directly, exact shape:

```
$ bash shape-b.sh   # scanner exits 0, tee target directory does not exist
tee: /nonexistent-dir-xyz/govulncheck.out: No such file or directory
no vulns
govulncheck: vulnerabilities found, see govulncheck.out above
shape B exit code: 0
```

A codex review on PR #5817 flagged exactly this at the `if !` line as a P1
false-green finding. That review was correct against the code as the PR stood
at `b93e2a078` — the `if !` rewrite, made to fix shape A's cosmetic diagnostic
loss, is what actually introduced the false green.

### Shape C (commit `41e77ee33`): fixed by capturing the full `PIPESTATUS` array

```bash
if ! <scanner> 2>&1 | tee out.txt; then
  statuses=("${PIPESTATUS[@]}")
  scanner_status=${statuses[0]}
  tee_status=${statuses[1]}
  if [ "${scanner_status}" -ne 0 ]; then
    printf '<scanner-specific finding message>\n' >&2
    exit "${scanner_status}"
  fi
  printf '<scanner>: failed to write out.txt (tee exited %s)\n' "${tee_status}" >&2
  exit "${tee_status}"
fi
```

`PIPESTATUS` is clobbered by the very next command, even a bare assignment, so
both statuses must be captured together in the statement immediately after the
pipeline. Reading `tee_status` and exiting with it when the scanner itself
succeeded closes the false green: a scanner success with an unwritten artifact
now reports failure, distinctly diagnosed from a real finding.

### The fix is now delegated to a shared, independently tested helper

Both `run:` blocks now call `scripts/ci/run-scan-with-tee.sh` (#5813) instead
of inlining this logic in the workflow YAML, and
`scripts/test-security-scan-tee-status.sh` is its executable regression suite
— following the same shape as `scripts/dev/nancy-local.sh` /
`scripts/test-nancy-local.sh`: a source-text-only assertion over the YAML
"could not tell a working pipeline from a broken one" (rejected in review on
PR #5806), so the logic lives in a script with its own tests instead. The
suite runs the REAL helper with stub scanners on PATH and asserts exit codes
and artifact contents for all three cases; the tee failure is induced via
ENOTDIR (a regular file used as a directory component in the artifact path),
not `chmod 000`, because a chmod-based failure is ignored when CI runs as
root, while ENOTDIR fails under any uid:

```
$ bash scripts/test-security-scan-tee-status.sh
PASS: scanner-fails
PASS: tee-fails-enotdir
PASS: clean-scan
PASS: run-scan-with-tee.sh distinguishes scanner failure, tee failure (ENOTDIR), and a clean scan
```

Failing-then-green proof that the suite actually pins the shape-B regression
(the helper's status logic was temporarily reverted to the old
`status=${PIPESTATUS[0]}; exit "${status}"` shape, the suite was rerun, then
the fix was restored):

```
$ bash scripts/test-security-scan-tee-status.sh   # helper reverted to shape B's logic
PASS: scanner-fails
test-security-scan-tee-status: tee-fails-enotdir: exit=0, want non-zero (this is the #5813 false-green regression). output:
tee: .../tee-fails-enotdir/not-a-directory/scan.out: Not a directory
clean scan, nothing found
fakescan: scanner failed with status 0, see scan.out above
exit code: 1   # (the TEST's own exit code — it correctly FAILED against the reverted helper)

$ bash scripts/test-security-scan-tee-status.sh   # fix restored
PASS: scanner-fails
PASS: tee-fails-enotdir
PASS: clean-scan
PASS: run-scan-with-tee.sh distinguishes scanner failure, tee failure (ENOTDIR), and a clean scan
```

This is the regression class that had no verifier before this change: neither
`scripts/dev/precommit-go.sh`'s govulncheck case nor `scripts/dev/nancy-local.sh`
uses `tee` (both use direct redirection), so neither could have caught a
shape-B-style false green. `scripts/test-security-scan-tee-status.sh` now
covers all three cases directly against the real helper both jobs call.

### Refuted-then-refined: the mistake to not repeat

A separate Claude session investigating this PR reproduced shape A (the bare
pipeline) and, from that, concluded the false-green claim was categorically
wrong — staging edits to this file and to the workflow comments asserting
"that claim is wrong" and that the fix was "diagnostic-only." The shape-A
reproduction itself was accurate; the error was applying its conclusion to
shape B, which is not the same code. `b93e2a078`'s `if !` wrapper genuinely
changes `errexit` behavior for that pipeline, and that is precisely what
converts a diagnostic-only bug into a false green. The session's staged
changes were discarded (never committed) once this was caught. The lesson:
when adjudicating a review comment against a specific commit, reproduce the
EXACT shape at that commit — a bare pipeline and an `if !`-wrapped pipeline
are not interchangeable for `errexit` purposes, and testing the wrong one
proves nothing about the other.

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
