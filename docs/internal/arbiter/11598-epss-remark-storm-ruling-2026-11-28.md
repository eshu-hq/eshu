# Arbiter ruling: #11598 EPSS re-mark storm & sequencing (2026-11-28)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Base `origin/main@60be6b1eb7`.

**Ruling: Option C** — combine reducer 0.01-floor `epss_percentile`
quantization, `#10880` Decision 4 amendment in `dirtyIdentifiersToCapture`,
`AuditWinners` provenance masking (without bumping winners contract v3), and
`first_epss` collector `StableID`/`FreshnessHint` cleanup; execute `#9432` and
`#11097` immediately after `#11626` (merged off) and `#11598`, before enabling
`#11626` paced fill and evaluating `#11630`.

## Verified facts

1. Even after `#11599` excluded `provenance.selected_epss_score_date` from
   `contentDigestOf`, live QA `supply_chain_impact_winners_dirty_log` held
   59,285 `fact_upsert` keys at `avg(dirty_version) = 53.0` (`max = 259`,
   3.14M cumulative marks) and 89,610 rows in `dirty_staging`.
2. Direct `fact_records` inspection showed daily FIRST EPSS refreshes keeping
   `probability` identical (`0.004180000`) while `percentile` drifted by
   `0.00005` (`0.277810000 -> 0.277860000`), and `first_epss` hashing
   `score_date` into `StableID`, minting a new `fact_id` daily that entered
   `evidence_fact_ids` and tripped `dirtyIdentifiersToCapture`.
3. `14,033,890` support rows back `82,299` canonical winners (`170.5x`) across
   `9,966` legacy `osv/_batch` + `3,306` `osv/_slot` scopes; individual
   advisories hold `33,532` support rows across `82` scopes for `246` keys.

## Decisions

1. **Reducer `EPSSPercentile` 0.01 floor (`product.go`):** quantize
   `signals.EPSSPercentile` via exact rational floor to two decimals inside
   `applyRiskSignals` (after raw `preferEPSSObservation` tie-breaking, before
   `withSupplyChainImpactPriority` and payload encoding). Preserves `0.80` /
   `0.95` priority thresholds and `PriorityContributions` stability while
   keeping raw 9-decimal values on `vulnerability.epss_score` source facts.
2. **Amend `#10880` Decision 4 (`winners_keyed.go`):** remove
   `memberSetsEqual(row.evidenceFactIDs, base.evidenceFactIDs)` from
   `dirtyIdentifiersToCapture`. Keep `evidence_fact_ids` served on
   `ListFindings`, keep `supports.payload` fresh for `Explain`, and leave
   `supplyChainImpactWinnerBodySQL` and `SupplyChainImpactWinnersContractVersion`
   at `3` (no 14M-support full resweep forced at deploy).
3. **Align `AuditWinners` (`supply_chain_impact_winners_audit.go`):** mask
   `((payload - 'evidence_fact_ids') #- '{provenance,selected_epss_score_date}')`
   on both `n` and `w` in `winnersAuditDiffSelect` (shared by legacy, stale,
   verification, and staged range audits), keeping all semantic columns
   compared verbatim and steady-state `Repairs = 0`.
4. **Collector companion (`eshu-collector-vulnerability-intelligence`):** drop
   `score_date` from `first_epss` `StableID` and compute `FreshnessHint` without
   `score_date` and with `0.01`-floored `percentile`.
5. **Sequencing:** `#11626` (merged off) -> `#11598` -> `#9432` + `#11097` ->
   re-measure & enable `#11626` paced fill -> evaluate `#11630`.
