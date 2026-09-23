# #6784 slice B: live-backend test stacks — performance evidence

No-Regression Evidence: the new `docker-compose.live-backend-*.yml` files
define test-only single-service backend stacks (one pinned image, no eshu
services). They change no production topology: the main
`docker-compose.yaml` / `docker-compose.neo4j.yml` are untouched, no hot
Cypher or graph-write path changes (Go changes in this PR are `_test.go`
files and ledger/registry YAML only), and the stacks exist solely for the
per-file fresh-database runs of `scripts/run-live-backend-tests.sh`.
Measured locally 2026-09-23: full `--backend both` run completes 38/38
ledger files green (22 NornicDB + 16 Neo4j) with per-file `down -v` + `up`
freshness; no production benchmark applies because no production code path
moves.

No-Observability-Change: this PR adds no runtime signals. The runner prints
per-file progress lines to stdout for CI logs; no metrics, spans, or
status surfaces change.
