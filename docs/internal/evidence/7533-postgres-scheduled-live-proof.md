# #7533: scheduled PostgreSQL live proof enrollment

The `postgres-scheduled` runner owns 182 exact named tests in 97 live files.
The same job runs daily at 04:35 UTC, manually, and for pull requests to
`main` changing its enrolled inputs. It uses one digest-pinned PostgreSQL 18.6 service.

Crossplane, infra inventory, ACK/completion, claim, liveness, fanout, search,
readiness and containerimage use separate fresh databases. The three disposable
groups use an administrative DSN to `postgres`; their shared helper creates a
random database from template0 and cleans it up. These rows have class
`verified_scheduled` and leave the legacy exemption inventory.

The exact hosted PR job must pass before enrollment merges. It is not a
universally required PR or merge check while cost probes lack repeated timing
evidence. Actual cron delivery remains NOT_CHECKED.

## Local enrollment proof

The source baseline was `eac0860933e4124400a7e59056f58954346d4c32`. Go was `go1.26.9 linux/arm64`; PostgreSQL was image `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`. Each package ran serially with `go test -json -race -p=1 -count=1` and an anchored selector of its exact named tests. The first five groups originally used source files from that baseline; the infra inventory group has the later test-only repairs described below. The ACK group updates one stale assertion to the current typed supersession contract. The five additional groups have unchanged source bytes from the owned five-database PostgreSQL 18.6 proof on parent `8e52aa782a892fc0a86a32fc8898a5405f552ce6`.

| Package | Files | Tests | Result | Process wall time |
| --- | ---: | ---: | --- | ---: |
| `./internal/storage/postgres/crossplane` | 5 | 9 | Nine run and pass events; package pass | 45.186s |
| `./internal/storage/postgres/infra/inventory` | 19 | 32 | 32 run and pass events; package pass | 109.448s |
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

No selected test or subtest skipped or failed. The first five local Go JSON streams are `/tmp/eshu-7533-proof/{crossplane-full,infra_inventory-full,cmd-golden-corpus-gate-disposable-pg,internal-query-disposable-pg,internal-storage-postgres-disposable-pg}.jsonl`; the coordinator recorded exact commands and outcomes in `pg-full-results.json` and `disposable-pg-results.json`. The query stream's 33.881s process wall time belongs to an enclosing ten-test run that also included the excluded peer test. Its events prove the selected nine-test subset passed, not its isolated cost. The ACK stream is `/tmp/eshu-7533-ack-full.jsonl`; its command, exit, and process wall time are in `/tmp/eshu-7533-ack-full-result.json`. The baseline stream `/tmp/eshu-7533-proof/ack-family.jsonl` had 42 passing tests and one failing stale supersession assertion before the test-only correction. The listed times are observed enrollment costs on a shared host, not a controlled speed or budget comparison. The versioned inventory index lists twelve closed JSON fragments. The manifest checksum binds the index bytes, each listed path, and every fragment byte. The committed inventory checks every Test name against source and workflow selectors; the event verifier rejects missing, duplicate, skipped, failed, malformed, or wrong-package results. The scheduled job runs negative harness tests before the packages.

The combined worktree replay on parent `8e52aa782a892fc0a86a32fc8898a5405f552ce6` ran the exact 43 ACK tests with `-race -p=1 -count=1` against a fresh owned PostgreSQL 18.6 database. It exited 0; the strict event check found 43 named run/pass pairs, package pass, and no selected skip or failure. The stream and command receipt are `/tmp/eshu-7533-pg57-ack-final.jsonl` and `/tmp/eshu-7533-pg57-ack-final-result.json`. Its 915.915s process wall includes cold race compilation in a new private Go cache, so it is not comparable to the earlier 159.951s ACK run or a controlled budget result. The original local 167-test bundle combines those six passing streams with the five passing streams from `/tmp/eshu-7533-isolation-{claim,liveness,fanout,search,readiness}.jsonl`. The five-group command and result manifest is `/tmp/eshu-7533-isolation-groups-results.json`; its receipt records 71 exact passing tests on five fresh databases in 458.455s total wall. Those group process times are observations on a shared host, not controlled speed comparisons. The local 167-test bundle labels its synthetic run metadata `local_simulation`; its artifact location is retained in the session receipt.

## First hosted attempt and corrected infra inventory proof

The first hosted PostgreSQL attempt failed its infra inventory group because
`TestRetentionLiveLockHoldAndDeriveWaitCost` reused one 90-second database
context across three complete rounds. It logged two successful rounds and
expired during the third. The corrected test gives each unchanged 50,000-row
round an independent 90-second context, joins its lock observer and retention
worker before returning, and keeps the 7-minute package timeout. A local race run exposed a fixture shutdown race in
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

## Hosted proof retention

Every hosted attempt writes `manifest.json` with the run URL, ID, attempt, event, reviewed PR head SHA, actual checkout SHA, event SHA, inventory and selected-source checksums, pinned image, and observed PostgreSQL version identity. A final step rechecks that manifest and all twelve Go JSON streams. The workflow uploads them together as a 14-day artifact, including after a failing proof step. The workflow contract pins every executable step, including checkout, Go setup, module prewarm, manifest creation, database bootstrap, proof commands, and bundle verification. Each Go proof step checks staged, unstaged, and untracked changes across the full Git checkout, including SDK inputs and a root workspace immediately before its test command and after event validation; it also refuses shell `GOFLAGS` or `GOWORK`, persistent `GOFLAGS` from `GOENV`, and an active ancestor or ignored `go.work`. This binds tested source bytes to the event checkout after module prewarm and between package groups. Before merge, record the passing PR run and artifact URL and verify its reviewed head and checkout SHAs. The local manifest and bundle check used synthetic run metadata with the owned PostgreSQL 18.6 fixture and retained Go streams. Its manifest says `local_simulation`; the hosted verifier rejects that origin, and GitHub Actions forbids the local allowance. This local check is not hosted execution evidence.

## Boundaries

Crossplane uses `ESHU_CROSSPLANE_REDRIVE_PROOF_DSN` and creates a unique schema in its fresh database. ACK/completion uses both `ESHU_POSTGRES_TEST_DSN` and `ESHU_POSTGRES_DSN` pointed at its own fresh database; the shared test helper creates a unique schema, applies the full bootstrap, and drops that schema on cleanup. Infra inventory uses `ESHU_POSTGRES_DSN`, bootstraps its fresh database, and some tests create and drop sibling databases. The disposable packages use `ESHU_TEST_CONTENT_INDEX_POSTGRES_*`, `ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_*`, and `ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_*` with each `*_DISPOSABLE` value set to `1`. Their admin DSNs target the same owned PostgreSQL service's `postgres` database. The peer-owned `code_topic_parallel_guarded_live_test.go` and four cost files with five tests remain excluded from this inventory. Claim uses `ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN`, `ESHU_REDUCER_FAIRNESS_PROOF_DSN`, and `ESHU_POSTGRES_DSN` in one fresh database; liveness uses `ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`; fanout uses `ESHU_REDUCER_FAIRNESS_PROOF_DSN` and `ESHU_POSTGRES_DSN`; search uses `ESHU_POSTGRES_DSN` with `ESHU_SEARCH_INDEX_PARTITION_LIVE=1`; readiness uses both `ESHU_REDUCER_FAIRNESS_PROOF_DSN` and `ESHU_POSTGRES_DSN` in one fresh database. These opt-ins preserve their existing test defaults. No new runtime telemetry was introduced because this batch changes only CI enrollment and proof metadata. No Go production code, SQL, golden snapshot, graph backend, or telemetry contract changed in this batch. The containerimage changes repair test setup only, as described below. The ACK live test now checks the canonical reducer claim rejection, exactly one dropped stale claim, `KeptCommitted`, and the surviving committed row with its lease cleared. The original assertion failed because it expected the older PostgreSQL wrapper error; the current typed error intentionally wraps the canonical reducer error.
