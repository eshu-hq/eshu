# Ifá fault-injection cell catalog

Every cell `scripts/verify-ifa-fault-injection.sh` dispatches, in dispatch order.

This lived as a comment block inside that script until the script reached its 500-line cap and
a three-entry correction could not be added without breaking it. It is prose — nothing parses
it — but it drifts silently, so `scripts/test-verify-ifa-fault-injection.sh` asserts that the
number of entries here equals the number of `ifa_fault_shard_run` dispatches in the gate AND that
the numbered titles match the dispatch order entry for entry. Add a cell, add its entry in
dispatch position, or that check reds.

Entries are numbered in dispatch order. The number is a stable label for cross-references
elsewhere in the gate (for example the once-fired marker list), not an index into
`IFA_FAULT_ALL_CELLS` — that array excludes the global baseline, so it holds one fewer element
than there are entries here.

1. baseline                              -- fault-free; establishes the
   digest every non-delta recovery cell is compared against. Cell 11
   deliberately does not compare against it; see that entry.
2. kill-worker-after-claim                -- `kill -9` the live host
   eshu-reducer process after a row is genuinely claimed, then start a
   fresh reducer process and let the fixed 1-minute lease
   (postgres.NewReducerQueue's hardcoded time.Minute,
   go/cmd/reducer/main_helpers.go) expire and get reclaimed.
3. expire-lease-mid-handler                -- force `claim_until = now()`
   directly via SQL on a genuinely claimed row (no kill), so the running
   reducer's OWN claim query (claimReducerWorkQuery's
   `claim_until <= $1`) reclaims it on the next poll while the original
   handler goroutine is still in flight.
4. fail-graph-write-once-then-succeed      -- the tagged
   (-tags ifafaultinjection) eshu-reducer with ESHU_IFA_FAULT_SCRIPT
   pointed at a queue-retry fault script that fails the CloudResource
   MERGE exactly once via go/internal/storage/cypher.FaultingExecutor.
5. restart-backend-between-phase-groups    -- the same tagged reducer
   with a fault script that pauses after the first completed graph-write
   group; this gate restarts the nornicdb Compose service while the
   reducer is blocked on that pause, then releases it.
6. kill-worker-after-claim-sql (#5555)     -- mirrors cell 2, but
   wait_for_claimed is scoped to domain=sql_relationship_materialization
   specifically, provably targeting SQL work instead of whichever domain
   the driven cassettes happen to schedule first (in practice GCP).
7. kill-worker-after-claim-code-calls (#5991) -- mirrors cells 2 and 6,
   but waits specifically for a claimed code_call_materialization row and
   exact-asserts the five code-call edges after reclaim.
8. kill-worker-after-claim-documentation (#5994) -- waits for a claimed
   documentation_materialization row, then proves lease reclaim and the
   exact three-edge DOCUMENTS set.
9. kill-worker-after-claim-rationale (#5998) -- holds the rationale
   handler's shared-intent write, kills the exact reducer process after a
   rationale_materialization claim, and proves reclaim above baseline.
10. duplicate-delivery (#5544)             -- drain once cleanly, then force
   every succeeded reducer row back to a claimable pending state in SQL and
   drain again. Proves the write path is idempotent under at-least-once
   redelivery: the graph after the second drain must equal the fault-free
   baseline exactly. The redelivery count is asserted > 0, so an UPDATE
   that stops matching fails loudly instead of making the second drain a
   no-op that passes vacuously.
11. delta-retract (#5544)                  -- drive the committed
   generation-2 SQL and rationale cassettes through
   ifa_det_run_sql_delta_live, the same
   helper scripts/verify-ifa-determinism.sh calls, so the two gates cannot
   drift on what a correctly-landed delta means. SQL and rationale
   generation 1 are asserted to have materialized BEFORE generation 2 is
   driven, otherwise "the retract removed it" and "it never arrived" are
   indistinguishable.
   This is the ONE cell that does not compare to the baseline digest:
   generation 2 changes the graph on purpose (it retracts one INDEXES edge
   and adds another), so that comparison would fail correctly and invite
   the wrong fix. Its proof is the SQL exact expected-v2 edge set plus the
   rationale exact-one edge record, Charge survivor, and durable lifecycle
   counts.
12. fail-graph-write-once-then-succeed-sql (#5555) -- mirrors cell 4, but
   the fault is anchored to a SQL edge MERGE (QUERIES_TABLE) instead of
   CloudResource. Fired-fault proof is the once-fired marker the fault
   decorator writes at injection time, not a log
   line, not fact_work_items attempt_count: sql_relationship_
   materialization's graph writes ride the async shared-projection
   intent path, which has no attempt_count column (see
   go/internal/reducer/shared_projection_runner.go's
   TestSharedProjectionRunnerLogsPartitionProcessingError).
   Runs by default since #5974. It was held out for months on the belief
   that the fault did not fire in CI; it always did, and the assertion was
   calling a binary the runner lacks. See the call site below.
13. fail-graph-write-once-then-succeed-code-calls (#5991) -- mirrors cells
   4 and 12, but anchors the one-shot queue-retry fault to the code-call
   CALLS MERGE, proves the durable marker names that operation, and
   exact-asserts the five code-call edges after recovery.
14. fail-graph-write-once-then-succeed-documentation (#5994) -- anchors the
   one-shot queue-retry fault to the DOCUMENTS MERGE and reasserts the exact
   three-edge set after recovery.
15. fail-graph-write-once-then-succeed-rationale (#5998) -- anchors the
   one-shot queue-retry fault to the production EXPLAINS MERGE and requires
   its marker, exact three-record graph truth, and exact durable counts.
16. baseline-deployable-unit (#5993) -- a FAMILY-SCOPED fault-free
   baseline, not a recovery cell: deployable_unit_edges materializes
   nothing without a bootstrap-index maintenance pass this gate's other
   cells never run (see scripts/lib/ifa_deployable_unit_live.sh's header),
   so the shared cell 1 baseline's digest has zero deployable_unit_edges
   materialization by construction and cannot serve cells 17-18 below.
17. kill-worker-after-claim-deployable-unit (#5993) -- mirrors cells 6-7,
   scoped to domain=deployable_unit_correlation, run AFTER a maintenance
   pass opens the readiness gate CrossRepoRelationshipHandler.Resolve
   checks.
18. fail-graph-write-once-then-succeed-deployable-unit (#5993) -- mirrors
   cells 12-13, anchored to the CORRELATES_DEPLOYABLE_UNIT MERGE, also
   run after the same maintenance pass.
19. baseline-codeowners (#5992) -- family-scoped fault-free baseline.
20. kill-worker-after-claim-codeowners (#5992) -- lease reclaim proof.
21. fail-graph-write-once-then-succeed-codeowners (#5992) -- retry proof.
22. baseline-repo-dependency (#5999) -- maintenance-backed family baseline.
23. kill-worker-after-claim-repo-dependency (#5999) -- full reclaim lifecycle.
24. fail-graph-write-once-then-succeed-repo-dependency (#5999) -- exact retry proof.
25. baseline-submodule-pin (#6002) -- family-scoped fault-free baseline.
26. kill-worker-after-claim-submodule-pin (#6002) -- lease reclaim proof.
27. fail-graph-write-once-then-succeed-submodule-pin (#6002) -- retry proof.
28. baseline-kubernetes-namespace-environment (#6309) -- family-scoped fault-free baseline; first direct-materialization family in this gate.
29. kill-worker-after-claim-kubernetes-namespace-environment (#6309) -- lease reclaim proof.
30. fail-graph-write-once-then-succeed-kubernetes-namespace-environment (#6309) -- TARGETS_ENVIRONMENT retry proof.
31. baseline-iam-instance-profile-role (#6309) -- family-scoped fault-free baseline.
32. kill-worker-after-claim-iam-instance-profile-role (#6309) -- lease reclaim proof.
33. fail-graph-write-once-then-succeed-iam-instance-profile-role (#6309) -- HAS_ROLE retry proof.
34. baseline-inheritance (#5996) -- family-scoped fault-free baseline.
35. kill-worker-after-claim-inheritance (#5996) -- lease reclaim proof.
36. fail-graph-write-once-then-succeed-inheritance (#5996) -- retry proof.
37. baseline-shell-exec (#6001) -- family-scoped fault-free baseline.
38. kill-worker-after-claim-shell-exec (#6001) -- lease reclaim proof.
39. fail-graph-write-once-then-succeed-shell-exec (#6001) -- retry proof.
40. baseline-workload-dependency (#6003) -- family-scoped fault-free baseline.
41. kill-worker-after-claim-workload-dependency (#6003) -- lease reclaim proof.
42. fail-graph-write-once-then-succeed-workload-dependency (#6003) -- retry proof.
43. baseline-symbol-runtime (#5995/#6000/#5997) -- ONE baseline shared by
   handles_route/runs_in/invokes_cloud_action; no kill-worker cell (ifa_fault_injection_symbol_runtime_cells.sh's header explains why).
44. fail-graph-write-once-then-succeed-handles-route (#5995) -- HANDLES_ROUTE retry proof.
45. fail-graph-write-once-then-succeed-runs-in (#6000) -- RUNS_IN retry proof.
46. fail-graph-write-once-then-succeed-invokes-cloud-action (#5997) -- INVOKES_CLOUD_ACTION retry proof.

47. kill-worker-after-runner-lease-wait-handles-route (#6208) -- exact-key reclaim proof.
48. kill-worker-after-runner-lease-wait-runs-in (#6208) -- exact-key reclaim proof.
49. kill-worker-after-runner-lease-wait-invokes-cloud-action (#6208) -- exact-key reclaim proof.
