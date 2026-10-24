# #8893: isolated PostgreSQL family enrollment

The scheduled PostgreSQL runner now owns 13 more unchanged live-test files and
26 exact test roots. The groups are admin (6 files, 8 roots), secret lines (5,
11), code reachability (1, 6), and scope completion (1, 1). The 13 paths are
the exact `next13.recorded_sources` set in the coordinator's source audit at
`c697b07618288ccd5ac298fb5ee3ba560d64fc0b`; every file's SHA256 still
matched that audit on the final workflow replay at `153feea755aec060319d2ba23462ae88fa618ac1`,
rebased onto merged freshness main `093143b350483e472602c3a66a7f3e6a6dfcaecf`.
No Go test body, assertion, fixture helper, production code, or migration changed.

The unchanged tests ran in four separate `go test -json -race -count=1
-timeout=6m -p=1` invocations with `GOMAXPROCS=4` and an anchored selector for
each group's named roots. Go was 1.26.10. The backend was the digest-pinned
PostgreSQL 18.6 image
`ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`.
Admin, reachability and completion used separate fresh databases through
`ESHU_POSTGRES_DSN`; admin also set `ESHU_ADMIN_REPLAY_FENCE_LIVE=1`. Secret
lines used its existing `ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN` administrative
connection and `ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1`, so its helper
created and dropped a separate database for each test. All proof databases
were dropped and their absence verified after the runs.

| Group | Files | Exact roots | Strict Go JSON result | Observed process wall |
| --- | ---: | ---: | --- | ---: |
| admin | 6 | 8 | 8 run/pass pairs, package pass, no skip/fail | 55.791s |
| secret lines | 5 | 11 | 11 run/pass pairs, package pass, no skip/fail | 43.363s |
| reachability | 1 | 6 | 6 run/pass pairs, package pass, no skip/fail | 9.434s |
| completion | 1 | 1 | 1 run/pass pair, package pass, no skip/fail | 5.526s |

The final replay executes the exact four workflow shell blocks, adapting only
the disposable connection target to localhost port 25474. GOFLAGS and GOWORK
are empty, and the clean-source checks and strict event verifier run before
and after the Go commands. Source hashes, workflow/body hashes, backend
identity, exits and cleanup are retained in
[the compact workflow receipt](8893-workflow-proof-summary.json). Earlier
baseline streams remain in `/tmp/eshu-8893-{admin,secret,reach,completion}.events.jsonl`. These
walls are observations on a shared host, including compilation. They are not a
performance comparison or a hosted budget claim.

Each new inventory fragment binds its source paths and exact selectors. The
runner rejects a source, selector, fixture, workflow, event stream or manifest
that differs from this reviewed mapping. The existing scheduled workflow runs
daily at 04:35 UTC, manually and on a pull request that changes enrolled paths.
The added steps run in its bulk leg and retain their Go JSON streams in the
aggregate proof bundle. They use one pinned service image with fresh named
admin, reachability and completion databases, while secret lines uses the
administrative connection. The 13 ledger rows are `verified_scheduled`, owned
by `postgres-scheduled`. On the current main base `6b662d182c659e6ae4afb142c78d535bf1fc2231`,
the legacy scheduled pin shrinks from 373 to 360 paths with SHA256
`4d735f8eaa840fe43c7ad9030efbfa9fe9a69e4483ed4e9e97df1c6ca63ac9fc`.
The combined runner has 133 files and 269 roots, including the merged
supply-chain index restart proof. The compact workflow receipt remains a
historical replay at `153feea755`; it does not prove this rebased head ran
against PostgreSQL. The rebased #8934 follow-up adds only a provenance
comment before migration 212 in `schema_order_late_test.go`; its ordered
entries and assertions stay unchanged. PR #8924 merged before that comment
was published.

Hosted execution on the reviewed revision and actual cron delivery remain
NOT_CHECKED until CI and scheduled delivery evidence is retained. The bulk
leg's existing hosted wall-time budget is unchanged; these local shared-host
costs do not prove its post-enrollment hosted wall.
