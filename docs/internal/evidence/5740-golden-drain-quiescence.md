# #5740 Golden Drain Quiescence Evidence

## Accuracy proof

The producer-completion lane is a third reducer ledger. A successful producer
ACK appends `cross_scope_completion_events`; fanout deletes the event in the same
statement that reopens its canonical consumer work. Reading only
`fact_work_items` and `shared_projection_intents` can therefore report a false
drain while a future-visible completion event still exists.

The retained 30-repository synthetic golden database reproduced that failure.
With one pending `container_image_identity` completion event and both older
ledgers at zero, the old gate returned exit 0 with 2 passes and 0 required
failures. Restarting the exact retained reducer consumed the event, reopened the
CI/CD and supply-chain consumers, and returned all three domains to `succeeded`.

The regression was written before the implementation. The repaired gate reads
all three ledgers in one Postgres statement, so every scalar subquery shares one
MVCC snapshot. Against the same retained state, the repaired binary returned
exit 1, named `container_image_identity/pending=1`, and reported 1 required
failure. Removing the synthetic probe returned the drain to 3 passes and 0
required failures.

## Performance and observability

No-Regression Evidence: `EXPLAIN (ANALYZE, BUFFERS, WAL)` on the retained golden
database measured the new completion-event count at 0.040 ms with one shared
buffer hit. The final atomic eight-value snapshot executed in 0.205 ms with 66
shared-buffer hits and no WAL. It replaces seven client/server round trips with
one and runs only in the golden-corpus gate, never on a production request or
reducer write path.

Observability Evidence: a timeout now reports the total completion-event count
and groups residual events by bounded producer domain and status. The required
finding `cross_scope_completion_events_nonterminal` is printed on every drain,
including successful zero reads.

## Live pipeline proof

The post-fix golden gate ran the committed comprehensive query profile over the
fresh synthetic 30-repository corpus. Every initial, maintenance, and
suppression drain observed zero completion events. The active, ignored-hidden,
ignored-audit, identical-retry, and expired-visible suppression states all
passed. The retained exact-head result was 518 passes, 0 required failures, 1
advisory warning because the maintenance drains took 27 seconds, and 118
seconds total against the 1,800-second ceiling.
