# #6182 — the sql_table blast-radius live proof now runs somewhere automatic

Issue #5409 delivered two live tests that prove every UNION branch of the
`sql_table` blast-radius query independently contributes its own repository.
The tests are correct. Nothing ran them.

## What was broken

Both tests skip unless `ESHU_REPLAY_TIER_LIVE=1`. One file in the repo sets
that variable, `scripts/verify-replay-tier.sh`, and its `go test` invocation
covered `./internal/replay/offlinetier/` and `./internal/reducer/` with an
explicit `-run` allowlist naming neither test.

```
$ rg -n 'ESHU_REPLAY_TIER_LIVE' scripts/ .github/workflows/ Makefile
scripts/verify-replay-tier.sh:82:export ESHU_REPLAY_TIER_LIVE=1
```

PR #5920, which closed #5409, added `impact_blast_radius_sql_table_live_test.go`
and no other file. So in CI both tests skipped, and a skip prints nothing that a
check summary distinguishes from a pass. The regression #5409 exists to catch —
a branch that silently returns zero rows — could reach main unnoticed.

The Docker-free guard that does run, `TestSQLTableBlastRadiusBranchTableMatchesQuery`,
catches a branch added or removed without a fixture. It cannot catch a branch
that exists in both places and stops matching, which is the #5116 shape the
two-branch `READS_FROM` split was written to work around.

## What changed

`scripts/verify-replay-tier.sh` runs the two tests as its own invocation after
the tier, against the same container. Separate rather than appended to the
existing package list for two reasons: the test seeds and deletes `probe5409*`
nodes in the shared graph and must not interleave with the tier's exact node and
edge assertions, and a separate invocation attributes a failure to the branch
proof instead of the tier.

The trigger widened in the three places `scripts/test-verify-replay-tier.sh`
keeps in step — the `replay-tier` row in `specs/ci-gates.v1.yaml`, the
`pull_request.paths` filter in `.github/workflows/verify-replay-tier.yml`, and
the `run_or_defer replay-tier` selector in `scripts/dev/pre-pr.sh`. Without
that, a PR editing only `impact_blast_radius.go` would not select the gate that
proves its branches.

### The non-vacuity guard, and why it is not optional

`go test -run` exits 0 when its regex matches nothing:

```
$ go test ./internal/query/ -run 'TestSQLTableBlastRadiusRenamedAway' -count=1 -v; echo $?
ok  	github.com/eshu-hq/eshu/go/internal/query	0.761s [no tests to run]
0
```

Renaming either test would therefore turn this proof into a no-op reporting
success. That is #5974's shape, where an assertion that never fired read as
green for months — and it failed there because it called a binary the runner did
not have, so "command not found" and "the pattern did not match" looked the
same. The gate now requires a `--- PASS:` line per test, and refuses to start
without `rg` rather than treating a missing tool as a clean no-match.

Run against the vacuous log above, the guard rejects it:

```
[verify-replay-tier] ERROR: TestSQLTableBlastRadiusEveryBranchContributesLive did not run:
no '--- PASS: TestSQLTableBlastRadiusEveryBranchContributesLive' line, so -run matched
nothing or the test skipped. A skip is not a pass.
exit 1
```

### What the wider trigger costs, measured rather than assumed

`go/internal/query/**` is a large, active package, and adding it to a Docker
gate is not free. Over the last 200 commits on main, 8 touched
`go/internal/query/` and 1 touched the blast-radius files:

```
$ git log --oneline -200 origin/main -- go/internal/query/ | wc -l
8
$ git log --oneline -200 origin/main -- go/internal/query/impact_blast_radius.go \
    go/internal/query/impact_blast_radius_sql_table_live_test.go | wc -l
1
```

So the whole-package trigger runs the gate about 4 percent of the time, seven of
those eight for a reason unrelated to blast radius. Narrowing to
`impact_blast_radius*.go` would cut that to one in 200 and would miss a change
to `repositoryAccessFilter` or `NewNeo4jReader`, both of which the branch query
composes and either of which could break a branch without touching the query
file. Seven extra four-minute runs per 200 commits is the price of not having
that hole, and it is the reason the trigger is the package rather than the file.

### The trailing cleanup had never run — found by the first live run

The test's trailing cleanup had never worked. `defer driver.Close(...)` runs when
the test function returns, before any `t.Cleanup` callback, so all three deletes
failed:

```
impact_blast_radius_sql_table_live_test.go:173: cleanup "MATCH (n) WHERE n.repo_id
STARTS WITH 'probe5409' DETACH DELETE n": neo4j query: Trying to create session on
closed driver
```

Harmless while nothing ran the test. Not harmless once the gate runs it against
the graph the replay tier asserts exact node and edge truth against. The driver
close moved to `t.Cleanup`, registered before the node cleanup so LIFO closes it
last.

| run | `Trying to create session on closed driver` lines |
| --- | --- |
| before the fix | 3 |
| after the fix | 0 |

### The database was not pinned — found by review on #6201

The gate exported `ESHU_NEO4J_DATABASE=nornic`, but the two live tests read
`NEO4J_DATABASE` alone — even though the same tests already preferred
`ESHU_NEO4J_URI` over `NEO4J_URI`. On a clean runner both are unset and the
`nornic` default holds, so CI never saw it. On a developer machine exporting
`NEO4J_DATABASE=neo4j` the branch proof went at a different database than the
tier had just asserted against, in the same gate run — and `make pre-pr` runs
this gate locally.

Not theoretical. Feeding the pre-fix shape a hostile ambient value fails:

```
$ NEO4J_DATABASE=neo4j go test ./internal/query/ -run '<both tests>' -count=1; echo $?
impact_blast_radius_sql_table_live_test.go:202: cleanup "MATCH (n) WHERE n.repo_id
STARTS WITH 'probe5409' DETACH DELETE n": neo4j query: Neo4jError:
Neo.ClientError.Database.DatabaseNotFound (Database 'neo4j' does not exist)
--- FAIL: TestSQLTableBlastRadiusEveryBranchContributesLive
--- FAIL: TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive
1
```

With the fix, the same hostile ambient value is ignored:

```
$ ESHU_NEO4J_DATABASE=nornic NEO4J_DATABASE=neo4j go test ... -count=1; echo $?
--- PASS: TestSQLTableBlastRadiusEveryBranchContributesLive
--- PASS: TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive
0
```

Both halves changed: `sqlBlastRadiusBackend` resolves each variable
ESHU_-first, matching how the URI already behaved, and the gate pins
`NEO4J_DATABASE` as well as `ESHU_NEO4J_DATABASE`. Either alone would close
this hole; the pair also keeps a future test that reads only one of the names
honest.

### The URI had the same hole — found by the second review round on #6201

Fixing the database and leaving the URI alone was the obvious mistake to make
and I made it. `sqlBlastRadiusBackend` prefers `ESHU_NEO4J_URI`, which
`go/internal/envregistry/entries.go` lists as the canonical name with
`NEO4J_URI` as its alias, and the gate pinned only the alias. Worse, the
comment I had just written claimed the gate was hermetic "whatever name that
test reads" — false for the URI at the moment it was written.

Reachable, on the same terms as the database hole:

```
$ NEO4J_URI=bolt://localhost:7687 ESHU_NEO4J_URI=bolt://localhost:7699 \
    go test ./internal/query/ -run '<both live tests>' -count=1; echo $?
verify graph connectivity: ConnectivityError: dial tcp [::1]:7699: connect: connection refused
--- FAIL: TestSQLTableBlastRadiusEveryBranchContributesLive
--- FAIL: TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive
1
```

With both names pinned, the same ambient value loses:

```
$ ... ESHU_NEO4J_URI=bolt://localhost:7687 go test ... -count=1; echo $?
--- PASS  (both)
0
```

The gate now pins both names of both variables under one comment, rather than
per-variable comments that let the next one drift.

### The pins themselves were unguarded — found by the third review round on #6201

The first two rounds fixed instances. This one is the class.

The contract mirror guarded the blast-radius invocation, the `--- PASS:`
non-vacuity check, the `rg` refusal, and the three-way trigger lockstep — and
nothing at all about the four graph-endpoint exports those two rounds had just
fixed. Deleting `export ESHU_NEO4J_URI` would have reopened the URI hole with
the mirror still green, which is exactly the drift that produced both earlier
findings.

`has_graph_endpoint_pins` now asserts each name pins to this gate's own
container, with one negation case per name plus a repoint case:

| mutation | contract test |
| --- | --- |
| delete `ESHU_NEO4J_URI` | exit 1 |
| delete `NEO4J_URI` | exit 1 |
| delete `ESHU_NEO4J_DATABASE` | exit 1 |
| delete `NEO4J_DATABASE` | exit 1 |
| repoint any of the four away from this container | exit 1 |
| delete the workflow's `run: bash scripts/verify-replay-tier.sh` step | exit 1 |
| put `if: ${{ false }}` on that step, leaving the `run:` line present | exit 1 |
| put `continue-on-error: true` on that step | exit 1 |
| put `if: ${{ false }}` on the replay-tier **job** | exit 1 |
| put `continue-on-error: true` on the **job** | exit 1 |
| weaken the guard from value-check to existence-check | exit 1, via the repoint cases |
| unmodified | exit 0 |

Every row above except the last mutation is a standing case in
`scripts/test-verify-replay-tier.sh`, not a one-time manual experiment. The
exception is deliberate: nothing mutates `has_graph_endpoint_pins` itself,
because a guard cannot stand-test itself. Weakening it to `^export ${var}=`
would still fail every deletion row, so what actually catches that regression
is the per-pin repoint loop — a weakened guard lets a repointed pin through and
the first repoint case fails.

That distinction cost a fifth review round: the gate-step guard shipped with its
evidence recorded in this table and no negation case in the mirror, so a future
weakening of that one line would have gone unnoticed. A table row is a record;
only the mirror is a guard.

The last two workflow rows are the sharper half of the same finding, and they
arrived one review round apart. The first version of the guard searched the
whole workflow file for the `run:` line, so a step GitHub skips entirely still
satisfied it — present but never executed, skip reading as pass. Rejecting
`if:` closed that; it did not close `continue-on-error: true`, under which the
step runs, fails, and the job passes anyway. Fail reads as pass, the same hole
with the opposite trigger.

The guard extracts the step's own block and rejects both keys outright rather
than inspecting them for a "safe" value. Then the next round found the same two
keys one level up: `jobs.replay-tier.if` skips the install, the contract test
and the gate together, with every `run:` line still present, and its
`continue-on-error:` twin lets the whole job fail without blocking. So the job
block is checked the same way.

Four rejections for one property, arrived at over three review rounds, because
the disabling vector kept moving and the guard kept not following. This gate is
unconditional and blocking at both levels; changing that should require a
deliberate edit here, not quietly stop the proof from gating merges.

The last two rows came from a fourth review round, and both are the same class
again. The mirror proved the workflow installs `rg`, runs the contract test and
carries the path trigger, but never that it runs the gate — so deleting that
step would have stopped CI running the blast-radius proof with the mirror still
green. And the repoint negation covered only `ESHU_NEO4J_URI`, so regressing the
guard to an existence-only check would have kept every deletion case passing.
Each pin now has a repoint case, which is what catches that regression.

The value is asserted, not just the assignment. `export ESHU_NEO4J_URI="$OTHER"`
satisfies an existence check while reopening the hole, which is why the repoint
row is there — a guard that passes for the wrong reason is the failure this
whole gate exists to stop, and this PR produced three of them before it stopped
producing them.

### A negative control was named a bite proof — found by review on #6201

`TestSQLTableBlastRadiusDetectsADeadBranchLive` detected no dead branch. It
queries a table nothing references, asserts no seeded repository comes back, then computed
`missing` by walking `sqlBlastRadiusBranches()` — a fixture list, never the
query rows — so it passed even if every shipped UNION branch were dead. The
codex reviewer and the repo owner reached that independently on #6201.

What it actually does is worth keeping: it rules out the opposite failure, a
branch matching everything, under which the positive proof would pass for the
wrong reason. So it is renamed
`TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive`, documented as a
negative control, and its duplicated fixture-vs-constant count check is
dropped — `TestSQLTableBlastRadiusBranchTableMatchesQuery` already runs that
without a backend, and keeping it here dressed a compile-time comparison up as
live evidence.

The name was the real defect. Wiring the gate made it load-bearing: a reader
trusting it could delete
`TestSQLTableBlastRadiusEveryBranchContributesLive` — the test that does detect
a dead branch — and believe the bite proof was still enforced.

### The tier swallowed its own failure message — found by reading this diff against itself

`set -e` is on, and a failing `( ... )` exits the shell immediately, so the
tier's `tier_status=$?` was unreachable and the `die` under it had never
printed:

```
$ bash -c 'set -euo pipefail; ( exit 7 ); s=$?; echo "reached, s=$s"'; echo $?
7
```

The gate still failed — with the raw `go test` status and none of the wall-clock
or diagnostic output. The blast-radius invocation guards this with `set +e`
deliberately, and leaving the two blocks inconsistent invites someone to
"simplify" the working one to match the broken one. Both are guarded now, and
the tier's failure path prints:

```
[verify-replay-tier] offline replay tier wall-clock: 0s
[verify-replay-tier] ERROR: offline replay tier test failed (status 3)
exit 1
```

## Evidence

Every exit code below was captured from the command itself, not read from `$?`
after a pipe.

| what | command | result |
| --- | --- | --- |
| contract test red first | `bash scripts/test-verify-replay-tier.sh` | exit 1 — "gate must run the sql_table blast-radius branch proof (#5409), not only the replay tier (#6182)" |
| contract test after wiring | `bash scripts/test-verify-replay-tier.sh` | exit 0 — PASS |
| registry trigger deleted | same | exit 1 — "CI gate registry must actively trigger on and run this contract test" |
| workflow path filter deleted | same | exit 1 — "workflow must actively run and trigger on this contract test" |
| pre-pr selector narrowed back | same | exit 1 — "active pre-pr replay selector does not mirror the workflow and registry" |
| full gate, live | `bash scripts/verify-replay-tier.sh` | exit 0 — tier 218s, blast-radius 14s, both tests `--- PASS` |
| one branch killed | `INDEXES` → `INDEXES_BITE_PROBE`, same gate | exit 1 — "these UNION branches contributed NO repository: [INDEXES]" |
| branch restored | focused rerun against the same container | exit 0, both `--- PASS`, zero cleanup errors |

The bite proof — the `INDEXES` row above — kills exactly one branch and leaves
the other eight intact. When this was written it was a manual experiment
recorded here, not a standing test; the one test whose name claimed that role
was a negative control and has been renamed. Making it permanent meant seeding
eight of nine branches and asserting the ninth is reported missing, which was
real work and outside what this PR was asked for, so it was raised on #6201 for
the owner rather than built here.

**It is a standing test now.** #6204 built it:
`TestSQLTableBlastRadiusReportsUnseededBranchMissingLive` omits each branch in
turn and asserts the omitted one is reported missing by name, through the same
`sqlBlastRadiusMissingBranches` the nine-branch proof calls. See
[6204-blast-radius-bite-test.md](6204-blast-radius-bite-test.md). Read the
paragraph above as the state at the time of this PR, not as the state today.

The hermetic count guard still passes at nine fixtures against
`blastRadiusSqlTableBranches = 9`, so the live tests remain the only thing in
the repo that catches a branch which stops matching — which is the claim this
wiring is here to make true.

Two guards I wrote were weak, and both were found by trying to break them rather
than by reading them. The registry check searched all of `ci-gates.v1.yaml`,
where twelve other gates carry `go/internal/query/**`; deleting the replay-tier
trigger left it green, so it now extracts the replay-tier block first. The
non-vacuity check was an unanchored fixed-string search that a `# ` prefix did
not defeat; it is now anchored at line start through tab indentation.

No-Regression Evidence: the gate's existing work is untouched. The tier
  invocation, its package list, and its `-run` allowlist are byte-identical.
  Two full runs, each measuring both halves in the same run so the ratio is
  comparable even though the totals are not — the second ran with a warm Go
  build cache, which is why its absolute numbers are much lower:

  | run | tier | blast radius | added share |
  | --- | --- | --- | --- |
  | cold build cache | 218s | 14s | 6.4% |
  | warm build cache | 35s | 3s | 8.6% |

  Do not read 218s against 35s as a speedup; they are not comparable runs. The
  added work starts no second container, and the two tests seed nine
  repositories and delete them, bounded and self-contained.

No-Observability-Change: no metric, span, or log line changes. This wires an
  existing test into an existing gate. The only new operator-visible output is
  the gate's own `[verify-replay-tier]` progress lines and the failure message
  naming a dead branch.

## What this does not claim

It does not claim the blast-radius query is correct beyond its nine branches
each matching one seeded repository at the expected hop count. It does not touch
the `sql_table` handler, its bounding, or its response shape. And the branch
fixtures are hand-derived: a branch whose real-world shape differs from the
seeded chain is proven only for the seeded chain.

Both tests call `blastRadiusSqlTableQuery(repositoryAccessFilter{allScopes: true})`,
so the scoped-access branch of that filter — the one a component-extension token
takes — is not exercised by either. That is a limit #5409's tests already had;
this change wires them into a gate without widening what they cover. A scoped
run would need its own seeded grant set and is a separate piece of work, not a
line to add here.
