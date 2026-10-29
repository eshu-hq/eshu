# #7533: scheduled PostgreSQL live proof enrollment

The `postgres-scheduled` runner owns 269 exact named tests in 133 live files (including the #8893 isolated-family enrollment and the merged runtime index restart proof).
The same job runs daily at 04:35 UTC, manually, and for pull requests to
`main` changing its enrolled inputs. It uses one digest-pinned PostgreSQL 18.6 service.

Crossplane, infra inventory, ACK/completion, claim, liveness, fanout, search,
readiness, containerimage and freshness-links use separate fresh databases. The three disposable
groups use an administrative DSN to `postgres`; their shared helper creates a
random database from template0 and cleans it up. The freshness-links group
additionally loads a second auxiliary database from the shipped scale fixture
(see below) and runs the retention-timing template build before its proof.
These rows have class `verified_scheduled` and leave the legacy exemption inventory.

The exact hosted PR job must pass before enrollment merges. It is not a
universally required PR or merge check while cost probes lack repeated timing
evidence. Actual cron delivery remains NOT_CHECKED.

## Local enrollment proof

The source baseline was `eac0860933e4124400a7e59056f58954346d4c32`. Go was `go1.26.9 linux/arm64`; PostgreSQL was image `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`. Each package ran serially with `go test -json -race -p=1 -count=1` and an anchored selector of its exact named tests. The first five groups originally used source files from that baseline; the infra inventory group has the later test-only repairs described below. The ACK group updates one stale assertion to the current typed supersession contract. The five additional groups have unchanged source bytes from the owned five-database PostgreSQL 18.6 proof on parent `8e52aa782a892fc0a86a32fc8898a5405f552ce6`.

| Package | Files | Tests | Result | Process wall time |
| --- | ---: | ---: | --- | ---: |
| `./internal/storage/postgres/crossplane` | 5 | 9 | Nine run and pass events; package pass | 45.186s |
| `./internal/storage/postgres/infra/inventory` | 19 | 32 | Corrected 32 run and pass events; package pass | 102.680s |
| `./cmd/golden-corpus-gate` | 1 | 1 | One run and pass event; package pass | 15.090s |
| `./internal/query` | 9 | 9 | Nine selected run and pass events; package pass | Not measured for the exact nine-test subset |
| `./internal/storage/postgres` disposable storage | 1 | 2 | Two run and pass events; package pass | 40.885s |
| `./internal/storage/postgres` ACK/completion | 22 | 43 | 43 run and pass events; package pass; no fail or skip | 159.951s |
| `./internal/storage/postgres` claim | 13 | 36 | 36 run and pass events; package pass; no fail or skip | 299.214s |
| `./internal/storage/postgres` liveness | 7 | 18 | 18 run and pass events; package pass; no fail or skip | 24.257s |
| `./internal/storage/postgres` fanout | 3 | 3 | Three run and pass events; package pass; no fail or skip | 39.440s |
| `./internal/storage/postgres` search | 5 | 8 | Eight run and pass events; package pass; no fail or skip | 37.286s |
| `./internal/storage/postgres` readiness | 4 | 6 | Six run and pass events; package pass; no fail or skip | 56.766s |
| `./internal/reducer/containerimage` | 8 | 15 | 15 run and pass events; package pass; no fail or skip | 8.495s |
| `./internal/storage/postgres/freshness/links` | 22 | 58 | 58 run and pass events; package pass; no fail or skip | 552.478s |

No selected test or subtest skipped or failed. The original first-five local Go JSON streams are `/tmp/eshu-7533-proof/{crossplane-full,infra_inventory-full,cmd-golden-corpus-gate-disposable-pg,internal-query-disposable-pg,internal-storage-postgres-disposable-pg}.jsonl`; the coordinator recorded their exact commands and outcomes in `pg-full-results.json` and `disposable-pg-results.json`. The original `infra_inventory-full.jsonl` is historical baseline evidence; `pg-infra-32-final.jsonl` supersedes it for the corrected infra inventory source, while the other four streams retain their source provenance. The query stream's 33.881s process wall time belongs to an enclosing ten-test run that also included the excluded peer test. Its events prove the selected nine-test subset passed, not its isolated cost. The ACK stream is `/tmp/eshu-7533-ack-full.jsonl`; its command, exit, and process wall time are in `/tmp/eshu-7533-ack-full-result.json`. The baseline stream `/tmp/eshu-7533-proof/ack-family.jsonl` had 42 passing tests and one failing stale supersession assertion before the test-only correction. The listed times are observed enrollment costs on a shared host, not a controlled speed or budget comparison. The versioned inventory index lists nineteen closed JSON fragments. The manifest checksum binds the index bytes, each listed path, and every fragment byte. The committed inventory checks every Test name against source and workflow selectors; the event verifier rejects missing, duplicate, skipped, failed, malformed, or wrong-package results. The scheduled job runs negative harness tests before the packages.

The combined worktree replay on parent `8e52aa782a892fc0a86a32fc8898a5405f552ce6` ran the exact 43 ACK tests with `-race -p=1 -count=1` against a fresh owned PostgreSQL 18.6 database. It exited 0; the strict event check found 43 named run/pass pairs, package pass, and no selected skip or failure. The stream and command receipt are `/tmp/eshu-7533-pg57-ack-final.jsonl` and `/tmp/eshu-7533-pg57-ack-final-result.json`. Its 915.915s process wall includes cold race compilation in a new private Go cache, so it is not comparable to the earlier 159.951s ACK run or a controlled budget result. The original local 167-test bundle combines those six passing streams with the five passing streams from `/tmp/eshu-7533-isolation-{claim,liveness,fanout,search,readiness}.jsonl`. The five-group command and result manifest is `/tmp/eshu-7533-isolation-groups-results.json`; its receipt records 71 exact passing tests on five fresh databases in 458.455s total wall. Those group process times are observations on a shared host, not controlled speed comparisons. The local 167-test bundle labels its synthetic run metadata `local_simulation`; its artifact location is retained in the session receipt.

## First hosted attempt and corrected infra inventory proof

The first hosted PostgreSQL attempt failed its infra inventory group because
`TestRetentionLiveLockHoldAndDeriveWaitCost` reused one 90-second database
context across three complete rounds. It logged two successful rounds and
expired during the third. The corrected test gives each round, which retains two separate 50,000-row
seed and prune phases, an independent 90-second context. It joins its lock
observer and retention worker before returning and keeps the 7-minute package
timeout. A local race run exposed a fixture shutdown race in
`TestWriterFenceLiveInvariantUnderConcurrentWritesAndRepairs`: a mark became
visible after its final reconcile cycle began. The test passed its 6-second
scheduling context to admitted SQL, so cancellation did not establish that
all server work had finished before the final check. Its three concurrent actors now use
the existing 90-second operation context, stop admitting work after six
seconds, record late operation and per-repository errors, and join before one
final reconcile and repeatable-read snapshot. No workload, actor count,
production SQL, or runtime contract changed.

At the corrected source, the exact 32-test infra inventory group passed with
`-race -p=1 -count=1 -timeout=7m` on a fresh PostgreSQL 18.6 database. The
strict event verifier found 32 named run/pass pairs, package pass, and no
selected skip or failure. The three retention rounds all logged their full
measurements and passed. The process wall time was 102.680 seconds on a shared
host, an observed cost rather than a controlled speed comparison. The retained
local stream is `pg-infra-32-final.jsonl` with receipt
`pg-infra-32-final-receipt.json`; its source hashes bind the corrected fence
and retention tests. Controlled unit regressions proved a fresh context per
round and that an admitted operation continues after scheduling stops, joins,
and reports its late error. A passing hosted job on the final revision remains
a merge prerequisite.

## Container-image fixture regression and route

The first 15-test race run passed 14 tests and failed the unmapped-config
handler proof because its legacy writer lacked the required activation lookup.
The repair preserves the original assertions in an internal test-only helper.
An external test wrapper supplies the real PostgreSQL scope-state lookup, and
this fixture activates its generation through the canonical trigger. No
production export, copied lookup query or fabricated epoch is used.

The corrected source at `b4ba61843ca3933fc1d50491f595a9a4b0425127` passed all 15
named tests with `-race -p=1 -count=1`, package pass, and no skip or failure.
Its 8.495s process wall includes canonical bootstrap in the package's external
`TestMain`. The retained receipt and stream are
`/tmp/eshu-7533-containerimage-corrected/receipt.json` and `tests.log`.
This is an observed enrollment cost, not a before/after performance comparison.

The scheduled group first creates `eshu_containerimage_scheduled`, sets its
`ESHU_POSTGRES_DSN`, and passes
`-args -containerimage-bootstrap=1 -containerimage-database=eshu_containerimage_scheduled`.
The test binary checks the explicit opt-in, exact requested and connected
database identities, public schema and absence of user tables before calling
`postgres.ApplyBootstrap` with a two-minute context. Ordinary test runs do not
bootstrap automatically. No pool limit or writer concurrency is changed.

Configuration and subprocess tests reject missing opt-in, absent or mismatched
DSNs, a foreign database and connection failure. An actual nonempty fixture was
rejected. Changing the bootstrap error exit to success caused the subprocess
contract to fail; the restored source passed. The 15 real tests retain their
hold/tombstone assertions, lock interleavings, heartbeat and deadlock checks.
The workflow's bounded containerimage package path includes its bootstrap and
bridge helpers. Group metadata lives in `postgres_scheduled_contract.py` so the
event verifier stays below the file limit.

## Freshness-links enrollment proof

The 22 changed-since link files (58 named tests, one `linksfreshnessstore_test`
package) ran serially with `go test -json -race -p=1 -count=1` on disposable
databases in image `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`
(observed `PostgreSQL 18.6 on aarch64-unknown-linux-musl`). Go was
`go1.26.6 linux/arm64`. The strict event verifier found 58 named run/pass
pairs, package pass, and no selected skip or failure; the dress-rehearsal
stream passes the same per-group check the hosted job runs.

Per-part observed costs on a shared host (enrollment observations, not
controlled speed comparisons): 53 template-harness tests 377.766s; W11 cost
measurement 6.751s; retention-timing template build 13.388s and run-mode prune
(40 generations, big link deleted) plus clone-drop regression 5.958s; scale
evidence 160.654s and p3 proof 30.295s. The single-invocation dress rehearsal
with the exact enrolled env set and 58-test selector took 552.478s process
wall. The scale fixture load (migrations 001, 002, table-only 003 and 136 as
shipped, `7127-link-writer-fixture.sql`, the two link-usable indexes, ANALYZE)
took 349.2s plus 43.5s for 9,753,608 fact rows.

Bounded enrollment config, pinned in the runner: `ESHU_7127_W11_COST=1`;
retention timing `MODE=run` on the pre-built `link` fixture (the step builds
the template with an uncaptured `MODE=build` run first); scale evidence with a
single round (`ROUNDS=1`, `VALID_ROUNDS=1`, `DEADLINE=30m`) and the p3 equality
proof (`PROOF=p3`, pre-fix statement against the fixed statement, rolled
back). The scale canary base resolves to the proof checkout (`BASE_REF=HEAD`,
the same fallback a shallow CI checkout resolves to), so the enrolled scale
proof is execution plus functional equality: the G7/G8 timing ratios stay
emitted evidence, not gates. The observed run had 1/1 valid G7 rounds, valid
n=1/2/4 concurrency windows with zero temp files, and `p3_done equal=true`.
The four `go/internal/reducer/freshness/links` rows stay scheduled for a later
batch. No Go production code, SQL, fixture, or telemetry contract changed.

## Hosted proof retention

The setup leg writes `manifest.json` with the run URL, ID, attempt, event, reviewed PR head SHA, actual checkout SHA, event SHA, inventory and selected-source checksums, pinned image, and observed PostgreSQL version identity. On a partial job rerun, the aggregate selects the latest artifact for each leg from the same workflow run, rejecting a missing or incomplete latest leg. It retains `artifact_sources.json` with the selected attempt per leg. The final step rechecks the manifest against the current run, checkout, reviewed head, and source bytes, then verifies every enrolled Go JSON stream. A manifest from an earlier attempt of that same run is valid; a future attempt or different run or head is not. The aggregate upload step runs on every aggregate attempt (`if: always()`), including after an assembly or bundle-verification failure, and retains whatever bundle files were assembled for 14 days; an upstream proof-leg failure skips the aggregate job. The workflow contract pins every executable step, including checkout, Go setup, module prewarm, manifest creation, database bootstrap, proof commands, and bundle verification. Each Go proof step checks staged, unstaged, and untracked changes across the full Git checkout, including SDK inputs and a root workspace immediately before its test command and after event validation; it also refuses shell `GOFLAGS` or `GOWORK`, persistent `GOFLAGS` from `GOENV`, and an active ancestor or ignored `go.work`. This binds tested source bytes to the event checkout after module prewarm and between package groups. Before merge, record the passing PR run and artifact URL and verify its reviewed head and checkout SHAs. The local manifest and bundle check used synthetic run metadata with the owned PostgreSQL 18.6 fixture and retained Go streams. Its manifest says `local_simulation`; the hosted verifier rejects that origin, and GitHub Actions forbids the local allowance. This local check is not hosted execution evidence.

## Step 7 sharding (#8617)

The serial suite's p95 was ~84 min (ten latest hosted runs on 2026-10-19,
success range 62-84 min). The workflow now runs seven jobs: `scheduled-setup`
(enrollment gate plus manifest from its own same-image service),
`scheduled-freshness-scale`, `scheduled-freshness-a`, `scheduled-freshness-b`,
`scheduled-ack`, `scheduled-bulk`, and `scheduled-aggregate` (download,
bundle verify, retain). The freshness split and leg budgets rest on run
39339606297 (success) per-test artifact timings:

| Leg | Files | Tests | Measured test time |
| --- | ---: | ---: | ---: |
| freshness-scale (scale, scale-proof, retention-timing, timing-clone) | 4 | 4 | ~16.6m |
| freshness-a (retention-plan, fence, lock-timeout ×2, retention ×2, journal, link, poison) | 9 | 33 | ~5.7m |
| freshness-b (rekey, plan, plan-stats, prior-plan, stale-stats, retention-race, prior-wait, fence-breach, prior-fence) | 9 | 21 | ~4.3m |
| ack (unchanged) | 22 | 45 | ~4.6m tests inside a 22.4m wall incl. the one-time -race build |
| bulk (all other families, serial, one warm build cache) | 75 | 139 | ~16m tests |

Leg caps: setup 15, scale 45, chunks 25, ack 30, bulk 40, aggregate 15.
The runner pins the matrix shape (one selector per group across legs, fresh
database and DSN per group, per-leg stream uploads, aggregate fan-in and exact
bundle paths) and the workflow SHA256. Acceptance (#8617 step 7, as amended
2026-10-20; the original 25-minute figure was a pre-measurement estimate):
slowest leg under 35 minutes against the 45.7m serial freshness_links
baseline. Measured on run 39421484741 (all legs green, aggregate SUCCESS):
scale 32m20s, bulk 26m33s, ack 23m09s, chunks ~7-8m, setup 50s, aggregate 16s.
Leg caps above are timeout kill limits, not the acceptance bound. No Go
production code, SQL, fixture, or telemetry contract changed.

## Storage leg (#9488)

By 2026-10-29 `scheduled-bulk` was cancelled at its 40-minute cap on PRs
#9481 (twice) and #9483. Per-family step walls from four job logs show that
the cost is the claim family's `-race` test build of
`./internal/storage/postgres`, the first family to test that package in the
leg. It is not the tests themselves:

| Run (bulk job) | Claim wall | Claim tests | Bulk step | Result |
| --- | ---: | ---: | ---: | --- |
| main 40485127858 (122124889517) | 14.7m | 3.5m | 24.5m | pass |
| main 40674818081 (122770411208) | 19.7m | 5.0m | 33.0m | pass |
| PR #9483 40842111898 (123341097469) | 25.7m | 5.3m | 39.7m | cancelled |
| PR #9481, attempt 2 | 26.4m | 5.4m | 39.5m | cancelled |

The package grew 1.0% in source bytes (13.48 to 13.61 MB) between the heads
of runs 40485127858 and 40842111898. The two slow runs used runner image
20261025.353.1 and the two fast ones used 20261012.334.1; that is a
correlation, not a diagnosed cause. The five other root-package families reuse
the build and take 0.2-1.1m each. A local trial found no measurable `go vet`
cost.

The six families that test the root package (claim, liveness, fanout, search,
readiness, disposable_storage) now run in `scheduled-storage`, from
`.github/actions/postgres-scheduled-storage`. The nine others stay in
`scheduled-bulk`. Predicted from the measured family walls, the storage leg
takes 16.9-29.8m of steps and the bulk leg 7.6-12.7m. The 12.7m top end
estimates the four families cancelled on #9481 from the other runs. Both caps
stay at 40. The runner rejects a root storage `go test` in any leg except ACK and storage,
so a new family cannot bring the build back into bulk.

## Boundaries

Crossplane uses `ESHU_CROSSPLANE_REDRIVE_PROOF_DSN` and creates a unique schema in its fresh database. ACK/completion uses both `ESHU_POSTGRES_TEST_DSN` and `ESHU_POSTGRES_DSN` pointed at its own fresh database; the shared test helper creates a unique schema, applies the full bootstrap, and drops that schema on cleanup. Infra inventory uses `ESHU_POSTGRES_DSN`, bootstraps its fresh database, and some tests create and drop sibling databases. The disposable packages use `ESHU_TEST_CONTENT_INDEX_POSTGRES_*`, `ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_*`, and `ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_*` with each `*_DISPOSABLE` value set to `1`. Their admin DSNs target the same owned PostgreSQL service's `postgres` database. The peer-owned `code_topic_parallel_guarded_live_test.go` and four cost files with five tests remain excluded from this inventory. Claim uses `ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN`, `ESHU_REDUCER_FAIRNESS_PROOF_DSN`, and `ESHU_POSTGRES_DSN` in one fresh database; liveness uses `ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`; fanout uses `ESHU_REDUCER_FAIRNESS_PROOF_DSN` and `ESHU_POSTGRES_DSN`; search uses `ESHU_POSTGRES_DSN` with `ESHU_SEARCH_INDEX_PARTITION_LIVE=1`; readiness uses both `ESHU_REDUCER_FAIRNESS_PROOF_DSN` and `ESHU_POSTGRES_DSN` in one fresh database. These opt-ins preserve their existing test defaults. No new runtime telemetry was introduced because this batch changes only CI enrollment and proof metadata. No Go production code, SQL, golden snapshot, graph backend, or telemetry contract changed in this batch. The containerimage changes repair test setup only, as described below. The ACK live test now checks the canonical reducer claim rejection, exactly one dropped stale claim, `KeptCommitted`, and the surviving committed row with its lease cleared. The original assertion failed because it expected the older PostgreSQL wrapper error; the current typed error intentionally wraps the canonical reducer error. Freshness-links points `ESHU_POSTGRES_TEST_DSN` and `ESHU_RETENTION_TIMING_DSN` at its fresh database and `ESHU_CHANGED_SINCE_LINK_SCALE_DSN` at the fixture-loaded auxiliary database, with the bounded cost, timing, and scale env pins above; no new runtime telemetry was introduced.
