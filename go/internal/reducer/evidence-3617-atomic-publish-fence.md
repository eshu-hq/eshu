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

## No-Regression Evidence

This change reorders already-existing database statements within `Resolve` and
adds one `Int64` counter plus one warn-log emitter. It adds no new Cypher, no new
graph write, no new query shape, no extra round trip, no worker/lease/batch
change, and no new hot-path scan. The same `UpsertIntents` batch
(`ON CONFLICT (intent_id)` multi-row upsert) and the same idempotent activation
statement run; only their order changes, so the per-generation work and cost are
unchanged. Backend/version: backend-neutral (Postgres acceptance + activation
statements unchanged; NornicDB/Neo4j untouched). Input shape: per generation, one
candidate/resolved/intent set bounded by the caller's resolved-edge count, same
as before. Measurement: `go test ./internal/reducer ./internal/storage/postgres
./internal/storage/cypher ./internal/status ./internal/telemetry -count=1 -race`
→ 4126 passed, including the four new fence tests
(`cross_repo_resolution_fence_test.go`): failing-first acceptance-before-
activation ordering, no-publish-on-acceptance-failure, idempotent
converge-on-retry (acceptance committed once, publish once), and the
empty-evidence tombstone fence. `golangci-lint run ./internal/reducer/...
./internal/telemetry/...` → no issues. Why safe: candidates/resolved rows only
become queryable once the generation is activated, so persisting them ahead of
the fence cannot publish anything; the only state that publishes a generation is
the activation flip, which now runs strictly after the durable acceptance
commit.

## Observability Evidence

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
