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

**Sustained-invalid-polling amplification (advisory, accepted tradeoff):** a
caller that polls a genuinely nonexistent `account_id`/`project_id`/
`subscription_id` in a loop pays the full worst-case probe cost
(~14.5ms in the corpus above) on every single call forever, because
`len(Resources) == 0` stays true for that caller on every poll — there is no
per-caller backoff or negative-result caching. This is a bounded, known cost
(not unbounded or quadratic) and the gating that gets us here is otherwise
correct, so it is accepted as-is rather than adding caching complexity
speculatively; revisit only if a real monitoring workload is observed doing
this.

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

### Review finding: false positive on a genuine GCP org-level asset

A hostile re-review asked whether a legitimate GCP org- or folder-level asset
(which genuinely has no project, by design) could look like a pre-rollout row
and produce a **false** `account_alias_rollout_gap` -- a differently-wrong
signal, no better than the ambiguity this change fixes. It does not, and the
reason is a specific Go/JSON invariant: `cloudInventoryAdmissionBasePayload`
(`go/internal/reducer/cloud_inventory_admission_writer.go`) writes
`"account_id"` into a `map[string]any` **unconditionally**, and
`encoding/json` always emits a map key regardless of an empty value -- there
is no implicit `omitempty` for maps. Confirmed live:
`'{"account_id":""}'::jsonb ? 'account_id'` → `t`,
`'{}'::jsonb ? 'account_id'` → `f`. So a genuine org-level asset's payload has
the key present with a blank value, and the probe's `?` key-EXISTENCE
predicate (never a value comparison) correctly excludes it. AWS and Azure
cannot even reach the ambiguous shape: both identifiers are decode-rejected
if absent from the source fact.

That invariant is the whole argument, and two tests now pin it directly so a
future refactor cannot silently reopen this class of false positive:

- `TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider`
  (`go/internal/reducer/cloud_inventory_admission_account_id_test.go`) gained
  a `{gcp, ""}` case, plus an explicit
  `if _, ok := decoded["account_id"]; !ok { t.Fatalf(...) }` key-presence
  assertion (not just a value-equality check) on the JSON-round-tripped
  payload.
- `TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive`
  (`go/internal/query/cloud_inventory_gcp_org_level_rollout_signal_live_test.go`)
  seeds the real present-but-blank shape (reusing
  `seedCloudInventoryGCPOrgLevelAssetLiveCorpus`), issues a
  `project_id`-filtered `GET /api/v0/cloud/inventory` through the actual HTTP
  handler (not `cloudInventoryIdentities` directly), and asserts
  `warning_flags` is absent.

Both were proven RED before GREEN, not just written and left green:

**Unit test RED** (production `cloudInventoryAdmissionBasePayload` temporarily
changed to only set `account_id` `if strings.TrimSpace(resource.AccountID) !=
""`, simulating an `omitempty`-style refactor):

```
cd go && go test ./internal/reducer/... -run 'TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider' -count=1 -v
echo $?
```

```
=== RUN   TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider
    cloud_inventory_admission_account_id_test.go:126: gcp payload[account_id] = <nil>, want ""
--- FAIL: TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider (0.00s)
FAIL
FAIL	github.com/eshu-hq/eshu/go/internal/reducer	0.949s
```

`echo $?` = `1` (reported as `FAIL` in the `go test` summary; the guard was
reverted immediately after, restoring the unconditional assignment).

**Unit test GREEN** (after revert):

```
--- PASS: TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider (0.00s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/reducer	1.459s
```

`echo $?` = `0`.

**Live test RED** (the shared fixture `seedCloudInventoryGCPOrgLevelAssetLiveCorpus`
in `cloud_inventory_gcp_project_gap_live_test.go` temporarily edited to omit
the `"account_id"` key entirely -- the true pre-fix shape -- instead of
writing it blank, to prove the test's assertion actually fires on a genuine
gap rather than trivially passing regardless of probe correctness):

```
cd go && ESHU_POSTGRES_TEST_DSN="postgres://eshu:change-me@localhost:25961/eshu?sslmode=disable" \
  go test ./internal/query/... -run 'TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive|TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive' -count=1 -v
echo $?
```

```
=== RUN   TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive
    cloud_inventory_gcp_org_level_rollout_signal_live_test.go:64: warning_flags = []interface {}{"account_alias_rollout_gap"}, want absent -- the only gcp row in scope has account_id present-but-blank (a genuine org-level asset), which must never be mistaken for a pre-#5238 row with no account_id key at all
--- FAIL: TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive (0.04s)
=== RUN   TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive
--- PASS: TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive (0.05s)
FAIL
FAIL	github.com/eshu-hq/eshu/go/internal/query
```

`echo $?` = `1`. This proves the new test correctly detects a real pre-fix gap
(the sibling test, which does not depend on `account_id` presence, is
unaffected) -- not just that it happens to pass when nothing exercises it.
The fixture was reverted immediately after.

**Live test GREEN** (after revert):

```
=== RUN   TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive
--- PASS: TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive (0.69s)
=== RUN   TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive
--- PASS: TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive (0.10s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/query	1.643s
```

`echo $?` = `0`.

Live-Postgres regression, full combined run (`go/internal/query/cloud_inventory_rollout_signal_live_test.go`,
`cloud_inventory_gcp_org_level_rollout_signal_live_test.go`, same throwaway-container
pattern as the branch's existing live proofs):

```
cd go && ESHU_POSTGRES_TEST_DSN="postgres://eshu:change-me@localhost:25961/eshu?sslmode=disable" \
  go test ./internal/query/... -run \
  'TestCloudInventoryAccountIDMatchesExactScopeIDLive|TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive|TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive|TestCloudInventoryPreFixPayloadRolloutWindowLive|TestCloudInventoryAccountAliasCrossProviderIsolationLive|TestCloudInventoryPreRolloutEvidenceExistsLive|TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive' \
  -count=1 -v
echo $?
```

```
=== RUN   TestCloudInventoryAccountIDMatchesExactScopeIDLive
seeded 5 scopes / 11 canonical identity facts across aws/gcp/azure
--- PASS: TestCloudInventoryAccountIDMatchesExactScopeIDLive (0.08s)
=== RUN   TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive
seeded 5 scopes / 11 canonical identity facts across aws/gcp/azure
=== RUN   TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/gcp_project_id
=== RUN   TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/azure_subscription_id
--- PASS: TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive (0.05s)
    --- PASS: TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/gcp_project_id (0.00s)
    --- PASS: TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive/azure_subscription_id (0.00s)
=== RUN   TestCloudInventoryAccountAliasCrossProviderIsolationLive
--- PASS: TestCloudInventoryAccountAliasCrossProviderIsolationLive (0.03s)
=== RUN   TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive
--- PASS: TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive (0.03s)
=== RUN   TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive
--- PASS: TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive (0.03s)
=== RUN   TestCloudInventoryPreRolloutEvidenceExistsLive
--- PASS: TestCloudInventoryPreRolloutEvidenceExistsLive (0.04s)
=== RUN   TestCloudInventoryPreFixPayloadRolloutWindowLive
--- PASS: TestCloudInventoryPreFixPayloadRolloutWindowLive (0.03s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/query	1.162s
```

`echo $?` = `0`.

`TestCloudInventoryPreRolloutEvidenceExistsLive` proves the real Postgres
jsonb `?` key-exists semantics against three real scopes: a pre-fix `aws` row
is found (probe `true`) for `provider=aws`, no `gcp` row in the corpus is
pre-fix so the `provider=gcp` probe returns `false`, and a caller granted only
the post-fix `aws` scope cannot see the pre-fix sibling scope's gap (access
scoping matches the primary query exactly).
`TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive` proves
the false-positive guard above through the real HTTP handler.

This branch's `ESHU_POSTGRES_TEST_DSN`-gated live tests (now seven) still skip
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

## PR #5881 review follow-up (two P1, two P2)

A hostile re-review of the branch found four legitimate defects, one showing
a prior commit did less than its own message claimed. All four were fixed
with a failing test first.

### P1 — the cross-provider fix required SOME provider, not the MATCHING one

The commit "require provider alongside an account alias to prevent
cross-provider collisions" only checked `provider != ""`, not that the
provider matched the alias used. `provider=gcp&account_id=123` was still
accepted and could resolve against the GCP row whose normalized
`account_id` happened to equal `123`, even though `account_id` is documented
as the AWS-specific alias — the same collision class, reached through a
different door.

Fix: `cloudInventoryAccountAliasRequiredProviders` (`cloud_inventory_readback.go`)
maps each alias to its one documented provider (`account_id`→`aws`,
`project_id`→`gcp`, `subscription_id`→`azure`); `filterFromRequest` rejects
any mismatch as `invalid_argument`. OpenAPI (`openapi_paths_cloud_inventory.go`),
the MCP tool description (`cloud/inventory_tools.go`), and `http-api.md` were
updated in lockstep.

Proof: `TestCloudInventoryHandlerAccountAliasProviderMismatchRejected` covers
all six mismatched pairs (`account_id`+`gcp`, `account_id`+`azure`,
`project_id`+`aws`, `project_id`+`azure`, `subscription_id`+`aws`,
`subscription_id`+`gcp`). RED (before the fix): all six returned `500`
(request reached the store instead of being rejected). GREEN (after): all
six return `400 invalid_argument` naming both the alias and the mismatched
provider; the existing matching-provider tests
(`TestCloudInventoryHandlerAccountAliasWithProviderStillWorks`,
`TestCloudInventoryHandlerGCPProjectIDAndAzureSubscriptionIDDispatchCanonicalPayloadMatch`)
stayed green throughout, proving no regression to the documented shape.

```
cd go && go test ./internal/query/... -run 'TestCloudInventoryHandlerAccountAliasProviderMismatchRejected|TestCloudInventoryHandlerAccountAliasWithProviderStillWorks|TestCloudInventoryHandlerAccountAliasWithoutProviderRejected|TestCloudInventoryHandlerGCPProjectIDAndAzureSubscriptionIDDispatchCanonicalPayloadMatch' -count=1
echo $?
```

`echo $?` = `0`.

### P1 — route AWS/Azure identity decode through the typed factschema contract

`cloudInventoryRecordFromRow` resolved `account_id`/`subscription_id` via a
raw `map[string]any` lookup plus `coerceJSONString`, which tolerates shapes
the typed contract rejects: a missing key coerces to `""`, a JSON number
coerces to its `fmt.Sprint` form, a JSON bool coerces to `"true"`/`"false"`.
That weakens the exact claim the rollout-gap signal's correctness argument
rests on -- that AWS `account_id` and Azure `subscription_id` are
structurally impossible to admit blank, because both are REQUIRED fields on
their typed `sdk/go/factschema/{aws,azure}/v1` structs.

Fix: new `go/internal/storage/postgres/cloud_inventory_identity_decode.go` adds
`decodeAWSResourceForCloudInventory` / `decodeAzureCloudResourceForCloudInventory`,
wrapping `factschema.DecodeAWSResource` / `factschema.DecodeAzureCloudResource`
via the same `postgresFactschemaEnvelope`/`newPostgresFactDecodeError` pattern
`secrets_iam_trust_chain_anchor_decode.go` already established for this
package. `cloudInventoryResolveAccountID` dispatches AWS/Azure through this
seam and rejects the row (drops it, matching every other malformed-row
outcome this loader already has) when decode fails; GCP's `project_id` stays
on the raw lookup because it is genuinely OPTIONAL in its typed contract (an
org/folder-level asset has none). The typed decode dispatches at schema major
"1" (`postgresDefaultSchemaMajorVersion`) rather than reading
`fact_records.schema_version` (not selected by this loader's SQL): both
collectors emit only major 1 today, and this loader already hardcodes v1
payload key names throughout, so this adds no new versioning assumption.

**File naming, deliberately not `factschema_decode*.go`.** That glob is what
`go/internal/payloadusage`'s gate-2 payload-usage manifest uses
(`ParseDecodeSeamsGlob`, `LoaderDir = go/internal/storage/postgres`) to
discover new `decode<Kind>` seams. `aws_resource` and `azure_cloud_resource`
already have a canonical reducer-side seam (`decodeAWSResource`,
`decodeAzureCloudResource` in `go/internal/reducer/factschema_decode*.go`)
that already gates `account_id`/`subscription_id` as required, declared
fields. First attempt named this file `factschema_decode_cloud_inventory_
evidence.go`, matching `factschema_decode_cloud_tag_evidence.go`'s
convention; that broke `TestRunGenerateAgainstRealRepoProducesNonTrivial
Manifest` (`len(Kinds) = 126, want 124`) and `TestLoadCoversWiredAzureKinds`
(`FactKindAzureCloudResource DecodeFunc = "decodeAzureCloudResourceForCloud
Inventory", want "decodeAzureCloudResource"`): `BuildManifest`
(`go/internal/payloadusage/manifest.go`) emits one `KindManifest` per
`DecodeSeam`, keyed by `FuncName`, not deduplicated by `FactKind` -- so a
second, differently-named seam for an ALREADY-seamed fact kind produces two
manifest entries sharing one `FactKind` constant, which
`TestLoadCoversWiredAzureKinds`'s single-entry-per-`FactKind` `byKind` map
cannot disambiguate deterministically. Renaming the file outside the glob
avoids the collision without losing any real coverage, and mirrors the
established precedent: `secrets_iam_trust_chain_anchor_decode.go` decodes
several fact kinds already seamed by the reducer (e.g.
`facts.AWSIAMPrincipalFactKind`, seamed by
`go/internal/reducer/factschema_decode.go`'s `decodeAWSIAMPrincipal`) the
same way, through `factschema.Decode*` directly, in a file intentionally
outside the `factschema_decode*.go` glob.

**Was the raw path deliberate?** No. The removed comment said this predated
account_id extraction and was "pre-existing debt, not something this change
introduces" -- it was accidental, not a measured performance tradeoff. No
performance defense is needed; this is a correctness fix.

Proof: `TestPostgresCloudInventoryEvidenceLoaderRejectsMalformedRequiredIdentityForAWSAndAzure`
seeds four malformed rows (AWS missing `account_id`, AWS `account_id` as a
JSON number, Azure missing `subscription_id`, Azure `subscription_id` as a
JSON bool) with every OTHER required field present, isolating the identity
field as the only possible cause. RED (before the fix) showed the exact
silent coercions this closes: `AccountID:"1.23456789012e+11"` (a stringified
float) and `AccountID:"true"` (a stringified bool), plus the two
missing-field rows silently admitted with `AccountID:""`. GREEN (after)
drops all four rows (`len(records) == 0`).

```
=== RUN   TestPostgresCloudInventoryEvidenceLoaderRejectsMalformedRequiredIdentityForAWSAndAzure
    cloud_inventory_evidence_account_id_test.go:157: len(records) = 4, want 0 (every row is malformed and must be dropped, not coerced); records = []reducer.CloudInventoryRecord{reducer.CloudInventoryRecord{Provider:"aws", ..., AccountID:"", ...}, reducer.CloudInventoryRecord{Provider:"aws", ..., AccountID:"1.23456789012e+11", ...}, reducer.CloudInventoryRecord{Provider:"azure", ..., AccountID:"", ...}, reducer.CloudInventoryRecord{Provider:"azure", ..., AccountID:"true", ...}}
--- FAIL: TestPostgresCloudInventoryEvidenceLoaderRejectsMalformedRequiredIdentityForAWSAndAzure (0.00s)
FAIL
```

```
cd go && go test ./internal/storage/postgres/... -run 'CloudInventory' -count=1
echo $?
```

`echo $?` = `0` (after the fix; full `CloudInventory`-matched suite, including
every other AWS/Azure fixture in this package updated to carry the complete
required-field set -- `resource_id`/`region` for AWS, `location` for Azure --
so the typed decode's stricter validation does not itself regress any
existing positive-path test).

Micro-benchmark (`BenchmarkCloudInventoryRecordFromRowAWSAllowlist`,
`-benchtime=100x`, isolated by temporarily reverting to the pre-fix code and
re-running the identical benchmark on the same machine): AWS
`cloudInventoryRecordFromRow` cost rose from 5987 ns/op (3696 B/op, 90
allocs/op) to 6810 ns/op (4192 B/op, 95 allocs/op) -- roughly +14%, the
direct cost of the typed decode's additional required-field validation. GCP's
`BenchmarkCloudInventoryRecordFromRowGCPPassthrough` was unaffected (2676 vs
2668 ns/op, within noise), confirming the change is scoped to AWS/Azure. This
loader runs once per source-fact row during reducer admission, not on a live
HTTP read path, so this is an accepted, bounded, honestly-measured cost of
correctness, not a regression requiring mitigation.

### P2 — the probe ignored `management_origin` (false-positive source)

`buildCloudInventoryPreRolloutProbeSQL` applied `provider` and the
access-scope predicate but never `management_origin`, so a
`management_origin=declared`-filtered alias query that matched zero rows
could still warn `account_alias_rollout_gap` because of an unrelated
`management_origin=observed` pre-fix row in the same provider/access scope --
a FALSE warning, the same failure class the GCP org-level guard closed for a
different filter.

Fix: `buildCloudInventoryPreRolloutProbeSQL` now applies the identical
`fact_records.payload->>'management_origin' = $N` predicate the primary query
does, whenever `filter.ManagementOrigin` is set.

Proof: `TestCloudInventoryHandlerManagementOriginFilteredAliasQueryDoesNotFalselyWarnLive`
(new, in `cloud_inventory_rollout_signal_live_test.go`) reuses the existing
`seedCloudInventoryRolloutSignalLiveCorpus` fixture, whose aws pre-fix row
carries `management_origin=observed`. A
`provider=aws&account_id=<no-such-account>&management_origin=declared`
request through the real HTTP handler matches zero primary rows either way;
RED (before the fix) showed the probe still warning because it ignored
`management_origin` and matched the observed-origin pre-fix row anyway.

```
=== RUN   TestCloudInventoryHandlerManagementOriginFilteredAliasQueryDoesNotFalselyWarnLive
    cloud_inventory_rollout_signal_live_test.go:249: warning_flags = []interface {}{"account_alias_rollout_gap"}, want absent -- the only pre-fix row in scope carries management_origin=observed, not declared, so a probe that correctly scopes by management_origin must not match it
--- FAIL: TestCloudInventoryHandlerManagementOriginFilteredAliasQueryDoesNotFalselyWarnLive (0.81s)
FAIL
```

GREEN (after):

```
=== RUN   TestCloudInventoryHandlerManagementOriginFilteredAliasQueryDoesNotFalselyWarnLive
--- PASS: TestCloudInventoryHandlerManagementOriginFilteredAliasQueryDoesNotFalselyWarnLive (0.56s)
PASS
```

`echo $?` = `0` for both the isolated run and the full live suite (below).

### P2 — the probe fired on non-initial pages (false-positive source)

With a stale or out-of-range non-zero cursor, an empty page means the
requested offset ran past the available rows, not that the account may be
missing -- but `cloudInventoryRolloutGapWarningFlags` fired the probe on any
zero-result alias-filtered page regardless of offset.

Fix: gated to `filter.Offset != 0` in addition to the existing
`AccountAliasKey`/zero-results conditions.

Proof: `TestCloudInventoryHandlerAccountAliasNonInitialPageSkipsProbe`
(sqlmock-recording, asserting exact dispatched query count) issues
`?provider=aws&account_id=...&cursor=50` against a zero-row primary response.
RED (before the fix):

```
=== RUN   TestCloudInventoryHandlerAccountAliasNonInitialPageSkipsProbe
    cloud_inventory_rollout_signal_test.go:190: Postgres received 2 queries, want 1 (non-zero cursor, probe must not fire); queries = [...]
--- FAIL: TestCloudInventoryHandlerAccountAliasNonInitialPageSkipsProbe (0.00s)
FAIL
```

GREEN (after):

```
=== RUN   TestCloudInventoryHandlerAccountAliasNonInitialPageSkipsProbe
--- PASS: TestCloudInventoryHandlerAccountAliasNonInitialPageSkipsProbe (0.00s)
PASS
```

### Combined verification (postdates every fix above)

```
cd go && go test ./internal/query/... ./internal/storage/postgres/... ./internal/mcp/... ./cmd/api ./cmd/mcp-server -count=1
echo $?
```

`echo $?` = `0`.

```
cd go && ESHU_POSTGRES_TEST_DSN="postgres://eshu:change-me@localhost:25971/eshu?sslmode=disable" \
  go test ./internal/query/... -run \
  'TestCloudInventoryAccountIDMatchesExactScopeIDLive|TestCloudInventoryGCPAndAzureAccountAliasMatchExactScopeIDLive|TestCloudInventoryGCPOrgLevelAssetExcludedFromProjectIDButVisibleUnscopedLive|TestCloudInventoryPreFixPayloadRolloutWindowLive|TestCloudInventoryAccountAliasCrossProviderIsolationLive|TestCloudInventoryPreRolloutEvidenceExistsLive|TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive|TestCloudInventoryHandlerManagementOriginFilteredAliasQueryDoesNotFalselyWarnLive' \
  -count=1
echo $?
```

`echo $?` = `0` (all eight `ESHU_POSTGRES_TEST_DSN`-gated live tests pass;
they still skip visibly under `.github/` where that DSN is never set, per
existing disclosed precedent).

```
cd go && golangci-lint run ./internal/query/... ./internal/storage/postgres/... ./internal/mcp/...
```

`0 issues.`

```
bash scripts/verify-openapi.sh
```

`OpenAPI surface clean: 253 HandleFunc routes, 253 OpenAPI path entries`

```
cd go && go test ./cmd/golden-corpus-gate/... -count=1
```

`ok` (this round did not touch the golden snapshot; re-run anyway as a sanity
check per the mandatory post-snapshot-edit rule).

```
bash scripts/verify-payload-usage-manifest.sh
```

`ok  	github.com/eshu-hq/eshu/go/internal/reducer` (green after the file rename
above; also independently confirmed via
`cd go && go test ./cmd/payload-usage-manifest/... ./internal/payloadusage/... ./internal/reducer/... -run PayloadUsage -count=1`,
all `ok`).

```
cd go && go test ./... -covermode=count -coverprofile=go-code-coverage.out
```

Full-module run for `scripts/generate-code-coverage-report.sh` (required
because this round added one non-test `.go` file,
`cloud_inventory_identity_decode.go`). One pre-existing, unrelated failure
observed and disregarded per known precedent:
`TestRepoDependencyProjectionRunnerQuarantinesHeartbeatLossBeforeSuccess`
(documented macOS timing flake, not a regression introduced here). No other
package failed in that run once the payload-usage-manifest fix above landed.
`docs/public/reference/code-coverage.md` and `code-coverage-shield.json` were
regenerated and committed in the same commit as the code changes, per the
project rule that a coverage-report commit landing after `make pre-pr`
invalidates the per-SHA stamp.
