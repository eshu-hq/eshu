# #6204 — the blast-radius missing-list accumulation now has a standing bite test

`TestSQLTableBlastRadiusEveryBranchContributesLive` is the only thing in the
repo that catches a `sql_table` blast-radius UNION branch which silently stops
matching. Until this change, the only evidence that its detection actually bit
was a manual experiment recorded in
[6182-blast-radius-gate-wiring.md](6182-blast-radius-gate-wiring.md): break the
`INDEXES` branch, watch the gate fail naming it, revert. A recorded manual run
is not a gate.

## What was unguarded

The detection is one property: the `missing` list accumulates from the rows the
query returned. `scripts/verify-replay-tier.sh` requires a `--- PASS:` line per
required test, which catches a renamed or skipped test. It cannot catch a test
that still runs, still passes, and no longer proves anything.

A refactor that walked `sqlBlastRadiusBranches()` instead of the returned rows
would have kept every test green and gutted the detection. Not hypothetical:
that is exactly what the test formerly named
`TestSQLTableBlastRadiusDetectsADeadBranchLive` did, which is why #6201 renamed
it to `TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive` and deleted its
fixture-vs-constant counter.

## What changed

The accumulation moved out of the test body into
`sqlBlastRadiusMissingBranches`, and the fixtures are now built by
`sqlBlastRadiusBranchesFor(prefix)` so a second test can seed its own set. Both
live proofs call that one helper, so the new test guards the code the old one
runs rather than a copy of it.

`TestSQLTableBlastRadiusReportsUnseededBranchMissingLive` omits each branch in
turn, seeds the other eight under `probe6204`, and asserts the omitted branch —
and only the omitted branch — comes back named.

Two fixture details are load-bearing. The bite proof addresses its own table
(`probe6204_orders`) so it cannot read the rows the nine-branch proof leaves in
the same graph. And the `CONTAINS` fixture is the only one that *creates* the
shared table; the other eight `MATCH` it. Omitting `CONTAINS` without an anchor
would leave every seed unable to attach and all nine branches would come back
missing, so that one case seeds a bare `MERGE (t:SqlTable {name: ...})` first
and the assertion stays honest.

## Theory proved before the change was written

Prove-The-Theory-First. The theory was that seeding eight of nine branches makes
the real query return exactly eight repositories with the omitted branch absent,
for whichever branch is omitted — and that a nine-cycle loop is cheap enough to
run in a gate. A throwaway probe against the gate's own pinned NornicDB image,
deleted afterwards, measured both:

```
omit=CONTAINS                 repos=8 missing=[CONTAINS]
omit=QUERIES_TABLE            repos=8 missing=[QUERIES_TABLE]
omit=TRIGGERS                 repos=8 missing=[TRIGGERS]
omit=INDEXES                  repos=8 missing=[INDEXES]
omit=READS_FROM/SqlView       repos=8 missing=[READS_FROM/SqlView]
omit=READS_FROM/SqlFunction   repos=8 missing=[READS_FROM/SqlFunction]
omit=WRITES_TO/SqlFunction    repos=8 missing=[WRITES_TO/SqlFunction]
omit=REFERENCES_TABLE         repos=8 missing=[REFERENCES_TABLE]
omit=MIGRATES                 repos=8 missing=[MIGRATES]
nine omission cycles wall-clock: 155.940875ms
```

156ms for all nine settled the design question the issue left open. The issue
suggested one omitted branch; covering one would prove the accumulation derives
from rows just as well and prove nothing about the other eight, and the measured
price of covering all nine is under a fifth of a second against a blast-radius
gate half that takes 3s warm and 14s cold.

## Evidence

Every exit code below was captured from the command itself, never read from `$?`
after a pipe. All Go runs are the FULL `./internal/query/` package with no
`-run` filter, against the gate's pinned NornicDB image
(`timothyswt/nornicdb-cpu-bge:v1.2.3`) with `ESHU_REPLAY_TIER_LIVE=1`.

| what | command | result |
| --- | --- | --- |
| contract mirror red first | `bash scripts/test-verify-replay-tier.sh` | exit 1 — "gate must run the sql_table blast-radius branch proof (#5409) …" |
| contract mirror after wiring | same | exit 0 — PASS |
| full package, live | `go test ./internal/query/ -count=1 -v` | exit 0 — all three live tests `--- PASS`, bite proof 0.20s for nine subtests |
| full package, no backend | `go test ./internal/query/ -count=1` | exit 0 — the three live tests skip, as they do on a runner without Docker |
| full gate, live, end to end | `bash scripts/verify-replay-tier.sh` | exit 0 — tier 291s, blast-radius half 4s, all three `--- PASS` |
| graph left clean | `count(n)` over `probe`-prefixed nodes after the live run | `0` — neither prefix leaves nodes behind |

The gate ran with `ESHU_REPLAY_TIER_HTTP_PORT=7476
ESHU_REPLAY_TIER_BOLT_PORT=7689` so it could not contend with anything else on
this machine. It exports its own endpoint from those ports, so the run is
hermetic either way. The `graph left clean` row matters because leaving probe
nodes behind in the graph the tier asserts exact node and edge truth against was
a real bug under #6182. Its blast-radius half went from 3s warm to 4s with the
bite proof added, which the `--- PASS` line breaks down as 0.28s for the nine
subtests.

That row was a hand-run query, and a hand-run query is not a gate. The review of
this change pointed out that `sqlBlastRadiusCleanup` only logged its delete
failures, so any failure other than the closed-driver one #6182 hit would leak
the same way and still pass. Each cleanup delete now carries a verify query over
the same pattern, and the helper calls `t.Errorf` when a delete fails or leaves
rows behind, so a live run enforces `graph left clean` itself instead of waiting
for someone to check it by hand. The pairing between each delete and its verify
is held without a backend by `TestSQLBlastRadiusCleanupVerifiesEveryDelete`.

The leftover assertion itself runs only under `ESHU_REPLAY_TIER_LIVE=1` and has
NOT been re-run against a live NornicDB since it was added: the verify queries
compile and vet clean, and every non-live test in the package passes, but no
run has yet executed them against a backend. The three extra reads per cleanup
call are bounded (`LIMIT 5`, no aggregate) and the timings above predate them.

What no test without a backend catches is the reporting call itself. Downgrading
`t.Errorf` back to `t.Logf` on the leftover branch (1 substitution, `go vet` exit
0, so the mutated tree really compiled) still leaves
`go test ./internal/query/ -count=1` at exit 0, because
`TestSQLBlastRadiusCleanupVerifiesEveryDelete` reads the probe table and never
runs the cleanup loop. Two mutations it does catch, both exit 1: dropping a
probe's verify query, and pointing a verify at a different prefix than its
delete. The delete/verify pairing is guarded without a backend. The `t.Errorf`
that turns a leftover row into a failure is guarded only by a live run.

### The bite proof bites

The mutation the issue names — gut the accumulation to walk the fixture list —
applied to `sqlBlastRadiusMissingBranches` (1 substitution, `go vet` exit 0, so
the mutated tree really compiled):

| test | mutated | reverted |
| --- | --- | --- |
| `TestSQLTableBlastRadiusReportsUnseededBranchMissingLive` | FAIL, all 9 subtests | PASS |
| `TestSQLTableBlastRadiusEveryBranchContributesLive` | **PASS** | PASS |
| `TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive` | **PASS** | PASS |

The two bolded rows are the finding. The pre-#6204 suite passes with the
detection gutted, which is the false green the issue was filed for. Failure
text:

```
omitted branch "CONTAINS": missing = [], want exactly [CONTAINS] -- the missing
list must accumulate from the rows the query returned. A version deriving it
from the fixture list reports nothing missing even when a shipped UNION branch
is dead, which is the false green this test exists to stop (#6204)
```

A second mutation kills a real UNION branch rather than the accumulation —
`-[:INDEXES]->(table)` to `-[:INDEXES_BITE_PROBE]->(table)` in
`impact_blast_radius.go` (1 substitution, `go vet` exit 0), the same experiment
#6182 ran by hand:

| test | result |
| --- | --- |
| `TestSQLTableBlastRadiusEveryBranchContributesLive` | FAIL — "contributed NO repository: [INDEXES]" |
| bite proof, 8 subtests | FAIL — e.g. `missing = [INDEXES MIGRATES], want exactly [MIGRATES]` |
| bite proof, `INDEXES` subtest | PASS — correct: `INDEXES` is the omitted branch there, so its absence is what the subtest expects |

`go test` exit 1 both times. Both files were restored from a pre-mutation copy
and `git diff` confirms `impact_blast_radius.go` is untouched by this change.

### The gate wiring is guarded, not just recorded

Standing mutation cases in `scripts/test-verify-replay-tier.sh`, each verified
to reject:

| mutation to `verify-replay-tier.sh` | mirror |
| --- | --- |
| rename the bite proof (3 substitutions, confirmed present in the mutated file) | exit 1 |
| drop the bite proof from the `required_test` PASS list, leaving `-run` intact | exit 1 |
| comment out the blast-radius `go test` invocation | exit 1 |
| unmodified | exit 0 |

The rename row is a standing `for blast_test in …` loop over all three names,
not a one-time experiment — a table row is a record, only the mirror is a guard.
The first attempt at that rename check asserted 2 substitutions when the file
had 3, so the assertion aborted before mutating anything and the mirror then ran
against an unmodified file and printed PASS. That PASS meant nothing. Reporting
the substitution count is what caught it.

No-Regression Evidence: the gate's existing work is untouched. The tier
  invocation, its package list, and its `-run` allowlist are byte-identical, and
  no production file changed — the diff is two test files, the gate script's
  blast-radius invocation, and its contract mirror. The added live work is nine
  seed-and-query cycles measured at 156ms against the gate's own image, on a
  blast-radius half previously measured at 3s warm and 14s cold.

No-Observability-Change: no metric, span, or log line changes. The only new
  operator-visible output is the gate's own progress line, now naming the bite
  proof, and the subtest failure text identifying which branch went missing.

## What this does not claim

It does not widen what the blast-radius query is proven to do. The branch
fixtures are still hand-derived, both proofs still run
`blastRadiusSqlTableQuery(repositoryAccessFilter{allScopes: true})` so the
scoped-access branch stays unexercised, and the `sql_table` handler, its
bounding, and its response shape are untouched. This closes one hole: the
detection logic itself is now guarded by a test instead of by a name.
