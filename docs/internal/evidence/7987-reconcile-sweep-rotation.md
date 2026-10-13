# #7987 reconcile sweep rotation: no-regression and observability evidence

No-Regression Evidence: baseline origin/main `9784ab0ee1`, branch head with
rotation. `TestRepositoryReindexTargetedScopeIsNotStarved` (30 repositories,
limit 10, fake git plus stub resolver, Go 1.26.2) runs the full selection
walk three times: 0.253 s on baseline, 0.257 s with rotation on the same
loaded host; the delta is noise. Per-cycle `FullReconcileState` reads go from
"until the L-th slot fills" to one per repository: indexed primary-key point
lookups, the same class as the per-repository `LastProjectedCommitSHA` read
every cycle already performs, and dwarfed by the per-repository git
subprocess spawns. Terminal counts are unchanged: 10 forced per cycle in
both trees, and the bound test forces 5 of 5 due repositories within 3
cycles at limit 2. The change is safe because forcing, budget, hold, and
reindex-prefix semantics are untouched; only the visit start moves and
evaluation covers the whole cycle. With the #8027 skipped-parse follow-up, a
re-projection that spends a slot without selecting also advances the walk.

Observability Evidence: the new `eshu_dp_collector_reconciliation_due_unvisited`
gauge is asserted in-test (`never_reconciled=3` after a 5-due, limit-2 cycle,
zeros recorded for the other seven reasons). The suppression counter now
counts every evaluated scope rather than only pre-budget ones, so its volume
scales with fleet size; the counter semantics ("each held evaluation
increments") are unchanged.
