# Supply-Chain Evidence Headroom Gauges (#9778)

## Problem

A `supply_chain_impact` pass may load 100,000 expansion envelopes. Above that it
goes partial and retracts nothing (#7154). The counter
`eshu_dp_supply_chain_impact_evidence_truncated_total` says a pass was
truncated. It cannot say how many scopes are stuck now, or how close the widest
pass is to the limit, and the success log line carried no partial field.

## Change

Two process-local observable gauges and two success-log sub-signals. Nothing
pages, and the budget and retraction rules do not change.

- `eshu_dp_supply_chain_impact_partial_scopes{cause}`: scopes whose last
  recorded pass on this process ended partial, per cause. Every cause reports,
  zero included.
- `eshu_dp_supply_chain_impact_evidence_budget_ratio`: the widest pass in the
  last hour, as the envelopes the pass charged to its budget over the budget.
  Derived facts the protected-scan path adds after the static load are not
  charged and are not in the ratio. No series until a pass has
  run in the window.
- Sub-signals `evidence_partial` and `evidence_budget_ratio` on the
  `reducer execution succeeded` line.

The tracker is `telemetry.SupplyChainImpactHeadroomTracker`, a leaf mutex over a
map of partial scopes and sixty one-minute buckets. `recordEvidenceHeadroom`
feeds it after the writer admits the pass, so a superseded pass cannot overwrite
a newer pass's state, and it reads `truncation.causes()`, the same set that
decides `PartialEvidence`.

## Why process-local

A durable per-scope pass state needs a schema change and a write on every pass.
That is out of scope for the first slice, and the #9694 held-stale gauge set the
precedent. The cost is stated in the metric description and the telemetry page:
a scope another replica completed stays partial here until this process handles
it again (an overstatement), a restart empties the gauge (an understatement),
and the tracker keeps the pass that recorded last, not the highest fencing
token. So the value is a per-replica view, not a bound in either direction; the
WARN log names the exact scope.

## Proof

| Reading | Value | Status |
|---|---:|---|
| Widest pass, expansion envelopes, 115 passes in one hour | 32,984, or 33.0% of the budget | VERIFIED, `06-evidence-headroom-from-log.py`, exit 0 |
| Largest repository, active JS/TS file facts | 3,809 against the 50,000 threshold | VERIFIED, read-only aggregate on the QA read replica, 2026-11-01 18:27Z, 28 s |
| Repositories with JS/TS file facts | 345 repositories, p50 23, p99 1,588, 41,561 files in all | VERIFIED, same query |

The largest repository holds 7.6% of the page threshold, so repository paging
fits the budget with room. That answers the issue's first decision from data.
It does not prove paging is correct: the cross-repository dependency audit and
the paged differential are separate and not part of this slice.

Failing tests first (RED, before the wiring):

```text
TestSupplyChainImpactBudgetTruncatedPassRaisesHeadroomGauges  FAIL  partial_scopes = map[active_expansion_rounds:0 evidence_budget:0 other:0]
TestSupplyChainImpactRoundCappedPassCountsUnderItsOwnCause    FAIL
```

Both pass with the change. Two mutants were killed: dropping the scope release
on a complete pass fails `TestSupplyChainImpactCompletePassReleasesThePartialScope`
and the tracker test, and widening the window by a large amount fails
`TestSupplyChainImpactHeadroomTrackerWidestRatioIsWindowed`.

### Review fixes (verdict FIX-FIRST, one blocking P2)

F1 (blocking). The ratio and the `evidence_budget_ratio` sub-signal read
`expansionEnvelopes`, which the protected-scan path raises with derived
OS-package, analysis, and advisory facts after the static load
(`handler_authorized.go`, `staticLoaded.expansionEnvelopes += derivedFacts`).
Those facts are not charged to the budget, so a protection-engaged pass could
show a ratio above 1 with no partial scope. The loaded evidence now carries
`budgetEnvelopes` (`budget.used`, set in `evidence_load.go`) and the gauge and
sub-signal read it. `expansionEnvelopes` and `evidence_envelopes` keep their
meaning.

RED, before the fix, on the old code (exit 1):

```text
--- FAIL: TestSupplyChainImpactProtectedLoadDerivedFactsAreNotBudgetEnvelopes
    evidence_headroom_authorized_test.go:96: budget_ratio = 2 (seen true), want 0: derived facts are not charged to the budget
FAIL	github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core
```

GREEN after the fix (exit 0). The test runs `Handle` with a protection-engaged
authorized loader, one target holding four derived facts, a budget of 2, and no
truncation. The ratio stays 0, `partial_scopes` stays 0 for every cause, and
`evidence_partial` is 0.

F3. Three edge tests in `supply_chain_impact_headroom_edge_test.go` kill the
mutants the review found. Each mutant was applied to the production file, the
telemetry package run, and the file restored (checked byte-identical):

| Mutant | Edit | Killed by |
|---|---|---|
| M3 window one minute too wide | `oldest := nowMinute - int64(headroomBuckets)` (dropped the `+ 1`) | `TestSupplyChainImpactHeadroomTrackerWindowEdgeIsExactlySixtyMinutes` |
| M9 future-minute bucket allowed | dropped `|| bucket.minute > nowMinute` | `TestSupplyChainImpactHeadroomTrackerSkipsBucketsFromTheFuture` |
| M5 unknown-cause dedupe removed | `if !slices.Contains(folded, cause) \|\| true` | `TestSupplyChainImpactHeadroomTrackerCountsAScopeOnceForRepeatedUnknownCauses` |

F2/F4. The metric description, type comment, telemetry page, and this note now
say "last recorded pass" and describe the gauge as a per-replica view. They no
longer call `max by (cause)` a lower bound or `sum` an upper bound, because a
phantom entry overstates the count and a restart understates it.

F5. The catalog rows are in `supply-chain-active-evidence.md` (a Metric / Type /
Key labels / Use table), which the counter row in `metrics-reducer-storage.md`
links to. That page is at its 500-line cap, so the rows could not go there.

F6. The per-cause dedupe moved from `PartialByCause` to `RecordPass`, once per
pass. A scrape no longer allocates a map per partial scope under the mutex.

No-Regression Evidence: the pass path gains one `RecordPass` call after the
write: a mutex, one map write or delete, and one array slot. No query, no lock
shared with another pass, no transaction change. The gauge callbacks read the
tracker under the same mutex and run on the scrape, not on a pass. The
`supply_chain_impact` focused tests pass under `-race`.

Observability Evidence: `eshu_dp_supply_chain_impact_partial_scopes`,
`eshu_dp_supply_chain_impact_evidence_budget_ratio`, and the
`sub_signal_evidence_partial` and `sub_signal_evidence_budget_ratio` log fields.
The existing counter and WARN line are unchanged.

## Classification audit (partial)

The paging design needs a closed list of the indexes a repository's findings
consult. This is the part read so far, from main at `fb4fef62ab`
(`index.go`, `sbom_subjects.go`, `match.go`, `js_ts_reachability.go`,
`anchor_consensus.go`). Status: PARTIAL, a code read only, no differential.

| Index | Key | Pageable by repository |
|---|---|---|
| JS/TS reachability (`jsTSPackageReachabilityIndex`) | repository id from the file fact | Yes, one entry per repository |
| Package consumption (`index.consumption`) | package id, rows carry a repository id | Yes since #9725: manifest findings classify once per consuming repository, so one repository's rows do not pick another's winner |
| Deployments, workloads, services | exact repository id equality | Yes |
| Image identity consensus (`buildSupplyChainImageIdentityConsensus`) | digest, with votes per candidate repository | No. A digest's anchor repository is decided by counting rows across repositories, so it must stay scope-wide and outside any page |
| SBOM components and attachments, OS packages, scanner analyses | digest or package | No, scope-wide by key |
| CVE and affected-package groups, advisory retraction | CVE id | No, scope-wide |

So the file facts, which are 97.9% of the expansion rows, are repository-keyed
and can page. The cross-repository read is the image identity consensus, and it
is digest-keyed evidence that the file budget does not charge. Not read yet:
the Python, Go and JVM reachability keys, the security-alert findings, and
whether `coalesceAndSortSupplyChainImpactFindings` merges anything across
repositories. Paging must not start before those are read.

## Operating it

| Signal | Reading |
|---|---|
| `evidence_budget_ratio` climbing toward 1 | The widest scope is nearing the budget. Paging by repository becomes urgent. |
| `partial_scopes{cause="evidence_budget"}` above 0 | A scope is past the budget and keeps its stale findings. Find it in the WARN line. |
| `partial_scopes{cause="active_expansion_rounds"}` above 0 | The 8-round cap, not size. Paging does not help it. |

## Not in this slice

- The alert for a scope partial more than an hour: it needs the per-replica
  caveat settled with the dashboard owner.
- A status degraded reason for a scope partial for a day: an arbiter decision in
  the issue.
- `evidence_pages` on the success log: it belongs with the paging change.
