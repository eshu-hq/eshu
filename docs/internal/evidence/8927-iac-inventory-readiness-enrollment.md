# IaC inventory PostgreSQL readiness enrollment (#8927)

The three existing IaC inventory roots were legacy `scheduled` exemptions with no verified runner mapping. The two test files now carry readiness markers. The generated inventory selects their exact names in `./internal/query/iac`; the runner requires both `ESHU_POSTGRES_DSN` and `ESHU_IAC_INVENTORY_LIVE=1` before any package starts. The workflow supplies the opt-in and is selected by query/iac changes on pull requests and main pushes. It has no cron trigger; scheduled delivery is not claimed.

## Unchanged-source proof

On baseline `b3ea1a48a128e93c2b44c73f6ede33583f346a77`, Go 1.26.10 ran `go test -json -race -p 1 -count=1 -timeout=4m -run '^(TestPostgresIaCInventoryStoreLive|TestPostgresIaCInventoryReadModelLive|TestPostgresIaCInventoryFailedGenerationServesLastProjected)$' ./internal/query/iac` against a disposable PostgreSQL 18.6 container pinned to `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` (image ID `sha256:c293117fcecda7344b5480222e813b9f673d7abd69b1dd95eff239b768b04f59`).

Each named root had one run and one pass, with zero skips or failures; the package passed. Their individual Go event elapsed values were 0.07, 0.12, and 0.07 seconds. Total `go test` wall time was 121.529 seconds including a cold isolated-cache race compile. These are observed costs, not before/after speed measurements.

The fresh database had no IaC proof schemas before or after the run, and the owned container was removed. A compact proof receipt is committed as `docs/internal/evidence/8927-iac-inventory-proof-summary.json`; raw local JSON events and the bounded runner receipt are retained under `/tmp/eshu-7533-drive/8927-baseline/`. Source SHA256 values at the baseline are `f41f00d03b4529a58fd7dba8eef30cccff002f314708a7529830aa39d553a226` and `c853e4255d23a8d00ef0e129dd39f6765f6709f8bb4b0f7f306a3ff34dbaec78`. Removing only the new marker lines from each current test file yields those baseline bytes exactly, including fixture, SQL, assertions, and blank lines; the check is retained in `/tmp/eshu-7533-drive/8927-bodyhash-carryforward.json`.

On this branch the generated ledger has 896 classified live files. The two IaC files move from legacy `scheduled` to `postgres_ci`, so the branch's candidate exemption policy is 381 paths, SHA256 `5381c1ab3898157441a63dda4000de4b51edec70b0eda84a3ecf7562f44005cb`, derived from main's 383. This count and digest must be regenerated after any integration with sibling #7533 branches.

## Remaining acceptance

Exact integrated-head hosted readiness execution, CI review, and main-push delivery are NOT_CHECKED. The baseline proves unchanged local test behavior and cleanup; it does not prove a hosted job ran this new mapping. No production query, SQL, writer, or telemetry contract changed.
