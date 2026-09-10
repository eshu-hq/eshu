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

The five are `localPostgresPoolHolders`, and the number is a compile-time
`len()` of it, so it cannot drift from the list.

**Read 170 as a FLOOR, not as the ceiling.** The section "The ceiling follows
the configured pool" below supersedes this: `ResolveLocalPostgresMaxConnections`
returns `max(170, holders * configured_pool + reserved)`, so 170 is what the
default configuration yields, not a fixed cap.

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

## Known limit: the supervisor's own pools are unbounded

The enumerated holder list implies a completeness it cannot have. The local
supervisor opens connections with a bare `sql.Open("pgx", dsn)` and never calls
`runtime.ConfigurePostgresPool` — `config.go:216`,
`content_search_indexes.go:42`, `iac_reachability_finalizer.go:28`,
`progress.go:28`. `database/sql` defaults `MaxOpenConns` to **0, i.e.
unlimited**, and the first two are long-lived for a whole authoritative run.

Verified: `rg -c ConfigurePostgresPool internal/cli/localsupervisor/*.go`
returns nothing.

So **no finite ceiling is strictly sound** until those are bounded. This change
moves the local server from "guaranteed exhaustion at shipped defaults" to
"covers every capped holder", which is an improvement rather than a proof, and
the #4456 gap remains open in the supervisor. Stating it here rather than
letting the list read as exhaustive.

## The ceiling follows the configured pool, not just the default

Found in review of this change (#6603), and it invalidated the first version of
the fix.

`localsupervisor.ChildEnv` ends in
`procexec.MergeEnvironment(procexec.Environ(), values)` and does **not** set
`ESHU_POSTGRES_MAX_OPEN_CONNS`, so every supervised child inherits the
operator's value verbatim and sizes its own pool from it through
`runtime.LoadPostgresConfig`. The knob is documented and explicitly tunable
upward: `docs/public/reference/postgres-tuning.md:42` tells operators to raise
it "when workers are blocked waiting for DB connections", and line 64 states the
invariant as `sum(pool-holding services * ESHU_POSTGRES_MAX_OPEN_CONNS)`.

A ceiling that hard-codes the default 30 therefore **contradicts the repo's own
documented invariant** the moment the knob is raised. Five holders at 60 want
320 connections against a fixed 170 — the same exhaustion this budget exists to
prevent, reachable through a supported configuration.

`ResolveLocalPostgresMaxConnections` now reads the resolved value:

```text
max(LocalPostgresMaxConnections, holders * resolved_pool + reserved)
```

Configuration can raise the ceiling and can never lower it, which keeps the
scope note above true: the budget cannot be reduced below the invariant by
environment. Parse rules mirror `runtime.LoadPostgresConfig` — unset means the
default; non-positive and unparseable values are not honoured. This function
does not report a bad value because it sits on a config-construction path with
no error return, and the child reading the same variable through
`runtime.LoadPostgresConfig` still fails loudly on it, so the error surfaces
there rather than being swallowed.

Mutation-proved, worktree `pgconns`. Unlike the live run recorded further
down, these two mutants ARE reproducible at this head: the resolver and both
guards exist here, so the transcript below can be regenerated rather than
taken on trust.

```text
$ (resolver mutated to ignore the env knob)
  go test ./internal/eshulocal/ -run 'ResolveLocalPostgres' -count=1
--- FAIL: TestResolveLocalPostgresMaxConnectionsCoversTheConfiguredPoolBudget
    ESHU_POSTGRES_MAX_OPEN_CONNS=45: ceiling 170 is below the pool budget 245
    (5 holders * 45 + 20 reserved)
MUTANT-A-EXIT=1

$ (call site reverted to the fixed constant)
  go test ./internal/eshulocal/ -run 'DerivedMaxConnections' -count=1
--- FAIL: TestEmbeddedPostgresConfigCarriesDerivedMaxConnections
    start parameters do not carry
    strconv.Itoa(ResolveLocalPostgresMaxConnections(os.Getenv))
MUTANT-B-EXIT=1

$ (both restored) go test ./internal/eshulocal/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/eshulocal	1.513s
RESTORED-EXIT=0
```

Both mutants fail on the **value**, not on a missing symbol, so the guards
reject the real defect rather than passing vacuously.

### Proven against a live postmaster at a non-default pool

The unit tests prove the arithmetic. This proves the server honours it:

```text
$ ESHU_EMBEDDED_POSTGRES_LIVE=1 ESHU_POSTGRES_MAX_OPEN_CONNS=60 \
    go test ./internal/eshulocal -run 'Live' -count=1
--- PASS: TestStartEmbeddedPostgresBootstrapsThroughForkedDriverLive (5.55s)
LIVE60-EXIT=0
```

A real postmaster booted with `max_connections = 320` (5 x 60 + 20) and
reported it back through `SHOW max_connections`.

Negative control, the same environment with the assertion pinned back to the
floor constant:

```text
embedded_postgres_driver_live_test.go:107:
  SHOW max_connections = 320, want 170 (the resolved local pool budget)
CONTROL-EXIT=1
```

That failure is the point: the previous assertion compared against
`LocalPostgresMaxConnections`, so it was environment-dependent the moment the
server started following the configured pool. Comparing against the resolved
value is what makes this test correct rather than incidentally passing.

## Known limit: the derived ceiling has no upper bound

`ResolveLocalPostgresMaxConnections` multiplies the configured per-process pool
by the holder count and applies no upper bound, so the `max_connections` the
embedded postmaster starts with scales without limit as
`ESHU_POSTGRES_MAX_OPEN_CONNS` grows. That much is read from the resolver.

What happens at the top of that range is NOT measured here. The largest value
this note boots is 60, giving `max_connections = 320` above; nothing on this
branch establishes where a larger value stops working, or how it fails when it
does. `postgres-tuning.md` documents a default of 30 and the direction "do not
raise `ESHU_POSTGRES_MAX_OPEN_CONNS` beyond Postgres server headroom", and names
no maximum, so there is no documented envelope to place a given value outside of.

Recorded as a known limit rather than capped here: choosing the cap is a product
decision about the supported range of that knob, not a correction to this
budget.

## Known limit: the list counts ROLES, not process instances

Found in review of this change, and it is the sharper of the two limits.

`localPostgresPoolHolders` budgets **one** `eshu-mcp-server`. That is wrong for
attached MCP: `RunAttachedMCPStdio` starts its child at

    StartChildProcess("eshu-mcp-server", []string{"eshu-mcp-server"}, ChildEnv(dsn, ...))

— the last child-starting statement of `RunAttachedMCPStdio` in
`go/internal/cli/localsupervisor/host.go` — with no deduplication, no
admission control and no cap on concurrent instances. The guards above it check
the owner record, workspace match, process liveness and socket health — none of
them limits how many MCP children exist. So **N concurrent `eshu mcp start`
attachments open N pools of up to 30**, and the derived 170 under-counts by
`30 x (N-1)`. Two attached sessions already exceed the budget's assumption.

The same shape applies to any other role a user can start more than once
concurrently. Review named a second instance: concurrent `eshu vuln-scan repo`
invocations each start their own `eshu-api` child
(`startLocalAPI` -> `localsupervisor.StartChildProcess("eshu-api", ...)` in
`go/internal/cli/vulnscan/localruntime.go`) plus a bootstrap-index pass, so
two concurrent scans double those two holders the same way two attachments
double the MCP one.

Deriving the ceiling from the configured pool (previous section) raises the
number but does **not** close this: the multiplier is the count of concurrent
*instances*, which nothing here observes. Bounding it properly still means
capping concurrent children or sizing from an observed child count.

This is not fixed here, and the number is deliberately not inflated to guess at
N — an arbitrary multiplier would be a worse claim than a stated limit. The
honest reading of `LocalPostgresMaxConnections` is: *the budget for one instance
of each capped role, plus reserve*. It is a floor for the single-session case,
not a ceiling for every concurrent workflow.

Bounding this properly means either capping concurrent attached MCP children or
sizing the server from the observed child count; both are larger than this
change and belong to the open #4456 work.

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
`ESHU_EMBEDDED_POSTGRES_LIVE`) asserts `SHOW max_connections` equals the derived
value, so the proof covers the postmaster actually booting with it and not
merely a config struct carrying it.

**That test has been run at the shipped value, and it passed:**

```text
$ ESHU_EMBEDDED_POSTGRES_LIVE=1 go test ./internal/eshulocal -run 'Live' -count=1 -v
--- PASS: TestStartEmbeddedPostgresBootstrapsThroughForkedDriverLive (5.59s)
--- PASS: TestStopOrphanedPostgresFromLockFileStopsLiveWorkspacePostgres (0.00s)
--- PASS: TestStartEmbeddedPostgresStopsOwnerlessLivePostgresBeforeStart (0.00s)
PASS
LIVE-EXIT=0
```

The constant under test at that run was the derived
`localPostgresPoolHolderCount*30 + 20`, i.e. **170**. So a real postmaster
started with the raised ceiling and reported it back through
`SHOW max_connections`. This is the value proven, not merely the mechanism.

That distinction was worth closing rather than waving at. An earlier run by the
independent reviewer passed at **110**, and reporting it would have proven only
that *a* raised ceiling boots -- 170 is a larger fixed shared-memory allocation,
and nothing had booted at it. Running it took five seconds.

## The mechanism-level justification (stronger than the headroom argument)

`internal/graphowner/gated_writer.go:31-42` sizes `lockChunkSize` against
Postgres's **shared advisory-lock table**, which holds roughly
`max_locks_per_transaction * (max_connections + max_prepared_transactions)`
slots — about **6,400** at the stock defaults it names (64 x 100). #5007 P2-1
proved that assumption is load-bearing: one transaction taking 20,000
`pg_advisory_xact_lock` acquisitions failed outright with *"out of shared
memory"* against a default server.

The reducer runs against the embedded server under `local_authoritative`. So:

| max_connections | advisory-lock slots (64 x n) | vs the 6,400 the chunker assumes |
|---|---|---|
| **35** (shipped) | **2,240** | **35%** |
| 170 (this change, **floor**) | 10,880 | 170% |
| 320 (same change, pool knob at 60) | 20,480 | 320% |

At 35 the embedded server offered barely a third of the slots the reducer's own
chunker is sized against. That is a concrete mechanism by which the old value
could break a component of this system, and it is a much better argument for the
raise than "the new value is near PostgreSQL's default of 100" — which is a
headroom observation, not a mechanism. Credit to the reviewer for finding it.

Performance Evidence: raising a Postgres server's `max_connections` increases
allocated shared memory, which is why the floor is derived from the pool budget
(`5 x 30 + 20 = 170`) rather than matching Compose's 640. Read 170 as the FLOOR for the
default per-process pool rather than a fixed ceiling: `ResolveLocalPostgresMaxConnections`
raises it whenever the configured pool is larger, and the allocation rises with
it (a pool of 60 gives 320). The added shared memory
is the increment from 35 to 170. For scale, a stock PostgreSQL install defaults
to 100 -- the same order this repo already reasons about elsewhere
(`gated_writer.go` works from `max_connections=100` defaults) -- so 170 is
roughly 1.7x a default install rather than a different class of allocation. The
embedded server keeps the library's default `shared_buffers`. The touched path is
process startup, not a query path: no hot-path Cypher, SQL, reducer projection,
or queue behaviour changes, and no statement plan is affected.

An earlier revision of this note said 110 here and in the table above. That was
the round-1 figure, superseded when review found the holder count wrong in
composition as well as size; the shipped constant has always been 170 in code.
The table row now reads 64 x 170 = 10,880 slots, 170% of the 6,400 the chunker
assumes.

No-Regression Evidence: relative to `origin/main` this branch adds
`ResolveLocalPostgresMaxConnections` plus its single production call site in
`embeddedPostgresConfig` (the unit and live tests call it too), which turns the `max_connections` start parameter from
a compile-time constant into one `os.Getenv` read performed once at process
start. No other production path changes. The value is never consulted per
request or per work item, so there is no added steady-state work to measure.
The arithmetic itself is bound by the table in
`TestResolveLocalPostgresMaxConnectionsFollowsConfiguredPool`, and the carried
live test asserts the resolved value actually reaches the postmaster via
`SHOW max_connections`. That live assertion was executed at
`ESHU_POSTGRES_MAX_OPEN_CONNS=60`, reporting `max_connections = 320`
(5 holders x 60 + 20 reserved) with exit 0, and a negative control pinned back
to the old constant failed with
`SHOW max_connections = 320, want 170 (the resolved local pool budget)`.
PROVENANCE, stated rather than implied: that run happened on a pre-merge commit
of the branch this fix was carried from, which is not reachable from any remote
ref, so it is not independently reproducible from the repository. The work it
belonged to merged as `a7d22aa7f` (#6603), but main's copy of the test differs
from the reviewed one -- carrying that difference forward is why this branch
exists. The assertion has NOT been re-executed at this head.

No-Observability-Change: this branch adds no runtime signal and removes none.
The resolved ceiling is a start parameter visible through `SHOW max_connections`
on the local server and through the workspace `postgres.log` the package already routes
startup output to; connection exhaustion continues to surface as the Postgres
"too many clients already" error on the existing pool paths.
