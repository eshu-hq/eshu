# #7533: scheduled PostgreSQL live proof enrollment

The `postgres-scheduled` runner owns 41 exact named tests in 24 live files: nine Crossplane redrive tests in five files and 32 infra inventory tests in 19 files. The workflow runs daily at 04:35 UTC and supports manual dispatch. It uses one digest-pinned PostgreSQL 18.6 service and creates a separate fresh database for each package. The infra inventory role can create and drop sibling databases for the package's isolated fixture tests. These rows have class `verified_scheduled`; they are no longer counted as legacy scheduled exemptions. The runner is not a required PR or merge check while its cost probes lack repeated timing evidence.

## Local enrollment proof

The source baseline was `eac0860933e4124400a7e59056f58954346d4c32`. Go was `go1.26.9 linux/arm64`; PostgreSQL was image `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`. Each package ran serially on a fresh disposable database with `go test -json -race -p=1 -count=1 -timeout=7m` and an anchored selector of its exact named tests.

| Package | Files | Tests | Result | Process wall time |
| --- | ---: | ---: | --- | ---: |
| `./internal/storage/postgres/crossplane` | 5 | 9 | Nine run and pass events; package pass; no skips or failures | 45.186s |
| `./internal/storage/postgres/infra/inventory` | 19 | 32 | 32 run and pass events; package pass; no skips or failures | 109.448s |

The log streams were `/tmp/eshu-7533-proof/crossplane-full.jsonl` and `/tmp/eshu-7533-proof/infra_inventory-full.jsonl` on the local proof host. The root coordinator recorded their exact commands and outcomes in `/tmp/eshu-7533-proof/pg-full-results.json`. These are observed enrollment costs on a shared host, not a controlled speed or budget comparison. The committed inventory checks each Test name against source and workflow selectors; the event verifier rejects missing, duplicate, skipped, failed, malformed, or wrong-package results. The scheduled job also runs negative harness tests before it runs either package.

## Boundaries

Crossplane uses `ESHU_CROSSPLANE_REDRIVE_PROOF_DSN` and creates a unique schema in its fresh database. Infra inventory uses `ESHU_POSTGRES_DSN`, bootstraps its fresh database, and some tests create and drop sibling databases. No Go production code, SQL, fixture, golden snapshot, graph backend, or telemetry contract changed in this batch.
