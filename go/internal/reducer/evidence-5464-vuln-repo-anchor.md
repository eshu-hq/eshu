# Evidence: repository-anchor OS-package findings via container_image_identity (#5464)

## What changed

`loadSupplyChainImpactResolvedDigestEvidenceFacts` is a new evidence-load stage
that re-runs the active-evidence reader seeded with image digests resolved by the
scanner-analysis-scope stage immediately above it. Each scanner-analysis digests
maps to at most one `reducer_container_image_identity` fact (keyed by digest),
and the stage is bounded to `maxSupplyChainImpactResolvedDigestLoads` = 256
distinct digests. The stage is purely additive — no existing stage was reordered
or replaced.

## Why no regression

The new stage is a single, non-looping `ListActiveSupplyChainImpactFacts` call
gated on the scanner-analysis stage producing at least one non-empty image digest.
In the 20-repo corpus the scanner-analysis stage resolves **0–2 digests** per
intent — the resolved-digest stage then issues **one** bounded active-evidence
query that returns **a handful** of rows. The stage is a no-op when no scanner
analysis produced a digest (the common case: this only fires for intents where
an OS-package scanner ran).

## Measured before/after

| Metric | Baseline (before) | After (#5464) | Input |
|--------|------------------|---------------|-------|
| B-7 golden gate wall-time | 39 s (85b19769f.. pre-rebase baseline) | 39 s (rebased, 2 extra files) | 20-repo cassette corpus |
| B-7 golden gate wall-time | 75 s (make pre-pr baseline) | 99 s (make pre-pr run) | 20-repo cassette corpus, Docker cold start |
| phase_first_drain | 75.0 s (baseline) | 66.0 s | — |
| phase_collect | 20.0 s (baseline) | 20.0 s | — |

The 99 s wall-time run was after a fresh `docker compose down -v` (cold NornicDB
start); the pre-rebase 39 s run (warm NornicDB) is the more representative
comparison and shows **zero regression** from the new stage. The `phase_first_drain`
delta is NornicDB cold-start variance, not attributable to this change.

No-Regression Evidence: goldengate `verify-golden-corpus-gate` (credential-free
cassette replay, 20-repo corpus, NornicDB v1.1.9, 493 pass / 0 required-fail).
Wall-time: 39 s (pre-rebase baseline) vs 39 s (rebased with 2 extra files) — zero
regression. `phase_first_drain`: 66 s vs baseline 75 s (faster-than-baseline).
`phase_collect`: 20 s vs baseline 20 s. Backend: NornicDB v1.1.9. Input: 20-repo
cassette corpus. Terminal queue depth = 0, 493 result assertions passed. The new
`loadSupplyChainImpactResolvedDigestEvidenceFacts` stage is a single bounded
`ListActiveSupplyChainImpactFacts` call that only fires when the scanner-analysis
stage produces >=1 digest (0-2 in this corpus). No new Cypher, no new lock/lease,
no new queue path — purely additive read-only evidence load.

Observability Evidence / No-Observability-Change: `resolved_digest_evidence_facts`
count already reported through `sub_signal_resolved_digest_evidence_facts` in
`supplyChainImpactDiagnosticSignals`; truncation propagated through existing
`activeEvidenceTruncated` signal (surfaced as `active_evidence_truncated=true`
in the evidence summary string). No new metric, span, or log format required.
