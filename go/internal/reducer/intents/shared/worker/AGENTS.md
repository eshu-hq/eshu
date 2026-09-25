# Agent instructions: internal/reducer/intents/shared/worker

Scoped rules for this directory. The root `AGENTS.md` and
`internal/reducer/sharedintent/AGENTS.md` still apply.

## What this package is

The shared-projection partition worker, runner, lease heartbeat, batch
selection, readiness/presence gating, and repo-refresh-fence machinery every
generic shared-projection domain is drained through (issue #6061). See
[doc.go](doc.go) and [README.md](README.md).

## Hard rules

**Never import the reducer root** (`internal/reducer`), directly or
transitively. This package exists so the root's domain families can import
it without a cycle; importing the root back defeats that.

**Concurrency changes here are queue-claim-semantics changes.** Any edit to
`SelectPartitionBatch`, `ProcessPartitionOnce`, the lease heartbeat, or the
repo-wide-retract fence must follow the root `AGENTS.md`'s "Change reducer
queue claim semantics" section: prove idempotency under duplicate claim or
partial failure.

**Keep the superseded-generation drain behind the readiness gate and limited
to BLOCKED rows in `SelectPartitionBatch` (#7121).** A generation superseded
before workload materialization ran never publishes its phase row, so its
blocked rows wait forever. Ready and terminal rows on a superseded generation
must keep projecting: a delta successor never re-emits an untouched file's
edge, so draining a ready row is permanent edge loss. Key the lookup on the
terminal `superseded` status only, never on "not the scope's active
generation" (that races activation), and look up only the blocked rows'
generation ids. The port is optional: a nil or
non-implementing reader must stay byte-identical, and a lookup error must
fail the selection rather than drop or keep rows.

**Do not reorder the heartbeat stop before the lease release in
`ProcessPartitionOnce`.** `stopHeartbeat()` must run first, and
`ReleasePartitionLease` must use the pre-heartbeat context (`releaseCtx`),
not the heartbeat-derived one `stopHeartbeat` cancels. Reordering silently
breaks lease release under the cancelled context.

**Keep exported names byte-identical when moving code here from the reducer
root.** The root aliases/forwards this package's exported surface under the
original spellings for backward compatibility (cross-package callers in
`internal/storage/postgres`, `internal/storage/cypher`, `cmd/reducer`, and
`internal/replay/offlinetier` reach several of them through `reducer.X`
directly). Renaming a symbol here breaks those aliases.

## Adding a new shared-projection domain

Follow the root `AGENTS.md`'s "Add a new reducer domain" section for the
handler side. To add the domain to the generic worker's drain set, add its
`Domain` constant to `sharedProjectionDomains` in `runner.go`, and add a
readiness-phase case in `ReadinessPhase` (`domains.go`) if
the domain must gate on a graph-projection phase before writing.

## Adding a repo-wide-retract domain

Add the domain to `sharedintent.DomainHasRepoWideRetract`'s map (in
`sharedintent/refresh.go`, not here) AND to `internal/storage/cypher`'s
`wholeScopeRetractDomains` table in the SAME change. A domain added to one
but not the other gets the #6166 over-delete: a whole-repository DELETE
bound to the batch-wide repository list.
