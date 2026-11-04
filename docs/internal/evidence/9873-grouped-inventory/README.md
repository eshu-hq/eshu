# Reproducing the #9873 Grouped Inventory Measurements

These files rebuild the corpus and rerun the timings and parity checks behind
[the grouped inventory evidence note](../9873-grouped-inventory.md).

- `seed.sql`: the synthetic corpus. At 1x it holds 20,000 findings over 400
  repositories (25 percent unmapped), service catalog, workload identity, and
  CI/CD run facts. It adds 10 percent duplicate-scope copies, 5 percent
  superseded-generation findings, 1 percent operator suppressions, and
  `bgper` background facts per finding. `mult` scales everything.
- `bench_overlay_test.go.txt`: a Go test file for package `impact`, kept out
  of the module. `TestZZStoreApplySchema` applies the production bootstrap,
  `TestZZStoreRebuild` rebuilds winners with `RebuildAllWinners` and runs
  `VACUUM (ANALYZE)`, `TestZZStoreBench` writes one
  `EXPLAIN (ANALYZE, BUFFERS)` plan per case plus 3 timed store calls, and
  `TestZZStoreParity` compares sampled buckets with the count route (run it at
  1x; the count route is slow at 5x).

The query plans are not committed: each bench run writes them to `ZZ_OUT` as
`plan_<scale>x_<case>.txt`, next to `bench_<scale>x.jsonl`.

## Steps

Use a disposable PostgreSQL 18 server. The seed truncates `ingestion_scopes`
with `CASCADE`, so never point it at a database you need.

```bash
ADMIN='postgres://USER:PASS@127.0.0.1:PORT/postgres?sslmode=disable'
DSN='postgres://USER:PASS@127.0.0.1:PORT/grouped_bench?sslmode=disable'
psql "$ADMIN" -c 'CREATE DATABASE grouped_bench'

WT="$(git rev-parse --show-toplevel)"
DIR="$WT/docs/internal/evidence/9873-grouped-inventory"
OUT="$(mktemp -d)"
printf '{"Replace":{"%s":"%s"}}' \
  "$WT/go/internal/query/supply/chain/impact/zz_store_bench_test.go" \
  "$DIR/bench_overlay_test.go.txt" > "$OUT/overlay.json"

cd "$WT/go"
run() { ZZ_STORE_DSN="$DSN" ZZ_OUT="$OUT" go test -overlay "$OUT/overlay.json" \
  ./internal/query/supply/chain/impact/ -count=1 -v -timeout 30m "$@"; }
run -run TestZZStoreApplySchema
psql "$DSN" -v mult=5 -v bgper=8 -f "$DIR/seed.sql"
run -run TestZZStoreRebuild
ZZ_SCALE=5 run -run TestZZStoreBench

psql "$ADMIN" -c 'DROP DATABASE grouped_bench WITH (FORCE)'
```

For the 1x parity table, seed with `mult=1`, rebuild, and run
`-run TestZZStoreParity`.
