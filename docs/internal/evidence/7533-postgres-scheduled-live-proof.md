# #7533: scheduled PostgreSQL live proof enrollment

The `postgres-scheduled` runner owns 53 exact named tests in 35 live files. The workflow runs daily at 04:35 UTC and supports manual dispatch. It uses one digest-pinned PostgreSQL 18.6 service. Crossplane and infra inventory use separate fresh databases. The three other packages use an administrative DSN to `postgres`, then the shared test helper creates a random disposable database from template0 and cleans it up. These rows have class `verified_scheduled`; they are no longer counted as legacy scheduled exemptions. The runner is not a required PR or merge check while its cost probes lack repeated timing evidence.

## Local enrollment proof

The source baseline was `eac0860933e4124400a7e59056f58954346d4c32`. Go was `go1.26.9 linux/arm64`; PostgreSQL was image `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`. Each package ran serially with `go test -json -race -p=1 -count=1` and an anchored selector of its exact named tests. The selected source files are byte-identical at that baseline and the batch worktree HEAD.

| Package | Files | Tests | Result | Process wall time |
| --- | ---: | ---: | --- | ---: |
| `./internal/storage/postgres/crossplane` | 5 | 9 | Nine run and pass events; package pass | 45.186s |
| `./internal/storage/postgres/infra/inventory` | 19 | 32 | 32 run and pass events; package pass | 109.448s |
| `./cmd/golden-corpus-gate` | 1 | 1 | One run and pass event; package pass | 15.090s |
| `./internal/query` | 9 | 9 | Nine run and pass events; package pass | 33.881s |
| `./internal/storage/postgres` | 1 | 2 | Two run and pass events; package pass | 40.885s |

No selected test or subtest skipped or failed. The local Go JSON streams are `/tmp/eshu-7533-proof/{crossplane-full,infra_inventory-full,cmd-golden-corpus-gate-disposable-pg,internal-query-disposable-pg,internal-storage-postgres-disposable-pg}.jsonl`; the coordinator recorded exact commands and outcomes in the sibling `pg-full-results.json` and `disposable-pg-results.json` files. These are observed enrollment costs on a shared host, not a controlled speed or budget comparison. The committed inventory checks every Test name against source and workflow selectors; the event verifier rejects missing, duplicate, skipped, failed, malformed, or wrong-package results. The scheduled job runs negative harness tests before the packages.

## Boundaries

Crossplane uses `ESHU_CROSSPLANE_REDRIVE_PROOF_DSN` and creates a unique schema in its fresh database. Infra inventory uses `ESHU_POSTGRES_DSN`, bootstraps its fresh database, and some tests create and drop sibling databases. The disposable packages use `ESHU_TEST_CONTENT_INDEX_POSTGRES_*`, `ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_*`, and `ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_*` with each `*_DISPOSABLE` value set to `1`. Their admin DSNs target the same owned PostgreSQL service's `postgres` database. The peer-owned `code_topic_parallel_guarded_live_test.go` is excluded from this inventory. No Go production code, SQL, fixture, golden snapshot, graph backend, or telemetry contract changed in this batch.
