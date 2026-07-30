# #5238 — Account-alias rollout-gap warning signal: theory proof and local proof

## The defect this closes

Before this change, `GET /api/v0/cloud/inventory` and MCP
`list_cloud_resource_inventory`'s `account_id`/`project_id`/`subscription_id`
selectors made two very different situations byte-identical: "no such
account" and "this account's canonical rows predate the #5238 account_id
rollout and have not been re-admitted yet" (see the "Rollout window" note in
`docs/public/reference/http-api.md` and
`docs/internal/evidence/1997-1998-cloud-inventory-identity-admission.md`).
Both returned `resources: []`, the same hardcoded `truth.freshness.state`
(`FreshnessFresh`, `go/internal/query/contract.go:261`), and the same static
truth reason string. The distinction lived only in prose in a doc an operator
would have to already know to go read. That is exactly the "silently
wrong-looking result" class this repo forbids.

## The fix

A zero-result account-alias-filtered call now runs one additional bounded
Postgres check for whether any canonical `reducer_cloud_resource_identity` row
in the same provider/access scope predates the rollout (payload carries no
`account_id` key at all). The result surfaces as a `warning_flags` array on
the response body:

- `account_alias_rollout_gap` — a pre-rollout row exists in scope; the zero
  rows do not prove the account does not exist.
- `account_alias_rollout_gap_check_failed` — the disambiguation check itself
  errored; the primary (empty) result still stands.
- Absent — the check ran and found no pre-rollout row, so the zero rows are a
  genuine no-such-account/no-such-scope result.

Implementation: `go/internal/query/cloud_inventory_rollout_signal.go`
(`cloudInventoryRolloutGapWarningFlags`, `buildCloudInventoryPreRolloutProbeSQL`,
`(*ContentReader).cloudInventoryPreRolloutEvidenceExists`), wired into
`go/internal/query/cloud_inventory_readback.go`'s `listInventory` handler.

The probe is gated strictly to `filter.AccountAliasKey != "" &&
len(readModel.Resources) == 0` — the one case an operator cannot resolve from
the response alone. It never fires on the hot unfiltered path, a
`scope_id`-filtered call, or any account-alias-filtered call that already
returned rows.

## Prove-The-Theory-First: EXPLAIN ANALYZE proof

Per the root `CLAUDE.md`/`AGENTS.md` Prove-The-Theory-First gate, the query
shape was proven against representative data with `EXPLAIN (ANALYZE, BUFFERS)`
**before** it was wired into the handler.

### Environment

- `PostgreSQL 16` (`postgres:16` Docker image), a throwaway container private
  to this session (`docker run -d --name eshu-5238-proof-pg -e
  POSTGRES_PASSWORD=change-me -e POSTGRES_USER=eshu -e POSTGRES_DB=eshu -p
  25946:5432 postgres:16`), torn down after this proof.
- Full production schema applied via `postgres.ApplyBootstrap` (every
  migration under `go/internal/storage/postgres/migrations/`, including every
  real index) — not a hand-picked subset, so the planner sees the actual
  `fact_records_collector_status_active_idx` and friends production uses.
- Representative corpus: 200 active ingestion scopes (round-robin
  aws/gcp/azure), 500 `reducer_cloud_resource_identity` facts per scope
  (100,000 rows total, the fact kind under test), ~10% of scopes seeded
  pre-rollout-shaped (payload has no `account_id` key at all). `ANALYZE
  fact_records` run before measuring.
- Throwaway shim: `go/cmd/scratch5238proof/main.go` (deleted after this proof
  ran; not part of the shipped diff).

### Query shapes measured

1. **Baseline (unchanged)**: the existing primary `account_id`-filtered read
   (`buildCloudInventoryIdentitiesSQL`) against a value that matches zero
   rows — the case this feature activates for.
2. **New candidate, provider-scoped, early match**: the probe
   (`buildCloudInventoryPreRolloutProbeSQL`) for `provider=aws`, where a
   pre-rollout `aws` row exists early in scan order.
3. **New candidate, all-scopes (no provider)**: same probe with no provider
   filter.
4. **New candidate, true worst case**: the probe for a provider value that
   matches nothing anywhere in the corpus, forcing every one of the 200
   scopes to be visited (500 rows filtered each) before concluding `false`.
   This is the honest worst-case shape for the post-full-rollout steady
   state, where no pre-rollout row exists anywhere in scope.

### Output (captured to a file, exit code read from the file, never from a pipe)

```
cd go && PROOF_DSN="postgres://eshu:change-me@localhost:25946/eshu?sslmode=disable" \
  PROOF_SKIP_SEED=1 /tmp/scratch5238proof >/tmp/proof_output2.log 2>&1
echo $?
```

`echo $?` = `0`.

```
=== EXISTING primary query: account_id filter, zero-match case (baseline, unchanged) ===
Limit  (cost=29.13..29.14 rows=1 width=73) (actual time=15.016..15.018 rows=0 loops=1)
  Buffers: shared hit=8248
  ->  Sort  (cost=29.13..29.14 rows=1 width=73) (actual time=15.015..15.016 rows=0 loops=1)
        ->  Nested Loop  (cost=9.92..29.12 rows=1 width=73) (actual time=15.013..15.014 rows=0 loops=1)
              ->  Hash Join  (cost=9.50..18.55 rows=1 width=94) (actual time=0.035..0.102 rows=200 loops=1)
                    ->  Seq Scan on ingestion_scopes scope (200 rows)
                    ->  Hash  ->  Seq Scan on scope_generations generation (200 rows)
              ->  Index Scan using fact_records_collector_status_active_idx on fact_records
                    (actual time=0.074..0.074 rows=0 loops=200)
                    Filter: (provider = 'aws' AND account_id = '999999999999')
                    Rows Removed by Filter: 500
                    Buffers: shared hit=8238
Planning Time: 0.735 ms
Execution Time: 15.033 ms

=== NEW candidate: pre-rollout-evidence EXISTS probe (provider scoped) ===
Result (actual time=0.038..0.038 rows=1 loops=1)
  Buffers: shared hit=9
  InitPlan 1
    ->  Nested Loop (actual time=0.037..0.037 rows=1 loops=1)
          ->  Hash Join (rows=1 loops=1)
          ->  Index Scan using fact_records_collector_status_active_idx on fact_records
                (actual time=0.005..0.005 rows=1 loops=1)
                Filter: (NOT (payload ? 'account_id') AND provider = 'aws')
Planning Time: 0.667 ms
Execution Time: 0.049 ms

=== NEW candidate: pre-rollout-evidence EXISTS probe (all-scopes, no provider) ===
Result (actual time=0.048..0.049 rows=1 loops=1)
  Buffers: shared hit=9
Planning Time: 0.593 ms
Execution Time: 0.059 ms

=== WORST CASE: provider that matches nothing -- must exhaust every scope before returning false ===
Result (actual time=14.551..14.553 rows=1 loops=1)
  Buffers: shared hit=8248
  InitPlan 1
    ->  Nested Loop (actual time=14.550..14.552 rows=0 loops=1)
          ->  Hash Join (rows=200 loops=1)
          ->  Index Scan using fact_records_collector_status_active_idx on fact_records
                (actual time=0.072..0.072 rows=0 loops=200)
                Filter: (NOT (payload ? 'account_id') AND provider = 'doesnotexist')
                Rows Removed by Filter: 500
                Buffers: shared hit=8238
Planning Time: 0.565 ms
Execution Time: 14.566 ms
```

(Full unabridged `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)` output was captured
during the session; the excerpt above preserves every cost/timing/buffer line,
trimmed only of duplicate index-scan detail lines shown once above.)

### What the numbers prove

- **No sequential scan on `fact_records` anywhere.** Every plan resolves the
  probe through the same `fact_records_collector_status_active_idx` the
  existing primary query already uses — no new index required, no plan
  regression risk on a shared index.
- **Early-match cases are free**: 0.049ms / 0.059ms, 9 buffer hits. This is
  the common case while a rollout gap is fresh (a pre-rollout scope is found
  quickly because most active scopes were touched around the same deploy
  window).
- **The true worst case (steady state, no gap anywhere) costs about the same
  as the primary query's own already-accepted zero-result cost**: 14.566ms vs
  15.033ms, both ~8,248 buffer hits, both driven by the identical 200-scope ×
  500-row-per-scope shape. The probe never costs more than roughly doubling a
  cost the handler was already paying for the SAME zero-result outcome, and it
  shrinks toward zero as the transient rollout window closes (the collector's
  normal sync cadence naturally reaps pre-rollout rows over time — no forced
  re-sync is required, matching the documented rollout-window behavior).
- This is a **behavior addition** (a new signal for a case that previously had
  none), not a rewrite of existing behavior, so there is no old-vs-new
  equivalence check to run; the relevant proof is that the new query is
  bounded and index-driven at worst case, which the above shows.

## Mandatory Pre-PR Local Proof: focused Go tests

```
cd go && go test ./internal/query/... -run 'CloudInventory' -count=1
echo $?
```

`echo $?` = `0`.

Unit coverage (`go/internal/query/cloud_inventory_rollout_signal_test.go`,
sqlmock-backed via `openRecordingContentReaderDB` so the exact dispatched SQL
and query COUNT are asserted, not just response shape):

- `TestCloudInventoryHandlerAccountAliasZeroResultsWarnsOnPreRolloutGap` —
  zero-result alias filter + probe finds a gap → `warning_flags:
  [account_alias_rollout_gap]`, exactly 2 Postgres queries dispatched.
- `TestCloudInventoryHandlerAccountAliasZeroResultsWithoutPreRolloutGapStaysClean`
  — zero-result alias filter + probe finds no gap → no `warning_flags` key.
- `TestCloudInventoryHandlerAccountAliasNonEmptyResultsSkipsProbe` — alias
  filter already matched rows → exactly 1 Postgres query (probe never fires).
- `TestCloudInventoryHandlerUnfilteredZeroResultsSkipsProbe` — no alias filter,
  zero rows → exactly 1 Postgres query (probe never fires on the hot path).
- `TestCloudInventoryHandlerAccountAliasZeroResultsProbeErrorDegradesGracefully`
  — probe query errors → response stays `200` with `warning_flags:
  [account_alias_rollout_gap_check_failed]`, never a `500`.
- `TestBuildCloudInventoryPreRolloutProbeSQLScopesAccessGrantsLikePrimaryQuery`
  / `...AllScopesOmitsAccessPredicate` — the probe's access-scope predicate is
  byte-identical in shape to the primary query's.

A red-then-green check was run by temporarily replacing the handler's call to
`cloudInventoryRolloutGapWarningFlags` with a nil `warningFlags` and
re-running the two flag-asserting tests above; both failed for the expected
reason ("Postgres received 1 queries, want 2") before the revert.

Live-Postgres regression (`go/internal/query/cloud_inventory_rollout_signal_live_test.go`,
same throwaway-container pattern as the branch's existing live proofs):

```
cd go && ESHU_POSTGRES_TEST_DSN="postgres://eshu:change-me@localhost:25946/eshu?sslmode=disable" \
  go test ./internal/query/... -run \
  'TestCloudInventoryAccountIDMatchesExactScopeIDLive|TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive|TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive|TestCloudInventoryPreFixPayloadRolloutWindowLive|TestCloudInventoryAccountAliasCrossProviderIsolationLive|TestCloudInventoryPreRolloutEvidenceExistsLive' \
  -count=1 -v
echo $?
```

```
=== RUN   TestCloudInventoryAccountIDMatchesExactScopeIDLive
seeded 5 scopes / 11 canonical identity facts across aws/gcp/azure
--- PASS: TestCloudInventoryAccountIDMatchesExactScopeIDLive (0.03s)
=== RUN   TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive
seeded 5 scopes / 11 canonical identity facts across aws/gcp/azure
=== RUN   TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/gcp_project_id
=== RUN   TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/azure_subscription_id
--- PASS: TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive (0.03s)
    --- PASS: TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/gcp_project_id (0.00s)
    --- PASS: TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/azure_subscription_id (0.00s)
=== RUN   TestCloudInventoryAccountAliasCrossProviderIsolationLive
--- PASS: TestCloudInventoryAccountAliasCrossProviderIsolationLive (0.02s)
=== RUN   TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive
--- PASS: TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive (0.02s)
=== RUN   TestCloudInventoryPreRolloutEvidenceExistsLive
--- PASS: TestCloudInventoryPreRolloutEvidenceExistsLive (0.02s)
=== RUN   TestCloudInventoryPreFixPayloadRolloutWindowLive
--- PASS: TestCloudInventoryPreFixPayloadRolloutWindowLive (0.02s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/query	1.024s
```

`echo $?` = `0`.

`TestCloudInventoryPreRolloutEvidenceExistsLive` proves the real Postgres
jsonb `?` key-exists semantics against three real scopes: a pre-fix `aws` row
is found (probe `true`) for `provider=aws`, no `gcp` row in the corpus is
pre-fix so the `provider=gcp` probe returns `false`, and a caller granted only
the post-fix `aws` scope cannot see the pre-fix sibling scope's gap (access
scoping matches the primary query exactly).

This branch's `ESHU_POSTGRES_TEST_DSN`-gated live tests (now five) still skip
under `.github/` where that DSN is never set — disclosed, matching existing
repo precedent, not blocking (see the sibling doc
`5238-live-proof-corpus-gap.md`).

## No-Regression Evidence:

This change adds one new bounded, conditionally-fired Postgres query
(`buildCloudInventoryPreRolloutProbeSQL`) behind
`go/internal/query/cloud_inventory_rollout_signal.go`; it does not modify the
existing `buildCloudInventoryIdentitiesSQL` primary query, any index, or any
schema. The new query resolves through the SAME existing
`fact_records_collector_status_active_idx` the primary query already uses (see
the EXPLAIN ANALYZE proof above) — no new index, no migration. It fires ONLY
when `filter.AccountAliasKey != "" && len(readModel.Resources) == 0`, so the
hot unfiltered path, the `scope_id`-filtered path, and any alias-filtered call
that already returns rows are all byte-identical in query count and timing to
before this change (proven by
`TestCloudInventoryHandlerAccountAliasNonEmptyResultsSkipsProbe` and
`TestCloudInventoryHandlerUnfilteredZeroResultsSkipsProbe`, both asserting
exactly 1 dispatched Postgres query). In the one case it does fire, its
measured worst-case cost is bounded and comparable to a cost the handler was
already paying for the same zero-result outcome (~14.5ms vs ~15ms on the same
representative corpus), not an unbounded or quadratic addition.

## No-Observability-Change:

This change reuses the exact `cr.tracer.Start(ctx, "postgres.query", ...)`
OTEL span pattern `cloudInventoryIdentities` already uses in the same file,
with its own `db.operation` attribute value
(`cloud_inventory_pre_rollout_evidence_exists`) and `span.RecordError` on
failure — the same span mechanism, not a new metric, log, or runtime knob. The
existing `SpanQueryCloudInventoryReadback` handler span is unchanged.
