# AGENTS.md — internal/storage/postgres/iamcantargets

Scoped instructions for this package. The root `AGENTS.md` and `CLAUDE.md`
still apply.

- Keep every read bounded by the request: account, requested service kind and
  region, and exact ARN. Never widen it to a scan of an account's facts or of
  another account. The owner decision for #6785 forbids a whole-graph scan.
- Sample scope readiness before loading facts, and pin each load to the
  generation id the sample returned. Reversing that order reintroduces the
  #5875-class TOCTOU.
- Return an error, never an empty snapshot, when the database cannot answer.
  The handler reads an empty snapshot as "settled, unresolved".
- `candidateScopesQuery` is Postgres SQL on a readiness path. Changing it needs
  a fresh `EXPLAIN (ANALYZE, BUFFERS)` at a representative scope count, per the
  root Prove-The-Theory-First rule, and the numbers in README.md updated.
- This package imports `internal/facts`, `reducer/iamcan`, `postgres/db`, and
  `postgres/pgarray` only. It must not import the parent `postgres` package;
  the fact read comes in through the `FactLister` interface.
