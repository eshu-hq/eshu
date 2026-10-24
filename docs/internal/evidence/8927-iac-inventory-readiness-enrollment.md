# IaC inventory PostgreSQL readiness enrollment (#8927)

The three existing IaC inventory roots were legacy `scheduled` exemptions with no verified runner mapping. The two test files now carry readiness markers. The generated inventory selects their exact names in `./internal/query/iac`; the runner requires both `ESHU_POSTGRES_DSN` and `ESHU_IAC_INVENTORY_LIVE=1` before any package starts. The workflow supplies the opt-in and is selected by query/iac changes on pull requests and main pushes. It has no cron trigger; scheduled delivery is not claimed.

## Unchanged-source proof

On baseline `b3ea1a48a128e93c2b44c73f6ede33583f346a77`, Go 1.26.10 ran `go test -json -race -p 1 -count=1 -timeout=4m -run '^(TestPostgresIaCInventoryStoreLive|TestPostgresIaCInventoryReadModelLive|TestPostgresIaCInventoryFailedGenerationServesLastProjected)$' ./internal/query/iac` against a disposable PostgreSQL 18.6 container pinned to `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` (image ID `sha256:c293117fcecda7344b5480222e813b9f673d7abd69b1dd95eff239b768b04f59`).

Each named root had one run and one pass, with zero skips or failures; the package passed. Their individual Go event elapsed values were 0.07, 0.12, and 0.07 seconds. Total `go test` wall time was 121.529 seconds including a cold isolated-cache race compile. These are observed costs, not before/after speed measurements.

The fresh database had no IaC proof schemas before or after the run, and the owned container was removed. A compact proof receipt is committed as `docs/internal/evidence/8927-iac-inventory-proof-summary.json`; raw local JSON events and the bounded runner receipt are retained under `/tmp/eshu-7533-drive/8927-baseline/`. Source SHA256 values at the baseline are `f41f00d03b4529a58fd7dba8eef30cccff002f314708a7529830aa39d553a226` and `c853e4255d23a8d00ef0e129dd39f6765f6709f8bb4b0f7f306a3ff34dbaec78`. Removing only the new marker lines from each current test file yields those baseline bytes exactly, including fixture, SQL, assertions, and blank lines; the check is retained in `/tmp/eshu-7533-drive/8927-bodyhash-carryforward.json`.

On the combined branch the ledger has 914 classified live files. The two IaC files move from legacy `scheduled` to `postgres_ci`; the delta-union file also leaves the legacy set through its separate runner. The resulting candidate exemption policy is 357 paths, SHA256 `3f234e78b86bc80a586408ae56d05b74f49f271414cf0c30182492838c4b1e3b`, derived from merged main’s 360. Regenerate the count and digest after further inventory changes.

## Remaining acceptance

Exact integrated-head hosted readiness execution, CI review, and main-push delivery are NOT_CHECKED. The baseline proves unchanged local test behavior and cleanup; it does not prove a hosted job ran this new mapping. No production query, SQL, writer, or telemetry contract changed.


## Combined delivery

PR #8937 now carries both this enrollment and #8785’s delta-union workflow. The two workflows and event contracts remain separate. The existing physical receipts are historical source-bound evidence; both workflows still need hosted acceptance on the combined reviewed head. #8785’s deployed QA criterion remains open.


## Integration with the measured shard plan

Rebasing onto #8971 preserved the IaC and delta selectors, but the new shard drift check rejected three packages without measured costs. Its existing limit is two. The weights refresher was run on all four successful readiness shard logs from each of runs [40047410741](https://github.com/eshu-hq/eshu/actions/runs/40047410741), [40049588589](https://github.com/eshu-hq/eshu/actions/runs/40049588589), [40071537963](https://github.com/eshu-hq/eshu/actions/runs/40071537963), and [40074710798](https://github.com/eshu-hq/eshu/actions/runs/40074710798). This retains the weighted-cut observation accepted in #8930 as well as the last run, which includes this IaC package.

The regenerated model has a measured IaC package overhead of 1.5 seconds and no unmeasured packages. After the merged storage enrollment, the current plan has 595 tests with bounds 0, 94, 286, 355 and 595. Its four predicted job costs are 1170.2, 1198.4, 1320.8 and 1320.2 seconds, all below the unchanged 1440-second limit; the largest predicted deviation from the mean is 6.56%, below 15%. Eleven tests use the documented default test weight, within the unchanged drift limit. These are model predictions from the retained logs, not observed durations on the new cut. Hosted acceptance of that cut remains pending.

The existing refresher now renders two table entries per line. Its six-hundred-test round-trip regression failed at 614 lines before the formatting change, then passed without changing JSON data. The adopted weights have 315 lines and exactly the same values as the independently measured candidate.

At the preceding integrated base, all 40 shard-contract tests passed. On the latest base, readiness registration reports no drift and the current model has no budget or drift errors. The earlier 22 focused mirror/generator checks passed before that rebase and require the normal promotion checks on the combined tree. The complete existing fake-Go readiness self-test passed in 370.625 seconds on the preceding three-sample model, with unchanged inputs during that run; the mandatory pre-push floor includes its current four-sample run. This is hermetic proof, not another backend run. Registry fragments remain below 500 lines after moving a complete gate record across their boundary; parsed records and their order are identical, and both content-bucket review warnings remain intact. The derived exemption set is still 357 of 914 ledger rows.
