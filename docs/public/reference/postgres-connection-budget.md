# Postgres Connection Budget (Deployed Shapes)

Size a deployed Postgres by the sum of configured pool maxima, not by the
steady-state connection count. Steady state lies: pools open connections
lazily and reap idle ones, so a healthy fleet sits far below its ceiling and a
simultaneous restart of every workload can still exhaust the server with
`SQLSTATE 53300` (`remaining connection slots are reserved`) the moment
coincident demand fills those pools at once. Refs #11301.

For the local Compose and embedded shapes, where the server ceiling is derived
from the same knob, see [Postgres Tuning](postgres-tuning.md). This page covers
deployed servers whose `max_connections` is fixed independently of the
workloads (chart defaults, operator-run primaries).

## The burst inequality

Every Eshu process that opens Postgres bounds one pool at
`ESHU_POSTGRES_MAX_OPEN_CONNS` (default `30`,
`go/internal/runtime/data_stores.go`). The reducer opens one more connection
from a dedicated one-connection pool while its secret-lines repair holds the
rebuild lock. Size the server so the simultaneous worst case fits:

```text
sum(writer-facing share of configured ESHU_POSTGRES_MAX_OPEN_CONNS)
  + reducers * 1 (secret-lines lock pool)
  + reserved (superuser_reserved_connections, operator psql, admin-status)
  <= Postgres max_connections
```

The writer-facing share is the full cap for single-pool processes and the
writer half for reader/writer-split ones (an omitted
`ESHU_POSTGRES_READ_MAX_OPEN_CONNS` defaults to half the cap).

A rolling restart of all workloads at once is the demand spike to size for:
every pool re-establishes toward its maximum within the same window. Steady
state passing this check proves nothing; only the configured maxima count.

## Census recipe (read-only)

Run these against the live fleet; none of them writes:

```console
$ psql -tc "SHOW max_connections; SHOW superuser_reserved_connections;"
$ psql -c "SELECT count(*) FROM pg_stat_activity \
    WHERE backend_type = 'client backend';"
$ kubectl get deploy,sts -o json | python3 -c \
    "import json,sys; [print(i['metadata']['name'], i['spec'].get('replicas')) \
    for i in json.load(sys.stdin)['items'] \
    if any('DSN' in (e.get('name') or '') \
    for c in i['spec']['template']['spec']['containers'] for e in c.get('env', []))]"
```

The first two give the server ceiling and current use. The third lists every
pool-holding workload; read each one's `ESHU_POSTGRES_MAX_OPEN_CONNS` (absent
means the `30` default) and add the terms. Attribute live backends per workload
by grouping `pg_stat_activity` on `client_addr` and joining to pod IPs, but
treat that as a floor: a workload holding 1 connection now can still demand its
full maximum during a burst.

## Worked shape (from #11301)

A deployed fleet with `max_connections = 100` and 3 reserved slots serves 97
non-superuser connections. Its census showed about 32 pool-holding processes:
most at the `30` default, two reducers capped lower at `12` and `20`.
Reader/writer-split processes (API, core, MCP, both reducers) halve that cap
between the primary writer pool and the replica reader pool
(`go/internal/runtime/postgres/config.go`), so the primary-facing worst case
is roughly `27 * 30 + 3 * 15 + 6 + 10 + 2 ≈ 873` against the 97 budget, while
steady state held only about 61 backends (36 free). Two restart bursts that
month exhausted the budget with `53300` once coincident demand filled the
pools; a later simultaneous restart of the five heaviest workloads produced no
error because lazy pools never filled together. The budget, not any single
workload, is the defect: no pool is misconfigured, their sum simply does not
fit.

## Remedies

Pick one, in this order, and re-run the census after it:

1. **Lower per-workload maxima** to what each workload's measured peak needs.
   Collectors that hold 1-2 connections steady do not need the `30` default;
   set the knob per workload in the deployment values. This is the only remedy
   that shrinks the burst term at its source.
2. **Raise `max_connections`** after checking memory. Each extra connection
   costs shared memory plus up to `work_mem` per active sort; on a primary with
   tens of GiB this is usually cheap, but `max_connections` is fixed when the
   postmaster starts, so changing it needs a primary restart (and every
   replica).
3. **Add a measured pooling layer** (for example PgBouncer in transaction
   mode) between the fleet and the primary when neither knob above closes the
   gap. Measure under the real restart burst; a pooler that is never observed
   at burst load is a guess.

Do not fix this by staggering restarts or by serializing pool fill: that hides
the contention instead of removing it, and the next coincident peak fails the
same way.

## Acceptance evidence

A budget fix is done only when a post-change reading shows free slots above a
stated floor through a rolling restart of every workload at once, with zero
`53300` in any workload log across the whole window. Record the configured
maxima, their sum, the floor, and the reading in the change; the number stays
written down with the deployment values, not in a comment thread.
