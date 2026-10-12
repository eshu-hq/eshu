<!-- docs-catalog
title: Park-Holder Runbook
description: Hold and hand over the package-manifest backfill park without pinning the vacuum horizon, and clear the schema Job xmin precheck.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# Park-holder runbook

The park is an operator-held session advisory lock, key `(5318, 7088)`, that
keeps the reducer's package-manifest backfill from running passes. Each pass
starts by trying that lock
(`go/cmd/reducer/package_manifest_backfill.go`); while the park is held the
pass reports `contended` and retries on the next poll. Operators park the
backfill because a pass can hold one transaction for an hour on a large scope
(#7663), and that window is a bad time to start other sensitive work.

A park holder must be an idle session. A holder shaped as a long statement
pins the vacuum horizon for the whole database and stalls any `CREATE INDEX
CONCURRENTLY` migration behind it. That shape caused #7791, and this page is
the procedure that replaces it.

## What went wrong

On 2026-10-08 the schema Job applied migration 164, a `CREATE INDEX
CONCURRENTLY` on `content_entities`. The build took 678.6 s, almost all of it
in the `waiting for old snapshots` phase, blocked by one backend: the operator
session holding the park.

That session was `psql -c "SELECT pg_advisory_lock(5318, 7088); SELECT
pg_sleep(86400);"`. The lock call and the sleep run in one implicit
transaction, so the session held one open transaction for 24 h. The writer
showed:

- `pg_stat_progress_create_index`: phase `waiting for old snapshots`,
  `current_locker_pid` = the park holder.
- `pg_blocking_pids` of the index build = the holder; wait event `Lock` /
  `virtualxid`.
- The holder's `backend_xmin` was about 1.67 M transactions old (about 34 h).
  That pins the vacuum horizon for the whole database for as long as the
  holder lives, not only for the index build.

Cancelling the holder's statement would have ended the `psql` process and
dropped the session lock, so the park would have been lost. The fix was a
waiter-first handover, described below.

## Hold the park with an idle session

Start the holder as a pipe whose only statement is the lock call:

```bash
(echo "SELECT pg_advisory_lock(5318,7088);"; exec sleep infinity) \
  | psql "host=<writer> dbname=<db> user=<user> application_name=park-holder" \
    -v ON_ERROR_STOP=1
```

The lock statement runs in autocommit, so the session then holds no
transaction and no snapshot, and `sleep infinity` gives it no timer. If the
statement ever fails or is cancelled, `ON_ERROR_STOP=1` exits `psql` instead
of leaving an idle session that no longer holds the lock. Re-run the command
and verify if that happens.

Verify the holder from another session:

```sql
SELECT l.pid, a.application_name, a.state, a.backend_xmin, a.xact_start
FROM pg_locks l JOIN pg_stat_activity a USING (pid)
WHERE l.locktype = 'advisory' AND l.classid = 5318 AND l.objid = 7088
  AND l.granted;
```

You want one row with `state = idle`, `backend_xmin IS NULL`, and `xact_start
IS NULL`. The schema Job's xmin precheck ignores a holder shaped this way.

To release the park, end the `psql` process (Ctrl-C, or kill the `sleep`). The
session lock drops with the session, and the next reducer poll takes the
backfill lock and runs a pass.

## Hand over a held lock

Never cancel a statement-shaped holder directly: its `psql` exits and the
park is lost. Start the replacement as a waiter first, then cancel the old
one. Postgres hands the lock to the queued waiter on release. A measured
handover on PG 18 showed the waiter granted with the old session gone and no
gap across 600 concurrent try-lock attempts.

1. Start the new idle holder from the previous section with a distinct
   `application_name`, for example `park-holder-new`. Its lock statement
   blocks behind the old holder.
2. Confirm the waiter is queued:

   ```sql
   SELECT l.pid, a.application_name, a.state, a.wait_event_type, a.wait_event
   FROM pg_locks l JOIN pg_stat_activity a USING (pid)
   WHERE l.locktype = 'advisory' AND l.classid = 5318 AND l.objid = 7088
     AND NOT l.granted;
   ```

   You want one row for `park-holder-new` with `wait_event_type = Lock`.
3. Cancel the old holder's statement: `SELECT
   pg_cancel_backend(<old-pid>);`. Its `psql` exits, its session ends, and the
   waiter is granted.
4. Re-run the holder query from the previous section. You want one granted row
   for `park-holder-new`, and no session left for the old holder. Check
   promptly: the reducer polls the lock every second while the backfill is
   incomplete, so a lost park is picked up fast.

## When the schema Job refuses to start

The schema bootstrap Job runs an `xmin-precheck` init container before the
migration container. It lists sessions in the target database that hold a
`backend_xmin` older than 5 minutes and refuses to start while any exists. The
check is per-database: an old snapshot in another database of the same cluster
does not block a concurrent build (measured on PG 18), so it is ignored.

A refusal looks like this in the init container log:

```text
xmin-precheck: refusing to start schema bootstrap: 1 session(s) in database "eshu" hold backend_xmin older than 5 minutes. A CREATE INDEX CONCURRENTLY build would stall in "waiting for old snapshots" until they end; see the park-holder runbook (docs/public/operate/park-holder-runbook.md). Holders, oldest first:
pid=118 state=idle in transaction xact_start=2026-10-08 19:02:11.004+00 query=SELECT pg_sleep(86400);
```

A pass looks like this:

```text
xmin-precheck: no backend_xmin holder older than 5 minutes; starting schema bootstrap
```

What to do depends on which session the refusal names:

| Holder | Action |
| --- | --- |
| A stray `psql` or a statement-shaped park holder | End the session, or hand the park to an idle holder with the procedure above. |
| The reducer backfill mid-pass (#7663) | Its hour-long transaction blocks a concurrent build exactly like a park holder, so the refusal is correct. Wait for the pass to commit, or scale the reducer to 0, run the Job, and scale back. Cancelling the pass without scaling down only buys 30 s: the next poll retries. |
| An unknown application session | Find its owner from `usename` and `application_name` before touching it. |

Then re-run the Job. The precheck fails closed: if the database is
unreachable, the Job fails with the connection error instead of starting the
migrations blind.

## Diagnose a stalled concurrent build

If a build is already running and slow, check whether it is waiting on old
snapshots:

```sql
SELECT pid, phase, lockers_total, lockers_done, current_locker_pid
FROM pg_stat_progress_create_index;
```

A row in phase `waiting for old snapshots` with a `current_locker_pid` names
the blocking transaction. Confirm with `SELECT
pg_blocking_pids(<build-pid>);` and inspect the blocker's `backend_xmin` and
`xact_start` in `pg_stat_activity`.

A `CREATE INDEX CONCURRENTLY` statement is exempt from the migration lock
retry budget and waits instead of retrying (#7004): retrying would restart its
table scan from zero every attempt. The wait is not bounded by the Job's
`activeDeadlineSeconds` either: the deadline kills the bootstrap client only,
while the backend keeps building. If the deadline fires first, the index stays
INVALID. The next schema apply drops an invalid index by name before retrying
the build, so restart promptly rather than leaving it in place. See
[Schema bootstrap](../deployment/service-runtimes-bootstrap.md) for the full
migration wait contract.

## Compare migrations from the running image

The #7791 Job applied migration 163 as well as 164, because the running image
carried both while the fix PR only added 164. When a Job behaves
unexpectedly, compare against the image it ran, not against the commits you
expect.

1. Which image did the Job run?

   ```bash
   kubectl -n <namespace> get job \
     -l app.kubernetes.io/component=schema-bootstrap \
     -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'
   ```

2. Which migrations did that run apply?

   ```bash
   kubectl -n <namespace> logs \
     -l app.kubernetes.io/component=schema-bootstrap \
     | rg 'migration\.(applying|recorded)'
   ```

   Each event carries the migration `path`.
3. Which migrations has the database recorded, and when?

   ```sql
   SELECT path, variant, applied_at FROM eshu_schema_migrations ORDER BY path;
   ```

4. Which migrations does that image carry? Migrations are embedded in the
   binary, so resolve the image tag to its git ref and list
   `go/internal/storage/postgres/migrations/` at that ref. A migration present
   in the
   image but absent from the ledger is one the next Job run will apply.

## Related pages

- [Schema bootstrap](../deployment/service-runtimes-bootstrap.md) — the
  migration wait, retry, and advisory-lock contract the precheck sits in front
  of.
- [Health Checks](health-checks.md) — process health, readiness, and
  completeness.
- [Troubleshooting](troubleshooting.md) — common symptoms and their first
  checks.
