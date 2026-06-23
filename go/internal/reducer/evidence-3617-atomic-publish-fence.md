# Evidence: Atomic Publish Fence For Cross-Repo Resolution (#3617)

Scope: `cross_repo_resolution.go` (`CrossRepoRelationshipHandler.Resolve`
publish/acceptance ordering on both the main and empty-evidence paths, plus the
`recordActivationFenced` operator signal) and the `CrossRepoActivationFenced`
counter in `go/internal/telemetry/instruments.go`. This is the prevention
follow-up to the #3559/#3616 reconciliation cure: it closes the
`UpsertIntents`-after-`ActivateResolutionGeneration` window at the source so a
generation cannot be published with non-durable graph-edge acceptance intents.

## Failure window closed

Before this change, `Resolve` ran, in order: `UpsertCandidates` →
`UpsertResolved` → `ActivateResolutionGeneration` (publish to the
repo-dependency surface) → `UpsertIntents` (durable graph-edge acceptance
intents). If `UpsertIntents` failed after activation committed, the generation
was authoritative/published while its denormalized graph edges
(`confidence`/`generation_id`/`resolved_id`) were not durably accepted —
stranded inconsistent edges. The empty-evidence tombstone path had the same
shape: activate, then write retract intents.

The fix reorders both paths so the durable graph-acceptance intents
(`UpsertIntents`) commit BEFORE `ActivateResolutionGeneration`. Activation is now
the fence: a generation cannot become authoritative/published unless its
graph-acceptance intents are already durable.

## Conflict domain and scopes

- Conflict domain: the `relationship_generations` row for `(scope,
  generation_id)` (the authoritative publish flip) and the
  `shared_projection_intents` rows for that `generation_id`. Activation is the
  single authoritative-flip statement and is now strictly last.
- Transaction scope: each store call is its own statement/transaction; the
  handler does not wrap them in one transaction, so the ordering is a
  commit-ordering fence, not a shared-transaction atomic. The reorder makes
  activation strictly depend on a prior durable acceptance commit.
- Retry scope: the whole `Resolve` for a generation, re-driven by the reducer.
  All writes are idempotent under retry: `UpsertIntents` uses
  `ON CONFLICT (intent_id) DO UPDATE`; `ActivateResolutionGeneration` is an
  idempotent `INSERT ... ON CONFLICT (generation_id) DO UPDATE SET
  status='active'`; `UpsertCandidates`/`UpsertResolved` use `ON CONFLICT DO
  NOTHING`. A retry after an acceptance failure commits acceptance once and
  publishes exactly once — no double-publish.

## Graph-ahead window closed (codex P1)

Reordering acceptance before activation alone only moved the inconsistency: the
repo-dependency projection runner derives graph-projection authority from
`shared_projection_acceptance` (via `FilterAuthoritativeIntents` /
`AcceptedGenerationLookup`), NOT from `relationship_generations.status`. The
handler's `IntentWriter` (`SharedIntentAcceptanceWriter`) commits the intents AND
their acceptance rows atomically, so once acceptance commits the graph runner can
project edges for a generation that activation has not yet published — graph
ahead of the Postgres relationship read models (which filter on
`g.status = 'active'`). That is exactly the `graph-ahead` dual-write class
#3559 names.

The root-cause fix gates repo-dependency graph-projection authority on the
relationship generation being active. `RelationshipStore.IsGenerationActive` (a
primary-key lookup on `relationship_generations`) is adapted via
`postgres.NewRelationshipGenerationActiveLookup` and composed over the runner's
`AcceptedGen`/`AcceptedGenPrefetch` with `reducer.GateAcceptedGenerationOnActive`
/ `GateAcceptedGenerationPrefetchOnActive`. The gate returns "not authoritative"
(defer) whenever the accepted generation is not yet active, the active check
errors, or there is no acceptance row. This makes activation the single fence
that opens BOTH surfaces together:

- Acceptance commits first, but the graph runner defers (generation not active)
  → no graph-ahead.
- Activation commits → graph authority (acceptance present AND active) and the
  Postgres read models open together.
- Activation fails after acceptance → graph still defers (not active) AND the
  Postgres read models are unpublished → neither surface advanced; the retry
  re-runs `Resolve`, idempotently re-commits acceptance, activates, and
  converges. No graph-ahead, no graph-behind.

The gate is scoped to the repo-dependency lane only — `code_call` and other
shared-projection lanes keep the unfenced `AcceptedGenerationLookup` because they
do not have a `relationship_generations` row. The #3559/#3616 reconciler stays as
defense-in-depth for any residual drift.

## Measurement and safety

No-Regression Evidence: this change reorders already-existing database statements
within `Resolve`, adds one primary-key generation-active lookup
(`relationship_generations` by `generation_id`) used to gate repo-dependency
graph-projection authority, and adds one `Int64` counter plus one warn-log
emitter. It adds no new Cypher, no new graph write, no new query shape, no extra
round trip on the resolve path, no worker/lease/batch change, and no new hot-path
scan. The same `UpsertIntents` batch (`ON CONFLICT (intent_id)` multi-row upsert)
and the same idempotent activation statement run; only their order changes, so
the per-generation work and cost are unchanged. The new authority gate adds one
bounded PK lookup per accepted generation on the repo-dependency selection path
(memoizable, returns at most one row), not a scan. Backend/version:
backend-neutral (Postgres acceptance + activation statements unchanged;
NornicDB/Neo4j untouched). Input shape: per generation, one
candidate/resolved/intent set bounded by the caller's resolved-edge count, same
as before. Measurement: `go test ./internal/reducer ./internal/storage/postgres
./internal/storage/cypher ./internal/telemetry ./cmd/reducer -count=1 -race`
→ 4148 passed, including the four publish-fence tests
(`cross_repo_resolution_fence_test.go`), the five activation-gate tests
(`accepted_generation_active_gate_test.go`: defer-until-active, missing-acceptance
pass-through, defer-on-error, prefetch defer-until-active, and the end-to-end
runner defer), and the two Postgres active-lookup tests
(`relationship_generation_active_test.go`). `golangci-lint run ./...` → no issues.
Why safe: candidates/resolved rows only become queryable once the generation is
activated; the graph runner now also defers until the generation is active, so
neither the graph nor the Postgres read-model surface can advance ahead of the
other.

Observability Evidence:
`eshu_dp_cross_repo_activation_fenced_total` (bounded `scope_id` label) counts
generations whose activation was withheld because the durable graph-acceptance
intents failed to commit, so an operator can see at a glance that the prevention
fence fired rather than discovering stranded edges later via the reconciler. The
`recordActivationFenced` warn log ("cross-repo activation fenced: graph
acceptance not durable") names the scope, generation, withheld intent count,
reason (`graph_acceptance_commit_failed`), and underlying error class, giving an
at-3-AM operator the full picture without a dashboard. Both are exercised by
`TestCrossRepoResolutionDoesNotPublishWhenAcceptanceFails`. The #3559/#3616
reconciler (`eshu_dp_reconciliation_convergence_total`) remains as
defense-in-depth and is unchanged.
