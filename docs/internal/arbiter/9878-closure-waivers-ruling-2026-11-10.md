# Closure waivers for #9878 — arbiter ruling (2026-11-10)

Provenance (record-keeping, not part of the ruling): the text below is the
full result of arbiter-eshu (muse-wonder-full-1.0), subagent
01a1c510-338e-7573-8ded-81b246543fab, dated 2026-11-10, recorded by the
coordinator (arbiters do not edit; coordinators record). Absolute file
links in the original were made repo-relative, two environment names rephrased for the private-identifier gate, and raw line citations converted to file anchors for the doc-citations gate; no ruling meaning changed.
Tracker created under section 3: eshu-hq/eshu#10508 (KEV alias-group unit
pin). Section 5 was discharged by the coordinator before recording: the
four `9878Live` tests pass on this tree (see
`docs/internal/evidence/9878-enrichment-retrigger.md`).

**#9878 closes when the four decisions below are recorded and the required
proof in §5 is in the closure PR. No decision trades accuracy, performance,
or concurrency for closure; two decisions narrow what #9878 ever owned, one
re-scopes a performance AC to the surface the code actually touched, and one
accepts a proof split across two repos by composition.**

Arbiter: `arbiter-eshu` (`muse-wonder-full-1.0`). Skills loaded:
`eshu-performance-rigor`, `eshu-correlation-truth`, `eshu-diagnostic-rigor`.
Verified against `origin/main` `7e5e91712b` (arbiter-ran
`git rev-parse origin/main`, exit 0).

## Facts the ruling rests on

- AC1 fixture 3/3 PASS this session; AC4 merged in #9968
  (`6a8b64e5f1`), unit tests green — coordinator sweep, accepted as given.
- QA live equality 2026-11-10 (QA-environment image `sha-33075aeb`): KEV scope
  fresh, 1773 KEV CVEs, findings with CVE-in-KEV = 2622 = flagged,
  mismatches 0 both ways, winners 113 = 113, real row CVE-2022-22965
  (spring-beans) — coordinator sweep, accepted as given.
- #9968's production diff (`e92605824f --stat`, arbiter-ran): `fanout.go`,
  `fanout_signals.go`, `evidence_load.go`, `impact.go` (+2), telemetry
  instruments. No winners-resweep file, no read SQL, no winner sort.
- The 4 live PG tests are `dedicated_ci` under the `reducer-contention`
  runner (`specs/live-tests.d/live-tests-07.yaml:313-317`, summed 7.7s).

## Decisions

### 1. (a) Read-time shim artifacts — ACCEPTED as AC2 proof; no re-run

The shim was a prove-the-theory-first gate for a design choice, not a
performance claim about shipped code. Its decision value survives in the
record with the adopt bar, scales, and medians
([decision record](../../../docs/internal/design/9878-enrichment-refresh-decision.md),
[:144](../../../docs/internal/design/9878-enrichment-refresh-decision.md)):
prong 1 failed structurally at every scale, every statistic missing the
1.2x bar by 5x or more; read-time vs winners outputs were identical. A
margin that wide is not sensitive to the lost raw logs. A re-run today
could not reproduce the proof anyway: the extractor used decision-time
query text, which has since drifted, so it would measure today's queries,
not the rejected design as evaluated. Conditions: the summarized medians
must never be quoted as current performance figures (the record already
disclaims them as shim-local,
[:148](../../../docs/internal/design/9878-enrichment-refresh-decision.md));
if read-time enrichment is ever revived, it needs a fresh theory proof
against the then-current queries before any build.

### 2. (b) Largest-corpus p95 no-regression — AMENDED to touched-path scope; closed at that scope

The acceptance line at
[:120](../../../docs/internal/design/9878-enrichment-refresh-decision.md)
names resweep p95 and list/aggregate p95. The implementation that landed
touches neither surface (diff fact above;
[evidence note](../../../docs/internal/evidence/9878-enrichment-retrigger.md)
records zero shared code with resweep/read SQL). Measuring those paths
before/after a label-only diff on a shared host would measure host noise,
not this change — the note's own n=1 disclaimer
([:114](../../../docs/internal/evidence/9878-enrichment-retrigger.md))
says as much. The AC is therefore re-scoped, not waived: proof is required
for the touched path, and it exists — allocation-identical microbenchmarks
on `BenchmarkKEVEPSSRescoreBuild9878` and `BenchmarkWritePayloadWithDigest`
before/after at 1x/5x/10x
([:123](../../../docs/internal/evidence/9878-enrichment-retrigger.md),
[:132](../../../docs/internal/evidence/9878-enrichment-retrigger.md)),
with the resweep mechanism itself already barred by the recorded adopt-bar
(p95 ≤ 224ms at 10x vs 30s,
[decision :41](../../../docs/internal/design/9878-enrichment-refresh-decision.md))
and the list/aggregate budget owned continuously by the `read-api-latency-gate`
CI workflow (in the required-gates aggregate,
[.github/workflows/read-api-latency-gate.yml](../../../.github/workflows/read-api-latency-gate.yml:16)).
The QA live equality is accuracy proof for this waiver set; it proves
nothing about p95 and must not be cited as if it does. Binding conditions:
(a) closure text claims only "no-regression by construction plus allocation
parity on the touched path" — never "p95 no-regression proven";
(b) the evidence note's `NOT_CHECKED` for a quiet-host p95 series
([:128](../../../docs/internal/evidence/9878-enrichment-retrigger.md),
[:155](../../../docs/internal/evidence/9878-enrichment-retrigger.md))
stays as written — it becomes a noted residual owned by the CI latency
gate and the winners watermark gauges, not a #9878 blocker and not a
silent deletion.

### 3. (c) AC5 "CVE or alias" — NARROWED to direct-CVE for #9878; alias proof deferred with a tracked owner

First, a correction to the brief: the `index_build.go` range the brief cited is not alias
merging; it is direct KEV keying by `kev.CVEID`
([index_build.go](../../../go/internal/reducer/supplychain/core/index_build.go)).
The alias-group risk-signal merge lives in
[os_advisory_alias_apply.go](../../../go/internal/reducer/supplychain/core/os_advisory_alias_apply.go)
(`KnownExploited` merge at
[the KnownExploited arm](../../../go/internal/reducer/supplychain/core/os_advisory_alias_apply.go)),
invoked from
[index_build.go](../../../go/internal/reducer/supplychain/core/index_build.go),
and it is OS-advisory alias grouping — the Phase 3 item the decision
record explicitly deferred ("no advisory-group pre-keying… moot until
Phase 3 item 3.2", enrichment "stays `cve_id`-keyed at build until item
3.2",
[decision :73](../../../docs/internal/design/9878-enrichment-refresh-decision.md),
[same record](../../../docs/internal/design/9878-enrichment-refresh-decision.md)).
The alias leg therefore never belonged to item 2.3's mechanism. Ruling:
AC5's QA acceptance for #9878 is direct-CVE equality, and the QA sweep
(2622 = 2622, 0 mismatches both ways, winners 113 = 113) closes it. A
seeded QA alias case is rejected: fabricated QA data would prove the
seed, not corpus truth, at disproportionate cost. But the alias leg is
not deleted into silence — arbiter search found **zero KEV-specific alias
tests** (`rg "kevSource|KnownExploited"` over the alias test files: no
matches), so that `KnownExploited` merge is
currently unproven anywhere. Before #9878 closes, file a follow-up issue
under alias-grouping ownership requiring one seeded **core unit** pin
(one group member in KEV ⇒ grouped finding carries `known_exploited`),
linked from the closure PR with a `DEFER` line naming this ruling. That
issue blocks the alias-grouping item's closure, not #9878's.

### 4. (d) KEV/EPSS digest-skip proof location — ACCEPTED split across core seam + satellite; no restored core test

`3c0343d5b0` (#9999, ancestor of `origin/main`, arbiter-verified) deleted
the in-tree collector including `freshness_skip_9878_test.go` and
`vulnruntime/freshness.go`. The mechanism now legitimately lives in two
halves. Core owns the generic decision: `skipUnchangedGeneration`
consults `backfill.DecideUnchangedGeneration` before any write
([ingestion_backfill_seam.go](../../../go/internal/storage/postgres/ingestion_backfill_seam.go)),
pinned by `TestDecideUnchangedGenerationCells`
([generation_decide_test.go](../../../go/internal/storage/postgres/backfill/generation_decide_test.go)) —
hint-match skips, drift stores, for every hint-bearing scope. The
satellite (`eshu-collector-vulnerability-intelligence`, arbiter-read at
its local HEAD) owns the KEV/EPSS hint: `freshnessHint` is a pure content
digest (`freshness.go:21-34`), with `TestFreshnessHintIsStableForUnchangedContent`
(:52), `TestFreshnessHintChangesWithContentDrift` (:68),
`TestFreshnessHintMatchesTheInTreeCollectorGolden` (:110) and the shared
fixture (:351) proving byte-continuity with the deleted in-tree digest,
and `TestHandleSetsHint…`/`TestHandleHintIsStableAcrossClaims…`
(:157, :183) proving the hint is actually set. Composition closes the
RED/GREEN pair: stable hint (satellite) + hint-generic skip (core) = no
generation, no re-trigger wave; drift flips both. Restoring a
KEV/EPSS-specific test in core is rejected: core no longer owns this
collector, so such a test would fake the collector to prove a generic
seam. One repair is mandatory in the closure PR: the in-repo citations
to `freshness_skip_9878_test.go`
([evidence note :29](../../../docs/internal/evidence/9878-enrichment-retrigger.md))
and `vulnruntime/freshness.go`
([decision :64](../../../docs/internal/design/9878-enrichment-refresh-decision.md))
now dangle in this repo; repoint them to the satellite `freshness.go` /
`freshness_test.go` plus the core seam above, so the doc-citations gate
and future readers land on live proof.

### 5. Re-running the 4 live PG tests — REQUIRED on the current tree

QA equality does not substitute. It is a snapshot of flag correctness on
a different binary (`sha-33075aeb`), and it cannot observe the dynamics
the quartet proves: lowered-EPSS before/after winner flip, first-KEV
propagation, the commit-during-holder-pass interleave (the concurrency
proof behind
[decision :118](../../../docs/internal/design/9878-enrichment-refresh-decision.md)),
and identical-rescan no-fan-out
([live test file](../../../go/internal/storage/postgres/supply_chain_impact_fanout_enrichment_9878_live_test.go),
[KEV listing test](../../../go/internal/storage/postgres/supply_chain_impact_fanout_enrichment_9878_live_test.go),
[holder-pass test](../../../go/internal/storage/postgres/supply_chain_impact_fanout_enrichment_9878_live_test.go),
[identical-rescan test](../../../go/internal/storage/postgres/supply_chain_impact_fanout_enrichment_9878_live_test.go)).
Since #9968 merged, alias grouping changed `riskSignals` handling in the
same index build — exactly the kind of drift a merge-time pass cannot
cover. Required proof, either form: a local run of
`go test ./internal/storage/postgres/ -run '9878Live' -count=1` on the
closure tree with exit code recorded, **or** a cited green
`reducer-contention` dedicated-CI run at or after the closure tree's
base. (~8s of test time; cost is not a reason to skip.) This mirrors why
AC1's fixture was re-run this session rather than trusted from merge time.

## Required proof checklist for the closure PR

1. §5 run (or cited CI green) with command and exit code.
2. §3 follow-up issue number + `DEFER` line citing this ruling.
3. §4 citation repoints in the decision record and evidence note.
4. §2 claim wording exactly as bounded; `NOT_CHECKED` p95 lines intact.
5. QA equality numbers and image ID pasted from the 2026-11-10 sweep.

## What this ruling will not do, and why

- **No shim re-run** — it would measure drifted queries and re-litigate a
  decision carried by a ≥5x structural margin.
- **No largest-corpus p95 series for #9878** — the named surfaces are
  untouched by the only code diff; the budget has a continuous CI owner.
  This is a scope correction, not permission to skip performance proof
  on any future touched hot path.
- **No seeded QA alias case and no alias leg in #9878's QA gate** — the
  alias mechanism is item 3.2's; its proof must land as a unit pin under
  that ownership (§3), not as fabricated QA data here.
- **No restored KEV/EPSS core test** — core relinquished this collector
  in #9999; proof by composition across the seam is the honest shape.
- **No closure on QA equality alone** — snapshot equality on another
  binary never proves convergence dynamics or concurrency on this tree.

## NOT_CHECKED (arbiter did not verify)

- Ancestry of the QA-environment image `sha-33075aeb` relative to `7e5e91712b`;
  the QA numbers themselves (accepted from the coordinator sweep).
- AC1/AC4 test execution this session (accepted from the sweep).
- Whether a current-main `reducer-contention` run is already green
  (§5 accepts it if cited; the arbiter ran no tests per the read-only
  brief).
- Quiet-host p95 series for keyed tick and list/aggregate — carried as
  the §2 residual, deliberately unmeasured by this ruling.
- Satellite test *execution* (arbiter read test names and the hint
  contract; running satellite suites was out of brief scope).
