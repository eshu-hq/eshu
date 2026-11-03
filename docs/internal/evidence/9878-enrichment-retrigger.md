# CVE-keyed re-trigger convergence for KEV/EPSS refresh (#9878)

**Decision 1 of `docs/internal/design/9878-enrichment-refresh-decision.md`
already had its mechanism in the tree: the #9695 holder fan-out is the
CVE-to-owning-scope edge. This change proves the enrichment path end to end,
adds the fan-out `cause` label, and documents the freshness bound. No new
edge, no winner-sort change, no foreign-scope writes.**

## Verdict on the edge question

The decision record asked whether a CVE-to-owning-scope edge exists or must
be added (with readiness-floor treatment if added). It exists:

- EPSS and KEV facts are projector trigger kinds
  (`go/internal/projector/supply/chain/impact/intents.go`), so a changed
  enrichment generation enqueues `supply_chain_impact` for its own scope.
- The enrichment scope's pass re-derives its copies; an enrichment flip
  changes the finding content digest (`writer_changed.go`), so the writer
  reports the CVE in `ChangedCVEIDs`.
- `FindHolderScopes` (active finding rows by `cve_id`, joined to each
  scope's active generation) is the CVE-to-owning-scope mapping, used only
  to reopen holder work items (`fanout.go`, `SupplyChainImpactFanOutStore`).
  Each holder re-derives only its own copy.
- `crossscope.dependencyCatalog` is not this edge: it declares
  reducer-domain completion dependencies (container image identity, CI/CD
  correlation). Enrichment is collector facts, not a reducer domain, so no
  catalog entry or readiness-floor change applies. Plan section 4.2's
  catalog edge is for advisory-group re-keying (Phase 3), not for this item.
- The digest skip was already wired and proven
  (`freshness_skip_9878_test.go`): unchanged KEV/EPSS input creates no
  generation, hence no re-trigger wave.

Root-Cause Evidence: the masking state in the live regression below — after
the lowered EPSS generation commits and before any holder re-runs, the
winner still serves EPSS 0.5; after the drain, every copy and the winner
serve 0.05 with no manual rerun of the holder scope. The stale winner was a
vintage problem (copies scored from different enrichment generations), and
convergence, not a sort change, is what removes it.

## What changed

- `eshu_dp_supply_chain_impact_fanout_total` gains a closed `cause` label
  (`epss`, `kev`, `advisory`, `suppression`, `package_evidence`, `mixed`,
  `other`): the evidence family of the committing pass's own generation,
  classified from its scope-fact kinds (`fanOutCause`). Enrichment
  generations are single-source by construction (one collector target per
  scope generation), so `epss` and `kev` name an enrichment re-trigger
  exactly. The same value rides the `supply_chain_impact.fanout` span and
  the fan-out log line.
- Acceptance regressions (details below) and the freshness contract in
  `HOLDER_FANOUT.md` and the winners telemetry page: the stale-copy window
  is bounded by queue latency plus one holder pass per holder scope plus
  one 30-second resweep tick.

Observability Evidence: `eshu_dp_supply_chain_impact_fanout_total{cause}`
(counts re-trigger enqueues/reopens by CVE-change cause); digest-skip count
is the existing ingestion skipped-refresh counter (reason `unchanged`,
`telemetry.RecordSkippedRefresh`, log key `refresh_skip_reason`);
convergence lag is bounded by
`eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds` and
`eshu_dp_supply_chain_impact_winners_watermark_age_seconds` (#9805), with
fan-out `deferred`/`replay_flagged` outcomes showing an in-progress drain.

## Proof

Live regressions over the #9695 convergence harness (real handler, writer,
fan-out store, queue; disposable PostgreSQL 18.6), file
`go/internal/storage/postgres/supply_chain_impact_fanout_enrichment_9878_live_test.go`:

| Test | Proves | Result |
| --- | --- | --- |
| `TestEPSSLoweredConvergesAllCopiesAndWinner9878Live` | Two scopes; EPSS 0.5 → 0.05 with newer `score_date` lands only in the EPSS scope. Winner is the older higher copy (0.5, priority 90) before the drain; both copies and the winner carry 0.05 (priority 64) after, with no manual holder rerun. | PASS (1.85s) |
| `TestNewKEVListingReachesPreExistingFindings9878Live` | A first KEV listing in a new KEV scope reaches pre-existing copies in both scopes; winner gains `known_exploited` and `cisa_kev`. | PASS (1.90s) |
| `TestEnrichmentCommittedDuringHolderPassStillConverges9878Live` | Interleave: a holder pass parked after its evidence load commits its stale vintage while the EPSS pass runs; the replay flag re-runs it and the copies converge. No lost generation, no serialized scope runs (the passes overlap). | PASS (1.97s) |
| `TestIdenticalEPSSRescanFansOutNothing9878Live` | Duplicate delivery: an identical EPSS generation re-derives identical digests, so no holder is re-triggered. | PASS (1.92s) |

Core unit pins (`enrichment_retrigger_9878_test.go`): the writer reports the
CVE changed for lowered EPSS (newer date), new KEV, and date-only EPSS
changes against a prior-generation baseline, and nothing for an identical
re-derivation; the cause classifier table; a full `Handle` proving
`cause="epss"` on the reopened datapoint. The four live tests and the
digest pins passed on the pre-change tree as well: they are the proof the
mechanism pre-existed, kept as regression coverage. The cause tests failed
to build before the change (undefined `fanOutCause`), which is the RED half
for the new label.

Concurrency (per `concurrency-deadlock-rigor`): conflict domains are the
per-(scope, generation) write lock, the holder work-item rows, and the
fencing token order — all pre-existing. This change adds no lock, claim, or
queue behavior. The interleave test covers the enrichment-specific overlap;
duplicate delivery is idempotent by digest; the generic race matrix stays
in `supply_chain_impact_fanout_races_live_test.go` and
`supply_chain_impact_fanout_inflight_live_test.go`.

## Scale

Per enrichment change, the write cost is: one committing pass in the
enrichment scope (its ordinary full re-derivation), one indexed holder read
per 200 changed CVEs (measured at #9695: about 5 ms for one CVE, about
130 ms for a 200-CVE chunk), one single-row reopen per holder (at most 256
per claim, remainder drained by continuation), and one ordinary pass per
holder scope. Copies per finding are bounded by matching enrichment scopes
(about 5 EPSS target scopes at 447 QA finding CVEs), so per-change cost
grows with changed CVEs times holders-per-CVE, not with total findings. A
KEV catalog refresh that changes nothing is skipped before a generation
exists; one that lists k new CVEs flips only those k digests in the KEV
scope, so the wave is k CVEs wide, not catalog-wide.

| Scale | Resweep shape | Source |
| --- | --- | --- |
| 1x (100,000 findings, 83,333 winners, 1,000 dirty identifiers) | keyed tick 199.8 ms before / 247.5 ms after (one sample each, host load 37–58); full resweep over the same state 4.459 s / 4.456 s | this note's runs |
| 5x / 10x (645 / 3,225 / 6,450 winner keys in the rejected-option shim; keyed cost follows changed keys, not table size) | resweep p95 at most 224 ms at 10x in the decision proof | decision record, #9805 scale note |

No-Regression Evidence: same-state 1x scale proof before (`582f8ad32d`) and
after (this branch), largest corpus run locally: full resweep 4.459 s →
4.456 s (identical), keyed tick 199.8 ms → 247.5 ms (single samples on a
shared host at load 37–58; the diff touches no resweep or read SQL, so the
delta is host noise, and list/aggregate p95 is unchanged by construction —
no query text changed). Microbenchmarks (`benchtime=2s, count=3`, arm64):
allocations identical before/after (`BenchmarkKEVEPSSRescoreBuild9878`
3,437 / 19,270 / 36,886 allocs/op at 1x/5x/10x both trees;
`BenchmarkWritePayloadWithDigest` 196,041 both trees); ns/op ranges overlap
across trees under host noise. The reducer binary builds on both trees
(103,877,752 → 103,945,704 bytes). NOT_CHECKED: a quiet-host p95 series for
keyed tick and list/aggregate (CI's read-api latency gate owns the
list/aggregate budget); 5x/10x local corpus loads.

## Residuals kept, by design

- A delisting converges through the retraction side of the changed set; a
  partial-evidence pass proves no removal and does not propagate it.
- A date-only EPSS change re-triggers holders once (the selected date is in
  the digest) and lands on identical scores.
- A scope that acknowledged before the in-flight read with no finding row
  for the CVE is not a holder (documented in `HOLDER_FANOUT.md`).
- Final #9878 acceptance still needs the fixture re-run after #9693 (1.2
  severity) lands and the seeded-CVE QA check; both are outside this diff.

## Commands

```text
go test ./internal/storage/postgres/ -run '9878Live' -count=1 -v  (exit 0; DSNs per supplyChainImpactLiveDSN)
go test ./internal/reducer/supplychain/core/ -run '9878' -count=1 -v  (exit 0)
go test ./internal/reducer/supplychain/core/ ./internal/telemetry/ -count=1  (exit 0)
go test ./internal/reducer/ -run 'FanOut|SupplyChainImpactRegistration' -count=1  (exit 0)
go test ./cmd/reducer/ -run 'FanOut|Winners' -count=1  (exit 0)
go test ./internal/storage/postgres/ -run 'TestSupplyChainImpactWinnersResweepScaleProofLive' -count=1  (exit 0, both trees, ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_SCALE_PROOF=1x)
```

Environment: GOTOOLCHAIN=go1.26.10 (go/go.mod), GOROOT unset, PostgreSQL
18.6 local disposable databases, shared host (load average 37–58 during
timing runs; timing figures carry that noise).
