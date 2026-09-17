# #6738 Postgres bootstrap migration receipts

## Change and boundary

`eshu-bootstrap-data-plane` now records one receipt after each successful
Postgres SQL file. The receipt stores its path, full or deferred variant, and
SHA-256 checksum. A later run validates recorded checksums before pending DDL
and skips completed files. The existing connection advisory lock still
serializes bootstrap runners. An existing database without receipts must replay
the historical files once; no graph marker is treated as proof that those files
ran.

The deferred content path records migrations `004`, `057`, and `062` as deferred
because the latter two make conditional decisions before the content indexes
exist. A later full run revisits all three and verifies the exact three-index
ready state. The full-to-deferred path skips those files when full receipts
already exist.

## Performance Evidence

Performance Evidence: on one disposable PostgreSQL 18.6 database with the
current 129-file migration tree and **zero application rows**, the same Go test
measured the call boundary of `ApplyDefinitions` replaying the completed tree
at **66.657417 ms**. After the final runner created 129 receipts, an
`ApplyBootstrap` call over that schema took **12.488708 ms** and skipped all 129
files while a read-only transaction held `ACCESS SHARE` on
`scope_generations`. Both timings are single local samples with the same empty
schema and backend. The skip includes a catalog check for invalid concurrent
indexes. These are neither a throughput estimate nor a speedup claim for a
populated environment. The steady-state work is one ledger read of 129
rows, checksum comparison, and zero migration DDL. First application to an
untracked database still executes the historical SQL and can be costly.

The local baseline reproduced the lock failure: with a reader transaction on
`scope_generations`, replaying migration `002` with a 250 ms `lock_timeout`
returned `SQLSTATE 55P03`. The completed-ledger run under a held reader lock
passed. This proves the repeated-DDL failure mode and its local fix, not the
runtime of an initial replay on a large corpus.

## No-Regression Evidence

No-Regression Evidence: a fresh local database recorded 129 full receipts
and a second run reported `applied=0 skipped=129`. The terminal local counts
were 129 receipts, 0 `fact_work_items`, and 0 `scope_generations`; the empty
queue is a fixture property, not corpus-readiness evidence.

A recorded migration with a cancelled concurrent index build was RED because
`ApplyBootstrap` skipped the invalid index, then GREEN when the runner detected
and rebuilt it. A second RED test found that a failed repair retained its
success receipt. After clearing that receipt before the rebuild, the test
observed no receipt on failure, removed the invalid index to model a stop after
cleanup, and then rebuilt a valid index and restored its receipt on retry.
The documentation findings read and filter index restart tests
also passed. A schema with `search_path = isolated, public` was RED because it
borrowed the `public` ledger; after scoping receipts to `current_schema()`, it
recorded 129 isolated receipts and the original cloud reopen ordering test
passed. A separate test planted checksum drift and observed failure before
DDL. A later malformed migration preserved the earlier receipt; the corrected
retry then recorded both.
The deferred-to-full regression was RED when the entity-name GIN was absent,
then GREEN after the three dependent variants were tracked: the full run
reported `applied=3 skipped=126`, the lifecycle row was `ready`, and
`eshu_require_content_substring_indexes_ready()` returned true. A following
deferred run reported `applied=0 skipped=129`.

Focused commands after the final code edit:

```bash
cd go && go test ./cmd/bootstrap-data-plane ./internal/storage/postgres -count=1
cd go && go vet ./...
cd go && go test -tags integration ./internal/storage/postgres -run '^TestBootstrapDeferredContentMigrationUpgradesToFullLive$' -count=1 -v
```

Additional live PostgreSQL tests passed after the final code edit:
`TestBootstrapLedgerUsesCurrentSchemaLive`,
`TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive`,
`TestActiveOCIWarningIndexMigrationLifecycleLive`,
`TestDocumentationFindingsIndexRestartSafetyLive`, and
`TestBootstrapBinaryRecordsPostgresMigrationsLive`.

The integration tests used disposable PostgreSQL 18.6 databases supplied by
the documented test DSN variables. The command package's live adapter test also
recorded 129 receipts and verified that migration completion carries the
bootstrap runtime identity and `event_name` in JSON. The strict MkDocs build
and `git diff --check` passed. Hosted CI remains authoritative for its required
gates.

## Observability Evidence

Observability Evidence: the runner uses the bootstrap runtime structured
logger for each migration start and receipt, with path, variant, recovery flag,
position, total, `event_name`, and elapsed milliseconds. A completion log
records applied, skipped, total, and duration.
An error includes the failed migration name; no success receipt is written for
that file. These bounded logs let an operator distinguish a long first replay
from a skip-only restart and see which file stopped. No new metric label or
steady-state span was added.

## Ops-QA first rollout limit

The existing ops-qa database has no ledger and is about 92 GB. Its replay time
and data-transform cost have not been measured. Before running this version
there, preserve a recoverable database copy and rehearse the replay against it
with a 30-minute abort cutoff. Quiesce application reads and writes before the
live schema Job, verify receipts and a skip-only rerun, then restore workloads.
The cutoff is a decision point, not a completion estimate. A graph marker, a
synced Argo revision, and an empty local fixture do not establish that the
ops-qa corpus is ready. No live ops-qa mutation was performed for this note.
