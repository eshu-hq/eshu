# Evidence: per-call active-row budget for the supports drain (#11436)

`GetSupplyChainAdvisoriesForRepos`
(`go/internal/storage/postgres/service_vulnerability_advisory_loader.go`)
folded every under-cap page into `byRepo` across ALL requested repositories
with no call-level budget, and `drainActiveSecurityAlertReconciliationFacts`
(`facts_active_security_alert_reconciliation.go`) accumulated alert and
consumption pages with no cap at all. Both now share one call budget
(`supply_chain_impact_supports_call_budget.go`, default 500,000 active rows =
5x the per-drain cap, tunable via
`ESHU_SUPPLY_CHAIN_IMPACT_SUPPORTS_CALL_MAX_ACTIVE_ROWS` floored at the
per-drain default): past it the call fails with the existing truncation error
shape and the resume cursor instead of serving a prefix as complete.

Why: a per-drain cap alone cannot bound a call. 200 repositories at 90,000
actives each pass every per-drain cap and still accumulate 18,000,000
envelopes; the fail-fast truncation (nil result, retryable
`supply_chain_impact_supports_drain_truncated` class) keeps that working set
from OOMKilling the engine the way the uncapped per-drain path did (#11428).

No-Regression Evidence: no statement text changed (the page, fact, and
readiness statements are byte-identical in the diff), so plans and round trips
are unchanged; the only added per-row cost is one integer compare in the
fold, and under the budget the fold is the same append in the same order.
RED-first: the five call-budget tests failed before the implementation
(`go vet ./internal/storage/postgres/` exited nonzero on the undefined budget
symbols), and pass after. Over-budget behavior is pinned by
`TestGetSupplyChainAdvisoriesForReposTruncatesOverCallBudget` (repo-a 501
rows, repo-b cut to 250, cursor `fact-1249`, `activeRows` 751, nil map),
`TestListActiveSecurityAlertReconciliationFactsTruncatesFactStreamOverCallBudget`
(500 + 250 cut, cursor `fact-0749`, fail-fast before any supports statement),
and `TestListActiveSecurityAlertReconciliationFactsTruncatesImpactsOnRemainder`
(1 fact row + 100 supports rows cut, cursor `fact-0099`); under-budget
behavior by `TestGetSupplyChainAdvisoriesForReposServesUnderCallBudget` and
by the untouched suite: 204 related tests green
(`go test ./internal/storage/postgres/ -run
'SupplyChain|Supports|Advisories|Reconciliation|Drain|Truncat|CallBudget'
-count=1`), with only the two `Winners*Live` host-quiet gates failing on
system load (load1 10.59 over the 8.0 ceiling; they skip without
`ESHU_POSTGRES_TEST_DSN` and touch none of these paths). `go vet` and `gofmt`
clean; the commit hooks (including `golangci-lint` on changed packages) pass.
No live backend was used: none is needed where no SQL changed.

Observability Evidence: no new metric, span, or log line. A call-budget cut
reuses `SupplyChainImpactSupportsDrainTruncatedError` (retryable,
`supply_chain_impact_supports_drain_truncated`), and the existing
large-drain log line fires with `outcome=truncated` through the call/drain
sample's truncated flag, exactly as a per-drain cut does.

## Addendum: fact-stream truncation signals (PR #11469 P1 fix)

The fact-stream cut did not keep the paragraph above: it returned before
any `recordSupplyChainImpactSupportsDrain` or
`RecordSupplyChainImpactSupportsRead` call, so a hot filter left no
`outcome=truncated` large-drain line. The fix records the fact-stream
drain sample as failed+truncated and counts the read as failed before
returning the unchanged truncation error, mirroring the supports-phase
cut; the advisory loader already recorded both.

No-Regression Evidence: the success path is byte-identical (the new
`errors.As` check runs only inside the `err != nil` branch) and
non-truncation errors return exactly as before; the added work runs only
on the already-terminal truncation path (one log line plus one counter).
RED-first:
`TestListActiveSecurityAlertReconciliationFactsTruncatesFactStreamOverCallBudget`
failed before the fix with an empty log capture and passes after,
asserting the `large_drain` line with `outcome=truncated`,
`sample=drain`, `reader=security_alert_reconciliation` on the same
500 + 250 cut (cursor `fact-0749`). Focused suite green
(`go test ./internal/storage/postgres/ -run
'TestListActiveSecurityAlertReconciliationFacts|TestGetSupplyChainAdvisoriesForRepos|TestSupplyChainImpactSupportsCallBudget|TestSecurityAlertReadinessProbeFailureIsLogged'
-count=1`), `go vet` and `git diff --check` clean. No live backend:
no SQL changed.

Observability Evidence: still no new metric, span, or log line. The fix
makes the paragraph above true for the fact stream too: the same
`outcome=truncated` large-drain line and the same `ReadFailed` read
count the supports-phase cut already emitted.
