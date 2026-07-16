# Dead-Code Shared Scan-Budget Proof (#5248)

The first fairness fix gave each of the six candidate labels an independent
2,500-row allowance. That corrected first-label starvation, but widened the
worst-case raw scan from 2,500 rows and 10 pages to 15,000 rows and 60 pages.
Every page also hydrates content and probes incoming reachability, so the wider
ceiling was rejected before push.

The final schedule round-robins labels under one shared 2,500-row ceiling.
Sparse labels retire after a short page; saturated labels keep taking turns.
This preserves later-label fairness without increasing the old maximum work.

Performance Evidence: a deterministic saturation shim used the actual
`scanDeadCodeCandidates`, `scanDeadCodeInvestigation`, and
`scanCrossRepoDeadCodeCandidates` production functions. The store returned a
full page for every requested label, batch-hydrated every candidate, and marked
every candidate reachable so each scanner had to consume its complete raw-row
budget. One-label calls reconstruct the rejected per-label schedule without a
second implementation of scanner logic.

| Scanner | Shape | Wall time | Rows | Pages | Hydrated IDs | Reachability IDs | Labels reached |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| dead-code | legacy global ceiling | 8.696 ms | 2,500 | 10 | 2,500 | 2,500 | 1 |
| dead-code | rejected per-label ceiling | 42.227 ms | 15,000 | 60 | 15,000 | 15,000 | 6 |
| dead-code | shared round-robin ceiling | 7.378 ms | 2,500 | 10 | 2,500 | 2,500 | 6 |
| investigate | legacy global ceiling | 7.601 ms | 2,500 | 10 | 2,500 | 2,500 | 1 |
| investigate | rejected per-label ceiling | 46.290 ms | 15,000 | 60 | 15,000 | 15,000 | 6 |
| investigate | shared round-robin ceiling | 7.125 ms | 2,500 | 10 | 2,500 | 2,500 | 6 |
| cross-repo | legacy global ceiling | 7.525 ms | 2,500 | 10 | 2,500 | 2,500 | 1 |
| cross-repo | rejected per-label ceiling | 44.553 ms | 15,000 | 60 | 15,000 | 15,000 | 6 |
| cross-repo | shared round-robin ceiling | 7.954 ms | 2,500 | 10 | 2,500 | 2,500 | 6 |

These synthetic wall times compare scheduler and downstream Go work on one
machine; they are not retained-Postgres latency claims. The load-bearing result
is the exact work bound: the final shape restores rows, pages, hydration IDs,
and reachability IDs to the legacy maximum while reaching all six labels.

No-Regression Evidence: the existing saturated-first-label tests exercise all
three scanners and now prove the later `Class` row is returned without exceeding
the shared ceiling. Exact-kind requests still schedule only the requested label.
The proof command is:

```bash
cd go && GOCACHE=/tmp/eshu-5248-round-robin-proof go test ./internal/query \
  -run 'TestDeadCodeRoundRobinSaturationProof|Test(DeadCodeScan|DeadCodeInvestigation|CrossRepoDeadCode)ContinuesAfterFirstLabelSaturates|TestHandleDeadCodeReportsSharedTotalAndPerLabelCandidateScanLimits' \
  -count=1 -v
```

No-Observability-Change: the response keeps the existing candidate scan limit,
page, row, and truncation fields. The scheduler changes no metric, span, graph
write, queue, worker, transaction, or concurrency setting.
