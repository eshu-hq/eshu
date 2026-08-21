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

### A second defect the first live run exposed

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

The bite proof kills exactly one branch and leaves the other eight intact. The
hermetic count guard still passes at nine fixtures against
`blastRadiusSqlTableBranches = 9`, so the live test is the only thing in the
repo that catches it — which is the claim this wiring is here to make true.

Two guards I wrote were weak, and both were found by trying to break them rather
than by reading them. The registry check searched all of `ci-gates.v1.yaml`,
where twelve other gates carry `go/internal/query/**`; deleting the replay-tier
trigger left it green, so it now extracts the replay-tier block first. The
non-vacuity check was an unanchored fixed-string search that a `# ` prefix did
not defeat; it is now anchored at line start through tab indentation.

No-Regression Evidence: the gate's existing work is untouched. The tier
  invocation, its package list, and its `-run` allowlist are byte-identical, and
  the tier passed in 218s on the run that also added the branch proof. The added
  work is 14s of wall clock against the container the gate already starts, so
  the gate grows by roughly 6 percent and starts no second container. The two
  tests seed nine repositories and delete them, bounded and self-contained.

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
