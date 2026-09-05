# #5167 — The Code-Reachability Index Migrations Replay On Every Bootstrap

`migrations/` has no applied-migration ledger, so a create paired with a later
drop is not a one-time upgrade cost — it is work every start of every install
repeats. This note is the record for migrations 101 and 102, the pair that
builds `code_reachability_entity_repository_scope_generation_idx` and drops the
two-column `code_reachability_entity_repository_idx` it supersedes. The read
those indexes serve, and its cost measurements, are in
[#5167 cross-repo hidden-consumer walk](5167-cross-repo-hidden-consumer-walk.md).

Root-Cause Evidence: the observation is a second pass over the shipped
definitions, watched per statement.
`TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive` reads every index
on `code_reachability_rows` and its `relfilenode` before and after each
statement of that second pass; against the released migration 100 it recorded a
concurrent rebuild and a drop of the same index name. The section below names
the code path that makes it unconditional, and
`TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay` generalises it.

No-Observability-Change: schema definitions only; no metric instrument, span,
log event or runtime knob changes with this pair.

## The Replay

Codex raised a P1 on the shape the two index migrations shipped in, and it is
right. `migrations/` has no applied-migration ledger. `BootstrapDefinitions`
reads `//go:embed migrations/*.sql`, builds one definition per file and sorts
them by path; `ApplyDefinitions` Execs every one of them, unconditionally, and
consults no database (`go/internal/storage/postgres/schema.go`).
`TestApplyBootstrapExecutesDefinitionsInOrder` already pins exactly that. Every
start of `bootstrap-data-plane`, `bootstrap-index` or the local supervisor
therefore replays the entire directory.

So a create paired with a later drop is not a one-time upgrade cost. Migration
102 clears `code_reachability_entity_repository_idx`, which means the next
bootstrap's `CREATE INDEX CONCURRENTLY IF NOT EXISTS` no longer skips: it builds
the index again, concurrently, over a populated `code_reachability_rows`, and
102 drops it again. Every startup, forever.

The fix is to delete the create rather than reorder it: there is no migration
100 any more, migration 101 builds the four-column index and migration 102 drops
the two-column one. A fresh install builds one index and drops nothing; an
install that already applied the released migration 100 builds the four-column
index once and drops the two-column one once; every bootstrap after that issues
neither. That is the shape this directory already uses —
`059_relationship_family_candidate_index.sql` creates the replacement under a
new name and `068_drop_relationship_family_candidate_index_legacy.sql` drops the
legacy one, and 068's own comment states the rule ("on every subsequent boot
... `DROP INDEX CONCURRENTLY IF EXISTS` is a no-op, so this adds no per-boot
churn"). Editing migration 100 in place was rejected: its filename and its
derived definition name would then name an index the file does not create, and
the drop would have to run ahead of the create, leaving a window with neither
index while the concurrent build runs.

Proof is two tests rather than an argument.
`TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive`
(`go/internal/storage/postgres/code_reachability_index_replay_live_test.go`,
`integration` tag) seeds a populated store, builds the released two-column index
on it so the fixture stands where those installs stand, runs the shipped index
definitions once to converge it, and then runs them again through a recorder
that reads every index on `code_reachability_rows` and its `relfilenode` before
and after each statement. The second pass must build nothing, drop nothing and
change no `relfilenode` — asserted per statement, so a definition whose effect
the next definition undoes fails even though the state either side of the whole
pass is identical.
`TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay`
(`go/internal/storage/postgres/schema_index_replay_test.go`) is the hermetic
half and generalises it: no bootstrap definition may create an index name any
definition drops.

That second test found one more, outside this change and pre-existing on `main`.
Migrations 069 and 077 both `CREATE INDEX CONCURRENTLY IF NOT EXISTS
fact_records_identity_epoch_idx` — the same name, different predicates — and 076
drops that name between them, so every bootstrap of every install drops and
rebuilds a `fact_records` index. Deleting a file cannot fix it, because the
replacement reuses the name and an install holding 069's definition is
indistinguishable from one holding 077's; converging them needs the replacement
renamed, which changes an index the container-image identity path is measured
against. It is recorded as the single explicit exception in that test rather
than silently allowed, so a second offender still fails and removing this one
fails until the exception goes with it.
