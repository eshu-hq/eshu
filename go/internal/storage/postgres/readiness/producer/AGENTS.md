# AGENTS.md — internal/storage/postgres/readiness/producer

Scoped instructions for this package. The root `AGENTS.md` still applies.

- Keep `RuntimeProducerStatesQuery` a single read-only statement that pins
  `work.generation_id = scope.active_generation_id`. Counting a newer pending
  or an older generation's rows changes the fence's lifecycle contract
  (`servicecatalog.RuntimeProducerReadiness`); change that contract first.
- The statement must reach `fact_work_items` only through
  `fact_work_items_scope_generation_idx`.
  `TestReducerContentionGateRuntimeProducerStatesPlanUsesScopeGenerationIndex`
  (parent package) is the guard.
  Re-measure on a populated table (see README.md) before changing the join or
  predicates.
- The anchor column is `max(GREATEST(COALESCE(reopened_at, created_at),
  COALESCE(last_attempt_at, created_at), COALESCE(visible_at, created_at),
  COALESCE(claim_until, created_at)))` filtered to in-flight statuses. Do not
  drop `last_attempt_at` or `visible_at`: the producer's non-counting
  corpus-fence retries move only those, so without them every producer reads
  as past the bound when a long corpus fence closes. Do not drop
  `claim_until`: heartbeat writes only `claim_until` and `updated_at`, so
  without it a live run longer than 30 minutes reads as exceeded.
  `claim_until` is non-NULL only on claimed and running rows (retry, ack,
  fail, and dead-letter set it NULL). Never use `updated_at`.
- Keep both halves in ONE statement (`RuntimeProducerAndRunsOnStatesQuery`,
  #8169). The repo_dependency runner commits the RUNS_ON write, the workload
  replay, and the intent completion separately; two statements can read the
  producer before the replay and the intent after its completion and see
  neither in flight.
- The RUNS_ON half keys on `repository_id` only. Do not join it to
  `ingestion_scopes.active_generation_id`: whether the intent's
  `generation_id` is a scope generation is not proven.
- Return an error, never an empty map, when the database cannot answer. An
  empty answer reads as ready and commits runtime evidence read before the
  producer's write.
- The producer domains come from the caller
  (`servicecatalog.RuntimeProducerDomains`). Do not hard-code a domain here.
- Imports: `reducer/contract`, `reducer/servicecatalog`, `internal/scope`,
  and `postgres/db`. Never import the parent `postgres` package; its live
  proofs import this package instead.
- `ImpactProducerStateQuery` (#8827) is one aggregate keyed on the intent's own
  `scope_id` and `generation_id`; it must keep reaching `fact_work_items`
  through `fact_work_items_scope_generation_idx` (the live plan test is the
  guard) and keep the four-term last-workable anchor with no `updated_at`.
  An unanswerable question is an error, never an empty state: an empty state
  reads as "no impact work in flight" and lets the reconciliation spend
  attempts or give up while the producer runs. The domain constant comes from
  `reducercontract.DomainSupplyChainImpact`. The succeeded and failed counts
  (#8956) decide a genuine no-match: count only `succeeded` as succeeded and
  only `dead_letter`/`failed` as failed, so any other terminal status keeps
  `Settled` false.
