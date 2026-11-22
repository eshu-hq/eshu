# #10880 PR-B1 theory proof: scratch SQL

Scratch SQL behind
[the PR-B1 supports-table proof](../../design/10880-pr-b1-supports-table-proof.md).
It is evidence, not a migration: nothing here is applied by the product. All
data is synthetic. Run it against a disposable `postgres:18` that
`ApplyBootstrap` has bootstrapped.

| File | What it does |
| --- | --- |
| `gen_scale_corpus.sql` | The scaled corpus: 800 scopes, 20,000 canonical keys, 28,000 active supports, stale generations, operator overrides, derived-class rows, table noise. |
| `scale_profile_divergence_patch.sql` | Makes even multi-support keys `affected_exact` so precise eligibility can diverge between supports. Run after the generator and before the backfill is re-run. |
| `same_scope_collision.sql` | Two facts in one scope that share a canonical key (seed database). |
| `supports_ddl.sql`, `supports_indexes.sql` | The candidate table and the indexes the measured plans used. |
| `supports_backfill.sql` | Fills the table from `fact_records`; generated from `CanonicalFindingKeySQL` and `severityBucketFactSQL`. |
| `candidate_list_read_delta.sql` | The three fragments that turn the legacy list CTE into the supports read. |
| `census.sql` | Supports per key, duplicate (key, scope) pairs, per-field divergence. |
| `write_cost.sql`, `supports_wc_insert.sql` | WAL measurement of the supports writes (run `psql -f write_cost.sql` from this directory). |
