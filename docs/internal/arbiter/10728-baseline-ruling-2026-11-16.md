# Arbiter ruling: #10728 NVD baseline (2026-11-16)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Subject: issue #10728, "NVD baseline: full-corpus seed from
the official year feeds", criterion 1 (whether Eshu should hold the whole NVD
corpus, with the reducer consequence stated). Parent ruling:
[10671-cvelist-mirror-ruling-2026-11-15.md](10671-cvelist-mirror-ruling-2026-11-15.md).
Design: [10671-nvd-feed-transport-fallback.md](../design/10671-nvd-feed-transport-fallback.md).
Measurements:
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md).
Environment identifiers are redacted per #7803.

## Ruling

**Yes: hold the whole NVD corpus as a one-time baseline, bounded and
proof-gated.** The amendment-1 year-feed window seed of the #10671 ruling is
necessary for the 28-day outage gap and by construction insufficient for
#10728: covered windows serve only year-set records with `lastModified` in
`[start, end)` overlaid by Modified (design:79-81), so records last modified
before the first window emit no facts from the seed.

## Evidence (all first-hand)

- gh issue view 10728 (2026-11-16): 74 window scopes span 2026-10-09 to
  2026-10-18 (~8.6d), 10,123 `vulnerability.cve` facts for 10,073 distinct CVE
  ids, cursor 2026-10-17T20:18:09Z. ~238 MiB gz / 2.8 GiB decompressed / 25
  year files.
- 10671 ruling:192-196 amendment 1: year-feed seed required before QA enable
  because Modified spans ~9d vs ~28d lag; it advances the window cursor, it
  does not import the corpus.
- 10671 ruling:156 live API probe `totalResults` 425,770 vs 10,073 held
  distinct ids = 2.37% held, ~415,697 missing (derived arithmetic, first-hand
  inputs only).
- Reducer consequence / CPE uniqueness:
  `advisorySourceClass` in
  `go/internal/reducer/supplychain/core/derived_partition.go`
  (`affected_product` kind is NVD-only, hardcoded to nvd);
  `supplyChainCVEGroup.representative` in
  `go/internal/reducer/supplychain/core/provenance.go` (NVD is the only
  source emitting `affected_product` facts, the product-only CPE path);
  10671 ruling:42 (OSV/GHSA: severity only).
- Feed fidelity: design:161-164 + measurements M2 (45-52): feeds carry
  cpeMatch 34,675/34,675 with matchCriteriaId, all 7 vulnStatus states, all
  CVSS families; M8:131-132 20/20 deep-equal to the API. A baseline from year
  feeds is identity translation, contract class none.
- Scale (upper-bound extrapolation from M5, not measurement):
  measurements M3:56-58 (25 year feeds, 237.6 MiB gz, 2,809 MiB
  decompressed) and M5:75-79 (mean 7.07 facts/record over 12,818 records)
  give 425,770 x 7.07 ~= 3.01M facts, ~301 claims at the 10,000-fact cap
  (design:176), ~150h at one chunk per 30min planner run.
- Dirty-log/reducer risk is real but unmeasured, hence gated not assumed:
  M6:94-101 (1,080,966 dirty rows, 1.0-1.8k/min OSV/GHSA-keyed inflow from
  reducer finding upserts, not raw facts); design:201-210 (rows per NVD fact
  unmeasured).

## Decisions

1. Whole-corpus baseline: YES. Cost of not having it: ~97.6% of NVD has no
   NVD cve/affected_product facts until NVD happens to modify each record,
   so the product-only CPE path and NVD vulnStatus/Primary-CVSS/reference
   identity are silent false negatives for every pre-2026-10-09 untouched
   CVE: an accuracy failure, not a severity-enrichment gap.
2. Bounded form: reuse the collector#100 year-set seed and manifest already
   on the host volume (streamed, never held whole). No re-download, no new
   source, contract, scope, or cursor. Emit through the same `nvd_envelope`
   and stable keys with the same commit/fencing path, in per-claim chunks
   <= 10,000 facts / 2 MiB, deterministic year order, with a resumable
   baseline checkpoint distinct from `nvd/_window` so baseline claims never
   arm or advance the incremental cursor CAS. Pace no faster than one chunk
   per planner run until disposable-DB proof justifies otherwise.
3. #10706 gate: governed, not hard-blocked, per 10671 amendment 5. The
   baseline QA run obeys the same trip line (kill switch off on > 15k
   NVD-proxy rows/h in 2 consecutive hourly samples or oldest dirty > 96h,
   re-enable < 48h, 6 hourly samples; #10706 p95 <= 15s / watermark <= 60s
   stay soak bars). No pausing or serializing OSV/GHSA.
4. Idempotent re-run by stable keys (R6-style convergence test required).
   Records modified after seed generation converge via later windows.

## Required proof (implement/QA gates)

Disposable-DB run first: facts/record under satellite folding, dirty-log
rows per committed fact, wall clock and RSS including the R7b year-set
scan; R6-style idempotency (two baseline passes = one row set); the
"how much of NVD held" count query inside a 15s statement timeout, recorded
in evidence. Only then QA enable under the trip line.

## Will not do

No CVE List or third-party whole-corpus substitute (0/13,405 matchCriteriaId:
cannot supply CPE identity); no unbounded one-shot QA dump; no cursor
hand-edit or baseline-driven cursor advance; no new contract field, source
value, or scan-gate changes; no serialization of OSV/GHSA to make room.

## Not verified (does not change the verdict)

Rows per NVD fact, folded facts per record, and NVD facts outside the 74
scopes: each must be measured by the required proof before any QA baseline
run. If disposable-DB proof shows rows/fact fan-out breaching the trip line
at minimum pacing, the bounded form slows or splits further; it does not
revert to seed-only, which is proven insufficient by construction.

Refs #10728. Part of #9209.
