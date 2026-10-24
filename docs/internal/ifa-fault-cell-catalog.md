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
3. expire-lease-mid-handler                -- the tagged reducer holds exactly
   one direct GCP handler after a successful graph write, outside graph permits
   and transactions. The shell expires that recorded work item's exact claim
   epoch via SQL (no kill), requires a committed nonzero mutation and the same
   item's replacement epoch while the original handler remains held, then
   releases it and retains strict reclaim, drain, dead-letter, and digest checks.
   A prior claimed-row count alone cannot prove expiry landed (#7939).
   After the drain the cell reads the real reducer's log for the held work
   item and requires at least one fenced-outcome record (`ack_claim_rejected`,
   `ack_outcome_unknown`, or `lease_lost_during_execution`), at most one
   `succeeded` record, and at least two ACK outcome records in total; it then
   reads the queue row and requires status `succeeded` at the replacement
   claim (attempt_count = held + 1, a new last_attempt_at, lease cleared). A
   stale ACK accepted after the successor's own ACK would add a second
   `succeeded` record and fail the cell. When the stale and the successor ACK
   share one flush batch, the queue commits the successor and names the
   dropped stale claim, so the Service logs the successor as `succeeded` and
   the stale claim as `ack_claim_rejected` (#8007);
   the log carries no claim epoch, so the check does not attribute a record to
   the stale claim or the successor. Epoch-level rejection against a live
   successor is proven by `TestReducerQueueExpiryInterleaveLive` (class
   scheduled).
4. fail-graph-write-once-then-succeed      -- the tagged
   (-tags ifafaultinjection) eshu-reducer with ESHU_IFA_FAULT_SCRIPT
   pointed at a queue-retry fault script that fails the CloudResource
   MERGE exactly once via go/internal/storage/cypher/fault/executor.FaultingExecutor.
5. restart-backend-between-phase-groups    -- the same tagged reducer
   with a fault script that pauses after the first completed graph-write
   group; this gate restarts the nornicdb Compose service while the
   reducer is blocked on that pause, then releases it. The decorator writes
   the exact group whose executor call returned success to
   `<sentinel>.trigger.json` before exposing
   the sentinel, so a retained failure artifact identifies the operation,
   Cypher, parameters, and prepared rows at the restart boundary.
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
   go/internal/reducer/shared_projection_runner_partition_error_test.go's
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
34. baseline-iam-can-assume (#6228 fault half) -- family-scoped fault-free baseline.
35. kill-worker-after-claim-iam-can-assume (#6228 fault half) -- lease reclaim proof.
36. fail-graph-write-once-then-succeed-iam-can-assume (#6228 fault half) -- CAN_ASSUME retry proof.
37. baseline-iam-can-perform (#6228 fault half) -- family-scoped fault-free baseline; fourth direct-materialization family in this gate.
38. kill-worker-after-claim-iam-can-perform (#6228 fault half) -- lease reclaim proof.
39. fail-graph-write-once-then-succeed-iam-can-perform (#6228 fault half) -- CAN_PERFORM retry proof.
40. baseline-ec2-uses-profile (#6228 fault half) -- family-scoped fault-free baseline; fifth direct-materialization family in this gate.
41. kill-worker-after-claim-ec2-uses-profile (#6228 fault half) -- lease reclaim proof.
42. fail-graph-write-once-then-succeed-ec2-uses-profile (#6228 fault half) -- USES_PROFILE retry proof.
43. baseline-s3-logs-to (#6228 fault half) -- family-scoped fault-free baseline; sixth direct-materialization family in this gate.
44. kill-worker-after-claim-s3-logs-to (#6228 fault half) -- lease reclaim proof.
45. fail-graph-write-once-then-succeed-s3-logs-to (#6228 fault half) -- LOGS_TO retry proof.
46. baseline-cloud-resource (#8358) -- family-scoped fault-free baseline; first predicate-scoped family in this gate.
47. kill-worker-after-claim-cloud-resource (#8358) -- lease reclaim proof (AWS handler targeted, full four-edge set asserted).
48. fail-graph-write-once-then-succeed-cloud-resource (#8358) -- AWS writer MERGE retry proof.
49. baseline-iam-escalation (#6228 fault half) -- family-scoped fault-free baseline; seventh direct-materialization family in this gate.
50. kill-worker-after-claim-iam-escalation (#6228 fault half) -- lease reclaim proof.
51. fail-graph-write-once-then-succeed-iam-escalation (#6228 fault half) -- CAN_ESCALATE_TO retry proof.
52. baseline-authenticates-vault-role (#6228 slice A) -- family-scoped fault-free baseline; eighth direct-materialization family in this gate.
53. kill-worker-after-claim-authenticates-vault-role (#6228 slice A) -- lease reclaim proof.
54. fail-graph-write-once-then-succeed-authenticates-vault-role (#6228 slice A) -- SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE retry proof.
55. baseline-uses-vault-policy (#6228 slice A + #8539 seam) -- family-scoped fault-free baseline; ninth direct-materialization family in this gate. No handler-stage kill cell: the shared secrets_iam_graph_projection handler wait is owned first-landed by authenticates_vault_role (entry 53, arbiter verdict Option B on #6228); per-statement kill/reclaim for uses_vault_policy is claimed by entry 56 through the hold seam (statement_hold, wait_stage=statement), not by a borrowed handler wait.
56. kill-statement-hold-uses-vault-policy (#8539 seam) -- per-statement hold kill/reclaim proof: the hold decorator parks the worker inside the family's own USES_VAULT_POLICY MERGE (post-retract, post-prior-edge-write), the cell rendezvouses on the claimed work item plus the hold marker, kill -9 lands while held, the replacement reducer restarts unfaulted, and the full three-edge set converges with zero dead letters against the entry 55 baseline digest.
57. fail-graph-write-once-then-succeed-uses-vault-policy (#6228 slice A) -- SECRETS_IAM_USES_VAULT_POLICY retry proof.
58. baseline-built-from (#6228 slice C live leaf) -- family-scoped fault-free baseline; tenth direct-materialization family in this gate.
59. kill-worker-after-claim-built-from (#6228 slice C live leaf) -- lease reclaim proof.
60. fail-graph-write-once-then-succeed-built-from (#6228 slice C live leaf) -- BUILT_FROM retry proof.
61. baseline-cloud-resource-container-image (#6228 slice D live leaf) -- family-scoped fault-free baseline; eleventh direct-materialization family in this gate.
62. kill-worker-after-claim-cloud-resource-container-image (#6228 slice D live leaf) -- lease reclaim proof.
63. fail-graph-write-once-then-succeed-cloud-resource-container-image (#6228 slice D live leaf) -- AWS_lambda_function_uses_image retry proof.
64. baseline-crossplane-satisfied-by (#6228 crossplane live leaf) -- family-scoped fault-free baseline; twelfth direct-materialization family in this gate.
65. kill-worker-after-claim-crossplane-satisfied-by (#6228 crossplane live leaf) -- lease reclaim proof.
66. fail-graph-write-once-then-succeed-crossplane-satisfied-by (#6228 crossplane live leaf) -- SATISFIED_BY retry proof.
67. baseline-derived-from (#6228 derived_from live leaf) -- family-scoped fault-free baseline; thirteenth direct-materialization family in this gate.
68. kill-statement-hold-derived-from (#6228 derived_from live leaf) -- DERIVED_FROM hold/reclaim proof (statement_hold: the hold decorator engages the family's own MERGE; shares the container_image_identity handler with built_from, which owns the handler-stage wait).
69. fail-graph-write-once-then-succeed-derived-from (#6228 derived_from live leaf) -- DERIVED_FROM retry proof.
70. baseline-code-interproc-evidence (#6228 code_interproc_evidence live leaf) -- family-scoped fault-free baseline; fourteenth direct-materialization family in this gate.
71. kill-worker-after-claim-code-interproc-evidence (#6228 code_interproc_evidence live leaf) -- lease reclaim proof.
72. fail-graph-write-once-then-succeed-code-interproc-evidence (#6228 code_interproc_evidence live leaf) -- TAINT_FLOWS_TO retry proof.
73. baseline-code-taint-evidence (#6228 code_taint_evidence live leaf) -- family-scoped fault-free baseline; fifteenth direct-materialization family in this gate.
74. kill-worker-after-claim-code-taint-evidence (#6228 code_taint_evidence live leaf) -- lease reclaim proof.
75. fail-graph-write-once-then-succeed-code-taint-evidence (#6228 code_taint_evidence live leaf) -- HAS_TAINT_EVIDENCE retry proof.
76. baseline-observability-coverage (#6228 observability_coverage live leaf) -- family-scoped fault-free baseline; sixteenth direct-materialization family in this gate.
77. kill-worker-after-claim-observability-coverage (#6228 observability_coverage live leaf) -- lease reclaim proof.
78. fail-graph-write-once-then-succeed-observability-coverage (#6228 observability_coverage live leaf) -- AWS_COVERS_* retry proof.
79. baseline-incident-routing-evidence (#6228 incident_routing_evidence live leaf) -- family-scoped fault-free baseline; seventeenth direct-materialization family in this gate.
80. kill-worker-after-claim-incident-routing-evidence (#6228 incident_routing_evidence live leaf) -- lease reclaim proof.
81. fail-graph-write-once-then-succeed-incident-routing-evidence (#6228 incident_routing_evidence live leaf) -- HAS_*_ROUTING retry proof.
82. baseline-publishes (#6228 publishes live leaf) -- family-scoped fault-free baseline; eighteenth direct-materialization family in this gate.
83. kill-worker-after-claim-publishes (#6228 publishes live leaf) -- lease reclaim proof.
84. fail-graph-write-once-then-succeed-publishes (#6228 publishes live leaf) -- PUBLISHES retry proof.
85. baseline-grants-secret-read (#6228 remainder + #8539 seam) -- family-scoped fault-free baseline; nineteenth direct-materialization family in this gate. No handler-stage kill cell: the shared secrets_iam_graph_projection handler wait is owned first-landed by authenticates_vault_role (entry 53, arbiter verdict Option B on #6228); per-statement kill/reclaim for grants_secret_read is claimed by entry 86 through the hold seam (statement_hold, wait_stage=statement), not by a borrowed handler wait.
86. kill-statement-hold-grants-secret-read (#8539 seam) -- per-statement hold kill/reclaim proof: the hold decorator parks the worker inside the family's own GRANTS_SECRET_READ MERGE (post-retract, post-prior-edge-write), the cell rendezvouses on the claimed work item plus the hold marker, kill -9 lands while held, the replacement reducer restarts unfaulted, and the full three-edge set converges with zero dead letters against the entry 85 baseline digest.
87. fail-graph-write-once-then-succeed-grants-secret-read (#6228 remainder) -- SECRETS_IAM_GRANTS_SECRET_READ retry proof.
88. baseline-inheritance (#5996) -- family-scoped fault-free baseline.
89. kill-worker-after-claim-inheritance (#5996) -- lease reclaim proof.
90. fail-graph-write-once-then-succeed-inheritance (#5996) -- retry proof.
91. baseline-shell-exec (#6001) -- family-scoped fault-free baseline.
92. kill-worker-after-claim-shell-exec (#6001) -- lease reclaim proof.
93. fail-graph-write-once-then-succeed-shell-exec (#6001) -- retry proof.
94. baseline-workload-dependency (#6003) -- family-scoped fault-free baseline.
95. kill-worker-after-claim-workload-dependency (#6003) -- lease reclaim proof.
96. fail-graph-write-once-then-succeed-workload-dependency (#6003) -- retry proof.
97. baseline-symbol-runtime (#5995/#6000/#5997) -- ONE baseline shared by
   handles_route/runs_in/invokes_cloud_action; no kill-worker cell (ifa_fault_injection_symbol_runtime_cells.sh's header explains why).
98. fail-graph-write-once-then-succeed-handles-route (#5995) -- HANDLES_ROUTE retry proof.
99. fail-graph-write-once-then-succeed-runs-in (#6000) -- RUNS_IN retry proof.
100. fail-graph-write-once-then-succeed-invokes-cloud-action (#5997) -- INVOKES_CLOUD_ACTION retry proof.

101. kill-worker-after-runner-lease-wait-handles-route (#6208) -- exact-key reclaim proof.
102. kill-worker-after-runner-lease-wait-runs-in (#6208) -- exact-key reclaim proof.
103. kill-worker-after-runner-lease-wait-invokes-cloud-action (#6208) -- exact-key reclaim proof.
