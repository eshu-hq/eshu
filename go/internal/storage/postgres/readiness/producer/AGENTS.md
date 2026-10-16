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
- The cycle-start column is `max(COALESCE(reopened_at, created_at))`
  filtered to in-flight statuses. Never use `updated_at`: a retry or re-claim
  moves it, and the fence would never reach its bound for a wedged producer.
- Return an error, never an empty map, when the database cannot answer. An
  empty answer reads as ready and commits runtime evidence read before the
  producer's write.
- The producer domains come from the caller
  (`servicecatalog.RuntimeProducerDomains`). Do not hard-code a domain here.
- Imports: `reducer/contract`, `reducer/servicecatalog`, `internal/scope`,
  and `postgres/db`. Never import the parent `postgres` package; its live
  proofs import this package instead.
