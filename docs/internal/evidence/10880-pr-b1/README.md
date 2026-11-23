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
| `m6_write_cost_and_backfill_harness.go.txt` | Build-time harness (a `_test.go` once copied into `go/internal/storage/postgres`): writer WAL and wall with and without the supports statements on the real writer, the backfill duration at 100,000 facts, and the fence trigger cost on impact and other-kind inserts. |
| `m6_read_blocks_harness.go.txt` | Build-time harness (copy into `go/internal/query/supply/chain/impact` as a `_test.go`): block accesses and time of the legacy list read against the committed candidate at four grant sizes. |

## Re-running the Go harness

The harness is not committed. It lives untracked in the proof worktree as
`go/internal/query/supply/chain/impact/zz_b1_*_scratch_test.go` and
`go/cmd/zz-scratch-b1/main.go`. It needs a Postgres 18 and these steps:

1. Start a private `postgres:18` (`shared_buffers=2GB`, `work_mem=16MB`,
   `jit=off`, `track_io_timing=on`). Bootstrap two databases with
   `go run ./cmd/zz-scratch-b1 "<admin dsn>" seed scale`.
2. Seed: `B1_SEED_DSN=<seed dsn> go test ./internal/query/supply/chain/impact
   -run TestZZB1SeedScratch` (runs `seedMultiSupportCorpus` and
   `RebuildAllWinners`), then `same_scope_collision.sql`.
3. Scale: `gen_scale_corpus.sql`, `scale_profile_divergence_patch.sql`, then
   rerun `TestZZB1SeedScratch` with `B1_SKIP_SEED=1` against the scale DSN to
   rebuild the winners.
4. In both databases apply `supports_ddl.sql`, `supports_backfill.sql`,
   `supports_indexes.sql` (the proof's final index set uses
   `(scope_id, generation_id)` in place of `(scope_id)`).
5. Tests read `B1_DSN`: `TestZZB1Equality` (`B1_CORPUS=seed|scale`,
   `B1_IMAGE_REF` for the scaled image filter), `TestZZB1Explain` (`B1_OUT`
   prefix for results, `B1_ONLY` to pick scenarios, `B1_SKIP_LEGACY`),
   `TestZZB1Latency`, `TestZZB1Cold` (`B1_COLD=s80|r20|comp`, `B1_READ`),
   `TestZZB1Concurrency` (`B1_POPULATE_SQL` = `supports_backfill.sql`).
6. Read-shape switches: `B1_LATERAL=1` (indexed operator probe) and
   `B1_GRANTLEG=1` (grant legs, granted cases only). The final shape is both.
   Run one heavy job at a time; the scaled equality run takes about six minutes.
