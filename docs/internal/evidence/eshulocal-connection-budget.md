# Embedded local Postgres connection budget (#4456 extended to `eshulocal`)

## Defect

`internal/eshulocal/postgres_unix.go` started the embedded local Postgres with a
bare literal:

```go
"max_connections": "35",
```

The #4456 runtime invariant is:

```text
max_connections >= (pool-holding services) * ESHU_POSTGRES_MAX_OPEN_CONNS
                   + reserved/admin headroom
```

`internal/runtime` enforces this for every Compose stack in
`TestComposePostgresMaxConnectionsCoversPoolBudget`, where the default ceiling is
640. The embedded local server was never covered by that test and sat below the
budget for **even a single pool-holding process**:

| term | value | source |
|---|---|---|
| per-process pool ceiling | 30 | `runtime.defaultPostgresMaxOpenConns`; `cmd/bootstrap-index` defaults to 30 too |
| reserved/admin headroom | 20 | `reservedAdminConnections` in the compose budget test |
| minimum for one pool holder | **50** | 1 × 30 + 20 |
| worst-case local demand | **170** | 5 × 30 + 20 |
| shipped local ceiling | **35** | the literal above |

No local override lowers the pool: `runtime.LoadPostgresConfig` returns 30 for
every process unless `ESHU_POSTGRES_MAX_OPEN_CONNS` is set, and nothing in
`internal/eshulocal` or the `cmd/` entrypoints reduces it for local profiles.

**Consequence.** The local supervisor alone runs three pool-holding children
concurrently. Three such processes want 90 connections against a server that
admits 35 (32 usable: `superuser_reserved_connections` is 3), so the local stack can exhaust connections ("too many clients
already") and refuse the operator's own diagnostic session — the exact failure
the reserved headroom exists to prevent.

## Fix

`max_connections` is now derived from an **enumerated list** of pool holders
rather than a hand-entered count:

```text
5 pool-holding services * 30 per-process pool + 20 reserved/admin = 170
```

The five are `localPostgresPoolHolders`, and the ceiling is a compile-time
`len()` of it, so the number cannot drift from the list.

**An earlier revision of this change said three, and that was wrong in
composition as well as size.** It assumed API + MCP + an indexer. Verified from
source: `internal/cli/localsupervisor/host.go` starts `eshu-reducer`,
`eshu-ingester` and `eshu-mcp-server` as supervised children -- the API is not a
supervised child at all -- and `eshu vuln-scan repo` attaches to a running owner
and launches a short-lived `eshu-api` plus `eshu-bootstrap-index` against that
owner's Postgres (`cmd/eshu/gotchas-read-surface-commands.md`). Worst case is
therefore five concurrent holders, not three, and 110 would still have been
under-provisioned by 60 connections.
The figures are duplicated in `internal/eshulocal` rather than imported so the
package keeps its dependency-free ownership boundary (it imports no other
internal package, and `internal/runtime` does not depend on it — verified with
`go list -deps ./internal/runtime`, which returns no `eshulocal`).

Scope note: no new environment knob. Compose reads `ESHU_PG_MAX_CONNECTIONS`;
the embedded server deliberately stays derived, so the budget cannot be lowered
below the invariant by configuration. Adding a local override is a separate
change.

## Proof

RED/GREEN on the guard, run through the machine-wide build mutex, worktree
`pgconns` on base `c74d5b6c5`:

RED, constant pinned back to the shipped `35`:

```text
$ go test ./internal/eshulocal/ -run 'LocalPoolBudget|DerivedMaxConnections' -count=1
--- FAIL: TestEmbeddedPostgresMaxConnectionsCoversLocalPoolBudget (0.00s)
    postgres_unix_test.go:85: LocalPostgresMaxConnections = 35, want >= 110
        (3 pool-holding services * 30 per-process pool + 20 reserved/admin)
FAIL	github.com/eshu-hq/eshu/go/internal/eshulocal	0.265s
RED-EXIT=1
```

GREEN, constant derived, whole package:

```text
$ go test ./internal/eshulocal/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/eshulocal	0.801s
GREEN-EXIT=0
```

The RED fails on the **value** (`35, want >= 110`), not on a missing symbol or a
compile error, so it is a real regression proof rather than a test that is red
for the wrong reason.

### Second round: the guard the first round did not have

Independent review found the first round's guard was a **tautology**. It
compared `LocalPostgresMaxConnections` against `holders*perProcess+reserved`,
but the constant is *defined* as that product, so the only failure it could
detect was someone substituting a literal -- which is exactly what the RED did.
It could not detect the thing that actually goes wrong: the holder count falling
behind the code that starts those processes.

That mattered, because **the count was wrong**. See the correction above: five
holders, not three.

The replacement guard pins the enumerated list by name. Mutation-proved by
trimming it back to the old wrong count:

```text
$ go test ./internal/eshulocal/ -run 'PoolHolders|LocalPoolBudget|DerivedMaxConnections' -count=1
--- FAIL: TestLocalPostgresPoolHoldersCoverEveryLocalProcessThatOpensAPool
    localPostgresPoolHolders is missing "eshu-api"; it holds
    [eshu-reducer eshu-ingester eshu-mcp-server]. Every process that opens a
    pool against the embedded server must be listed, because the ceiling is
    derived from len()
MUTANT-EXIT=1

$ (list restored, byte-identical) go test ./internal/eshulocal/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/eshulocal	0.302s
GREEN-EXIT=0
```

So a future edit that shortens the list to make a budget complaint go away fails
loudly, and one that adds a local pool holder raises the ceiling in the same
edit.

The RED step is the mutation test for the guard: it proves
`TestEmbeddedPostgresMaxConnectionsCoversLocalPoolBudget` actually rejects an
under-budget ceiling rather than passing vacuously.

`TestEmbeddedPostgresConfigCarriesDerivedMaxConnections` additionally pins the
start parameters to the derived constant, so the ceiling cannot be raised while
the running server keeps a stale literal.

`TestStartEmbeddedPostgresBootstrapsThroughForkedDriverLive` (gated behind
`ESHU_EMBEDDED_POSTGRES_LIVE`) now asserts `SHOW max_connections` equals the
derived value, so the proof covers the postmaster actually booting with it and
not merely a config struct carrying it.

Performance Evidence: raising a Postgres server's `max_connections` increases
allocated shared memory, which is why the change is bounded to 110 rather than
matching Compose's 640. 110 sits just above PostgreSQL's own default of 100 --
the same scale this repo already reasons about elsewhere (`gated_writer.go`
works from `max_connections=100` defaults) -- so the added shared memory is the
increment from 35 to 110 on a server that a stock PostgreSQL install would have
provisioned for 100 anyway. The embedded server keeps the library's default
`shared_buffers`. The touched path is process startup, not a
query path: no hot-path Cypher, SQL, reducer projection, or queue behaviour
changes, and no statement plan is affected.

No-Regression Evidence: the change alters one Postgres start parameter and adds
three test assertions. No production code path other than
`embeddedPostgresConfig` is touched, and the value it produces is a compile-time
constant, so there is no added per-request or per-item work to measure.

No-Observability-Change: this change adds no runtime signal and removes none.
The ceiling is a start parameter visible through `SHOW max_connections` on the
local server and through the workspace `postgres.log` the package already routes
startup output to; connection exhaustion continues to surface as the Postgres
"too many clients already" error on the existing pool paths.
