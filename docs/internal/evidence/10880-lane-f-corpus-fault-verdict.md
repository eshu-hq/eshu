# Lane F verdict: PR #11476 corpus + fault-cell failures are harness conversions, not product bugs (Refs #10880)

Date: 2026-11-25. Branch: `drive/10880-c2-lane-f` (== `origin/drive/10880-prc-c2`
at 9f85812bb3). CI run 45704850503 (Build Test, failure) is the evidence run;
run 45713040866 was still in progress at investigation time.

No product code was changed in this lane. No corpus baseline was re-pinned and
no expected set was edited.

## Q1: corpus-gate + differential `active_baseline` failure — verdict (ii)

Failing assertion (`scripts/lib/golden-corpus-vulnerability-suppression.sh`,
`golden_suppression_assert_active_baseline`):

```text
suppression active_baseline query assertion failed: {"count":0,...,
"readiness":{"readiness_state":"readiness_unavailable",
"target_scope":{"cve_id":"CVE-2026-00010"},"evidence_sources":[],
"missing_evidence":["readiness_unavailable"],"freshness":"unknown",
"counts":{"findings_returned":0,...}},...}
```

(run 45704850503, job `corpus-gate (neo4j)` 139524516496, 21:53:34Z; both
differential captures fail identically — same script, same stack shape.)

Root cause: the corpus gate runs `eshu-api` with
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` unset (repo-wide search: only
`scripts/lib/read-api-latency-core.sh` and the latency gate set it; the
corpus gate script, its libs, and the workflow do not), so the findings list
takes `RouteLegacy` (`routeFor(false, ...)` returns legacy for every filter
shape, pinned by `TestRouteForEnumeratesEveryExistenceFilter`). The legacy
statement (`ListFindingsQuery`) reads `FROM fact_records AS fact WHERE
fact_kind = reducer_supply_chain_impact_finding`. The C2 writer no longer
writes that kind — `WriteSupplyChainImpactFindingRows`
(`go/internal/reducer/supplychain/core/writer_supports.go`) upserts supports
only ("It no longer writes the finding fact rows"), and migration 357 refuses
any residual finding-fact write (`WHEN (NEW.fact_kind =
'reducer_supply_chain_impact_finding')`). Zero finding facts means `count: 0`
on every legacy read, deterministically, on all three jobs.

Why not (i): supports readiness is not consulted by the legacy route at all
(`requireSupportsReady` runs only for `readsSupports()` routes), and the
product write path demonstrably resolved — every drain in the failing log
reaches `residual=0, dead_letter=0` (corpus drain, scope_setup, malformed),
including the impact domain. Early deferrals resolving to success are the
designed fail-closed behavior, not a stuck fence.

Why not (iii): no gate step seeds finding facts. The only direct fact INSERT
(the malformed-suppression fixture) writes `vulnerability.suppression`, a kind
the 357 trigger does not cover; corpus source facts flow through the projector
normally and drains pass.

`readiness_unavailable` is a secondary symptom on the same response: the
readiness snapshot reads only source-fact families plus keys tables
(`readiness_families.go` — no finding kind among them), none in C2's blast
radius, so it cannot be a write-stop regression by construction. Candidates
are consumption-keys-not-ready or a transient statement error under CI load;
it does not change the verdict (`count: 0` fails the assert regardless).
Next step for the owning lanes: capture the API `readiness_unavailable_cause`
WARN on the converted run to close this sub-question.

### Conversion spec for lanes A–D (corpus gate, do not re-pin baselines)

1. `scripts/verify-golden-corpus-gate.sh`: export
   `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=true` for the `eshu-api` it starts
   (covers corpus-gate + both differential captures; check the MCP server path
   if it serves findings reads too).
2. After drains, run the production winners resweep to cover the drained
   generations before query truth — the drain gate's residual bound does not
   cover winners materialization. Precedents: `9306cdd575`
   (`go/cmd/read-api-latency-gate/seed_winners.go` seeds through
   `WriteSupplyChainImpactFindingRows` + production resweep); the advisory
   cell's `eshu-ifa enqueue-advisory-groups` runs the production winners
   rebuild.
3. No expected-output edits: B-7c pins hold on the winners route by the B2a
   parity contract; the malformed-fixture path needs no change (suppression
   kind unaffected by trigger 357). Legacy reads are unseedable by design, so
   enabling winners reads is required, not optional.

## Q2: `cell_impact_family` rationale mismatch — harness, not product

Failing line (run 45704850503, `fault-injection (shard 8/15)` 139524516982):

```text
=== cell impact-family: fresh stack ===
...drives only the 3 impact cassettes (vulnerabilityintelligence,
sbomattestation, packageregistry)...
[PASS] fact_work_items_residual: residual=0 ... dead_letter=0
=== impactfamily: assert rationale materialized records (full-record exact set) ===
ifa assert-edges: ... do not match ... missing (3): ...
verify-ifa-fault-injection: impactfamily: rationale graph does not match the
three-record exact set
```

Root cause: `cell_impact_family`
(`scripts/lib/ifa_fault_injection_impact_cells.sh`) drives only its three
impact cassettes on a fresh stack, then calls `run_drain_gate`, whose tail
unconditionally runs `assert_rationale_truth` (the three-record rationale
oracle). Every other family cell calls `drive_all_cassettes` first (which
drives the rationale cassette), so only this cell starves the oracle —
`missing (3)` on an empty graph is exactly what a correct oracle reports when
given no input. Queue truth was clean (residual 0, no dead letters), so the
rationale projection lost no input; the expected set stays correct for all
rationale-driving cells and must not be edited.

Same run's static mirror fails the same way:
`test-verify-ifa-fault-injection: no catalog-label translation for dispatched
cell impact_family`. The catalog has entry 134 (`impact-family`), and the
dispatch count matches (134 == 134), but
`scripts/lib/test-ifa-fault-injection-entrypoint-cases.sh`
(`run_ifa_fault_cell_catalog_cases`) has no `impact_family)` branch.

### Conversion spec for lanes A–D (fault cell, do not touch expected sets)

1. `scripts/lib/test-ifa-fault-injection-entrypoint-cases.sh` (~line 139):
   add `impact_family) label="impact-family" ;;` to match catalog entry 134.
2. `scripts/lib/ifa_fault_injection_driver.sh` `run_drain_gate`: extract a
   rationale-free drain variant (gate poll + dead-letter semantics without
   `assert_rationale_truth`); `cell_impact_family` calls it. Do not add
   rationale driving to this cell (its header contract is impact-only) and do
   not touch `rationale_expected_edges` or
   `testdata/golden/ifa-impact-family-digest.txt`.
3. The cell's own assertions (dead letters, trigger armed + unrejected,
   supports+winners digest vs pin) then run as authored — they never executed
   in the failing run because `run_drain_gate` died first.

## Unblock note for lanes waiting on expected-output edits

No expected-output edits are warranted anywhere in this failure set: the
rationale three-record set and the corpus B-7c pins stay as-is. Both failures
are stale read-path/oracle wiring around the fact-free writer, fixed by the
two conversion specs above.

## Focused proof run in this lane

- `go test ./internal/query/supply/chain/impact/ -run
  TestRouteForEnumeratesEveryExistenceFilter` → ok (gate-off reads legacy for
  every filter shape).
- `go test ./internal/storage/postgres/ -run
  'TestImpactWriteTransactionTakesNothingTheFenceNeedsBeforeTheLock|TestImpactWriteTransactionSuppressionFamilyCensus'`
  → ok (no pass statement writes `fact_records` after the C2 write stop).
- No live gates run here (a `ci-gates` run was in flight; the expensive proof
  belongs to the converted run in lanes A–D).
