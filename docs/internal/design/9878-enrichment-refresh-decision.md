# Keep per-scope enrichment copies with CVE-keyed re-trigger convergence (#9878)

Status: Accepted (arbiter-eshu verdict, 2026-11-03). This record approves no
schema change, no winner-sort change, and no deployment.

**Keep the per-scope finding copies and the materialized winners table, and
fix KEV/EPSS masking by convergence: when a KEV or EPSS generation changes a
CVE's enrichment, every scope holding a finding for that CVE re-runs
`supply_chain_impact`, so all copies under a `canonical_key` score from the
same enrichment vintage. Full read-time enrichment is rejected on measured
grounds.**

## Context

Item 2.3 asks how KEV and EPSS changes reach existing findings. Each
enrichment generation runs `supply_chain_impact` for its own scope and writes
its own copy of every finding it matches; the winners table keeps one copy
per `canonical_key` by score order. Three code-reading hypotheses needed proof.

Measured facts and their sources:

- Today's-behavior fixture (`6ae57213a4` on
  `test/9878-kev-epss-rescore-fixture`,
  `go/internal/reducer/supplychain/core/kev_epss_rescore_9878_test.go`, green
  on re-run by the lead): same CVE under two scopes yields two `fact_id`s
  under one `canonical_key`; same-scope rerun keeps the `fact_id`. Winner
  order is `priority_score DESC, has_payload_finding_id DESC, fact_id ASC`
  with no recency tiebreak.
- Build-level EPSS selection is date-first (`preferEPSSObservation`,
  `go/internal/reducer/supplychain/core/index_build.go:261`): a fresh lowered
  score reaching the finding is correct; a stale lowered score being masked at
  build is also correct. The defect is only at winner level, across per-scope
  copies scored from different enrichment vintages. The 100-point clamp
  hiding a 0.71 to 0.12 drop is scoring design, not a masking bug.
- Read-time theory proof (shim at `/tmp/eshu-9878-proof`, 168 plans, private
  PostgreSQL 18, 1x/5x/10x corpora of 645/3225/6450 winner keys): the ADOPT
  bar was list+aggregate p95 within 20% of winners-table reads with resweep
  p95 under 30s. Prong 1 FAILED structurally at every scale (list about
  7x/52x/13x, aggregates 8x to 53x; DISTINCT ON over the wide computed
  canonical key plus per-row `BucketSQL` CASE, superlinear 1x to 5x).
  Prong 2 PASSED (resweep p95 at most 224ms at 10x vs 30s budget).
  Read-time vs winners outputs were IDENTICAL at all scales.
- KEV is synchronous at build (`KnownExploited`, `cisa_kev` +30, priority
  100/critical, zero resweeps); read-model visibility waits on the 30s
  maintainer tick (`defaultSupplyChainImpactWinnersInterval`).
- Freezes at ruling time: PR #9941 (keyed winners resweep) OPEN, covering
  `winners_keyed*.go`, `winners_maintainer.go`, `writer*.go`, and
  `supply_chain_impact_canonical_winners_*.go`. Item 1.2 (#9693) OPEN.

## Options

| Option | Cost | Verdict |
| --- | --- | --- |
| (i) Keep per-scope copies; converge by CVE-keyed re-trigger plus content-digest generation skip | Re-trigger wiring; copies kept as bounded storage | **Adopted (amended)** |
| (ii) Read-time enrichment from a reducer-owned table (advisory-group or `cve_id` key) | 7x to 53x read cost, superlinear; page-overlay slice is an accuracy violation | **Rejected** in full and in slices |
| (iii) Fan enrichment data out into finding scopes | Breaks scope ownership and retraction; one missed scope leaves the stale winner | **Rejected as writer**; CVE-to-scopes mapping **adopted as trigger only** |

## Decision

1. Convergence, not sort change: on a KEV/EPSS change for a CVE, re-run
   `supply_chain_impact` in every scope holding that CVE's finding. The
   stale-copy window is bounded by re-trigger drain plus one 30s resweep
   tick; document that bound as the freshness contract.
2. Digest skip first: verify, and if missing wire, the `FreshnessHint` skip
   (`go/internal/collector/vulnerabilityintelligence/vulnruntime/freshness.go`)
   so an unchanged KEV catalog or EPSS answer creates no generation and no
   re-trigger wave. This step is outside PR #9941's file list: not frozen.
3. Duplicate copies accepted as bounded storage in 2.3: copies per finding
   bounded by matching enrichment scopes (about 5 EPSS target scopes at
   today's 447 QA finding CVEs). No write-side dedup in this item.
4. No winner-sort change (zone D parity SQL, frozen by #9941, wrong layer),
   no magnitude-based EPSS selection (newest `score_date` is the contract),
   no foreign-scope writes, no advisory-group pre-keying (moot until Phase 3
   item 3.2), no clamp redesign in 2.3.
5. Sequencing: no edits to winner-select sort, `BucketSQL` call sites,
   aggregates, resweep maintainer, serving indexes, or any PR #9941 file
   until #9941 merges; the re-trigger then builds on the keyed resweep.
   This option never enters zone D, so #9693 is not an entry gate; re-run
   the #9878 fixture after #9693 lands and before claiming 2.3 done.

## Consequences

Good:

- Reads stay on the index-served winners path; no read-cost regression.
- Masking fixed at the layer that causes it (stale vintages), not hidden by
  a sort tweak.
- Digest skip removes the no-change KEV rerun waste at every scale.

Bad:

- Copies persist; storage grows linearly with findings times matching scopes.
- Convergence lags by up to re-trigger drain plus 30s; not synchronous.
- Follow-ups inside #9878: re-trigger implementation with parity-neutral
  proof; fixture re-run after #9693; seeded-CVE QA acceptance until QA has
  natural KEV overlap.

## Implementation plan

Affected paths: `vulnruntime/freshness.go` skip wiring (verify first);
CVE-to-owning-scope re-trigger edge (verify whether a `dependencyCatalog`
edge of the plan section 4.2 kind exists, else add with readiness-floor
treatment) on top of the merged keyed resweep. Patterns to follow: one
writer per scope; idempotent duplicate delivery; no scope-run
serialization as the convergence mechanism. Avoid: winner-sort edits,
foreign-scope writes, group pre-keying. Telemetry: re-trigger enqueues by
CVE-change cause, digest-skip count, convergence lag from enrichment commit
to winner reflection (or name the existing signals that cover all three).

## Acceptance criteria

- [ ] Regression extending the #9878 fixture: two scopes, EPSS lowered with
  newer `score_date`; winner is the older higher copy before, both copies
  and the winner carry the lowered score after. Same pair for a new KEV
  listing reaching a pre-existing finding.
- [ ] Digest RED/GREEN pair: unchanged KEV input skips (no generation, no
  re-trigger); drift commits.
- [ ] Concurrency proof on the re-trigger path with
  `concurrency-deadlock-rigor`: idempotent duplicates, no lost generation.
- [ ] No-regression measurement on the built binary: resweep p95 and
  list/aggregate p95 at the largest available corpus, before and after.
- [ ] Operator telemetry for re-trigger cause, digest skip, convergence lag,
  or named existing coverage.
- [ ] QA: KEV-flagged findings equal findings whose CVE or alias is in KEV,
  seeded CVE as proof.
- [ ] Fixture re-run green after #9693 lands.

## Decisions for the arbiter

None open. Both issue decisions are ruled: option (i) amended wins; with
read-time rejected, groups-vs-`cve_id` keying is moot and enrichment stays
`cve_id`-keyed at build until item 3.2.

<details>
<summary>Evidence and measurements</summary>

- Fixture: commit `6ae57213a4`, 330 lines, core package only; green on lead
  re-run (`go test ./internal/reducer/supplychain/core/ -run
  'TestKEVEPSSRescore' -count=1`, exit 0).
- Proof: `/tmp/eshu-9878-proof` (`bench_pass3.log`, `summarize.py`,
  `check_correctness.py` exit 0 all scales); extractor `/tmp/shimbuild`
  (byte-exact shipped query text, parity diffs clean). Repo untouched.
- Verified prong-1 magnitudes from `bench_pass3.log` medians: list about
  6.8x (1x), 52x (5x), 13x (10x, noisy host); aggregate count 24x/53x/8x.
  Every statistic at every scale misses the 1.2x bar by 5x or more. The
  brief's 6.3x/17x/22-63x ratios did not reproduce under one statistic and
  are not quoted as verified. Shim absolutes are shim-local; winners-side
  aggregates were shim-composed; filters neutral.
- Arbiter: `arbiter-eshu` (`muse-wonder-full-1.0`), 2026-11-03, full text at
  `/tmp/arbiter-9878-decision.md`. Skills: `eshu-performance-rigor`,
  `eshu-postgres-rigor`, `eshu-contract-rigor`, `eshu-diagnostic-rigor`.
- NOT_CHECKED carried forward: KEV rerun cadence in QA and whether
  `FreshnessHint` already skips there; QA scopes-per-CVE distribution;
  whether the CVE-to-owning-scope edge exists; fixture behavior after #9693.

</details>
