# Advisory group projection (#10411)

This note records the performance and observability proof behind the
`advisory_group_projection` reducer domain: per-pass writer cost, queue
behavior, and the operator signals. It covers the handler/enqueue lane;
the graph writer and winners reader carry their own live proofs.

## What changed on hot paths

- The supply-chain impact writer computes one extra change set per pass,
  `TouchedGroupIDs` (coalesced CVE-or-advisory group ids), from the rows
  and stored findings it already read. The stored-findings read gained
  one payload column (`advisory_id`); its row set, predicate, and plan
  shape are unchanged. A second recovery-only read
  (`listSupplyChainImpactGroupIDsQuery`) runs only on recovery writes,
  beside the existing recovery CVE read with the same scope/generation
  predicate. `ChangedCVEIDs`, `RecoveryCVEIDs`, `SuppressionCVEIDs`,
  `DerivedCVEIDs`, and the holder fan-out are byte-identical: the
  existing writer, fan-out, suppression, and scoped-pass suites pass
  unchanged.
- After the commit and before the holder fan-out, the handler admits
  one queue row per touched group (batched insert, 500 rows per
  statement via the existing `ReducerQueue.Enqueue`), plus one
  `RetouchWorkItem` update per group so a group touched twice in one
  generation re-projects (succeeded rows reopen, pending rows advance
  their cycle start, claimed rows carry the replay bit). Groups per
  pass are bounded by distinct changed CVEs/advisories, a set the
  fan-out already enumerates.
- The projection handler reads one group's qualifying winners (the
  coalesced group-key predicate served by migration 287's
  `supply_chain_impact_winners_group_key_idx` expression index over the
  winners columns, `ORDER BY repository_id, package_id`) and one
  watermark row per intent, then reconciles. Claims partition by entity
  key in the queue conflict fence, so disjoint groups drain in parallel
  and the same group never reconciles concurrently.

No-Regression Evidence: baseline `BenchmarkChangedCVEIDsBaseline`
(pre-#10411 change-set shape) 228270 ns/op, 631785 B/op, 31 allocs/op;
after `BenchmarkTouchedGroupIDs` 245943 ns/op, 631840 B/op, 31 allocs/op.
Backend/version: pure Go microbenchmarks, go1.26.10 linux/arm64, this
worktree host, 2026-11-09. Input shape: 1000 written rows against 1000
stored rows, all content-changed (worst case), one shared CVE/advisory
pair. Read: the touched set costs roughly one extra `changedCVEIDs`
linear pass (~0.25 ms per 1000 findings) inside the transaction the
writer already holds; no new statement runs outside recovery. Terminal
queue/row counts: live runs executed by the coordinator on 2026-11-09
against disposable postgres:18-alpine and the pinned
neo4j:2026-community (2026.08.1): the winners reader live proof
(`TestAdvisoryGroupWinnersQualifyingLive`, PASS 1.89 s), the writer live
reconcile proof (`TestBoltAdvisoryGroupReconcileIsIdempotentAndRetracts`
under -race, PASS 0.32 s), and the bidirectional seeded parity proof
(`TestAdvisoryGroupParityLive`, PASS 0.33 s) all green. PROFILE on the
same pinned Neo4j with 10,000 seeded Repository nodes: the upsert plans
MergeUniqueNode on `advisory_group_id` plus NodeUniqueIndexSeek on
Repository (500-edge batch: 368 ms, 4,519 db hits); the group-anchored
stale retract at 1,000-edge supernode degree runs Expand(All) from the
unique seek (990 stale edges deleted in 82 ms); no label scan on any
held shape. End-to-end drain time at repo scale remains NOT_MEASURED
and must be confirmed before any fleet claim.

Observability Evidence: eight new instruments in
`go/internal/telemetry/instruments_advisory_group_projection.go`
(`eshu_dp_advisory_group_projection_groups_total{domain,outcome}`,
`edges_upserted_total`, `edges_retracted_total`, `nodes_deleted_total`,
`member_union_oversized_total`, `enqueue_failures_total`,
`reconcile_duration_seconds`, `lag_seconds`), all bounded to domain and
a closed outcome; the watermark defer rides the enrolled non-counting
failure class `advisory_group_winners_watermark_not_ready` and
reclassifies to the counting `advisory_group_winners_watermark_stale`
past 30 minutes; counts-only INFO/WARN logs (no group or CVE ids).
Operator reference: `docs/public/reference/telemetry/advisory-group-projection.md`;
coverage row in `docs/public/observability/telemetry-coverage-reducer-domains.md`;
`scripts/verify-telemetry-coverage.sh` passes locally.

## Why the change is safe

- Accuracy: the handler never reconciles against winners older than the
  intent's enqueue/cycle start (watermark gate), so an ack cannot seal
  pre-pass state. Group building is deterministic (byte-sorted members,
  lowest-value edge on disagreement) and the writer reconcile is a
  replace-set, proven replay-identical by the reducer idempotency guard
  case.
- Concurrency: per-group conflict partitioning plus the writer's
  per-group reconcile keep disjoint groups parallel; the enqueue runs
  post-commit with no locks held and BEFORE the holder fan-out, so a
  failure aborts the pass while the writer-tx owed marker still stands
  and the retry re-enqueues the scope's recovery groups (composed
  regression TestGroupEnqueueFailureKeepsOwedMarkerForRetry). A
  same-generation re-touch retouches the existing work item: pending
  rows advance their cycle start, claimed rows carry the replay bit so
  the ACK reruns them (TestReducerQueueRetouchLive).
- Known limits: suppression expiry is evaluated at winners-read time,
  so a suppression that expires by wall clock un-hides winners with no
  write event; affected groups converge on their next touch, not at
  expiry. A group whose intent dead-letters in a
  generation stays stale until the next generation (a same-generation
  re-touch cannot revive a dead-lettered row); a failed
  enqueue fails the supply pass for retry, and the fan-out owed marker
  (which covers touched groups, not only changed CVEs) makes the owed
  rerun re-enqueue the scope's recovery groups.

Reader index proof (#10411, postgres:18-alpine shim, 220,000 seeded winner
rows in the reader's exact group-key shape). Before the index the planner
picked a Parallel Seq Scan (16.494 ms, 73,320 rows removed by filter per
worker); after `CREATE INDEX ... ((CASE WHEN btrim(cve_id) <> '' THEN
cve_id ELSE advisory_id END))` the same query is an Index Scan using the
group-key index (0.102 ms for a CVE key, 0.032 ms for an advisory-id key,
Buffers shared hit 40/6). The full production query adds the
suppression/scope heap filters and the fact join over only the group's
matched rows.

Criterion 9 completion (#10411). Built-binary queue drain: the production
reducer binary (built from this branch) refused to start graph writers
until the bootstrap-data-plane binary applied the graph schema marker
(the schema-fence ordering is enforced at boot, not assumed). After
bootstrap, with `ESHU_REDUCER_CLAIM_DOMAINS=advisory_group_projection`
against the same shims, the binary claimed the seeded intent, logged
`advisory group projection completed ... nodes_created=1 edges_created=1`,
`reducer execution succeeded` in 0.125 s, the queue row read
`succeeded` with attempt_count 1, and the graph readback returned
`test-agp-bin-repo | pkg:npm/bin`. The committed
`TestAdvisoryGroupQueueDrainLive` covers the same enqueue → claim →
handle → ack drain through the production queue in-process.
1x/10x read scaling (PROFILE, pinned neo4j): a 100-edge group reads at
DbHits 204 / 2 ms and a 1,000-edge group at DbHits 2004 / 10 ms —
linear in group degree from the unique-seek anchor; a paginated read
(SKIP 500 LIMIT 100) of the 1,000-edge group costs DbHits 3104 / 16 ms.
Concurrency: `TestBoltAdvisoryGroupParallelConvergence` (-race) races
disjoint groups plus duplicate same-group reconciles to the same final
graph with no duplicated edges; `TestAdvisoryGroupConflictPartition`
pins same-group/one-key across scopes.
