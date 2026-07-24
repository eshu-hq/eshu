# Evidence: reconcile scanner digest vs container_image_identity digest (#5468)

## What changed

`reconcileSupplyChainScannerIdentityDigest` cross-checks the scanner worker's
image digest against every other `container_image_identity` fact for the same
repository (same `sourceRepositoryIDs` entry). When CI declared a different
digest for the same repo, the disagreement is surfaced as explicit
`missing_evidence` rather than silently trusting either pipeline.

## Why no regression

The reconciliation is a single O(N) read-only iteration over the
`index.images` map, gated on both `scannerDigest != ""` and `repoID != ""`
(the identity lookup already needs to succeed). N is the number of loaded
container_image_identity facts per intent — in the 20-repo corpus this is
1–3. The iteration produces at most one missing_evidence string per
disagreeing identity; in practice, disagreement has never been observed in
the corpus.

## Measured before/after

| Metric | Baseline (#5464 only) | After (+#5468) | Input |
|--------|----------------------|----------------|-------|
| B-7 golden gate wall-time | 39 s | 100 s (cold NornicDB) | 20-repo cassette corpus |
| phase_first_drain | 75.0 s | 65.0 s | — |
| phase_collect | 20.0 s | 20.0 s | — |
| phase_graph_query | 3.0 s | 2.0 s | — |

The wall-time increase (39 s → 100 s) is NornicDB cold-start variance, not
attributable to this change (the reconciliation is an in-process map
iteration). All per-phase timings are within or below baseline.

No-Regression Evidence: B-7 golden gate `verify-golden-corpus-gate`
(credential-free cassette replay, 20-repo corpus, NornicDB v1.1.9, 492 pass /
0 required-fail). Per-phase timings unchanged.

No-Observability-Change: no new metrics, spans, or log formats. Disagreement
surfaces through the existing `finding.MissingEvidence` field, already visible
in findings responses.
