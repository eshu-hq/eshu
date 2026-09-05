# Backend conformance evidence notes

## Value-flow cloud sink conformance pair

The production query `valueFlowCloudSinkTargetsCypher`
(`go/internal/reducer/valueflow/value_flow_cloud_sink_loader.go`) returns zero rows on
NornicDB and the correct row on Neo4j 5.x community. It resolves which cloud
resources a function's cloud action can reach, and its failure is silent — no
error, just a graph missing that category of edge. Nothing in the repository
detected it.

Four independent backend divergences empty it, each measured against Neo4j 5.x
community with the same fixture on `eshu-nornicdb-pr290:3722b483c02c` and on
upstream `main`:

| Shape | Neo4j 5 | NornicDB |
| --- | --- | --- |
| `collect()` after two `MATCH` clauses | one row | no rows |
| `ws[0] AS w2` then `w2.name` | the property value | the literal text `"w2.name"` |
| `x IN relationship.listProperty` | matches | matches nothing |
| multi-hop `MATCH` whose anchor was bound by an earlier clause | one row | no rows |

Controls: two `MATCH` clauses without aggregation, aggregation after one
`MATCH`, `IN` over a *node* list property, reading the relationship property
directly, the same multi-hop pattern as one clause with a fresh anchor, and the
same bound anchor split into single-hop clauses all behave identically on both
backends. Filed upstream as orneryd/NornicDB#297, #298, #301 and #302.

**The list is not exhaustive, and its history says why.** It grew from two to
three to four as each review looked further along the statement, and every
addition was found by testing past where the previous round stopped. So this
case asserts that the production statement returns a row on a conforming
backend; it does not assert that these four are all the reasons it might not.
Fixing all four upstream is necessary for the query to work and has not been
shown to be sufficient.

The fourth has a workaround needing no upstream change: decomposing the compound
multi-hop `MATCH` into chained single-hop clauses works on both backends. That
is available to the production query today, independently of #297/#298/#301.

**A full rewrite avoiding all of them has been attempted and does not exist
yet.** Replacing `collect()`+subscript with a correlated
`WHERE NOT EXISTS { ... }` — the natural way to assert "exactly one workload"
without aggregation — runs straight into a fifth defect: correlated subqueries
ignore their inner predicate, so `EXISTS` admits every row and `NOT EXISTS`
admits none (orneryd/NornicDB#303, reproduced here with an uncorrelated control
and both polarities). Every alternative reached for so far has avoided one
defect by hitting another. Upstream is the path; a workaround-only rewrite would
need its own live-backend proof pass and that proof does not exist.

The conformance pair runs the complete production statement rather than stopping
at the first divergence, because a case truncated at the first bug goes green as
soon as that bug is fixed while the query stays broken on a later clause.

## Running it

The pair is **opt-in**, behind `ESHU_BACKEND_CONFORMANCE_VALUE_FLOW`, and off by
default it is absent from the corpora entirely rather than present-and-skipped.

```
ESHU_BACKEND_CONFORMANCE_VALUE_FLOW=1 ESHU_BACKEND_CONFORMANCE_LIVE=1 \
  ESHU_GRAPH_BACKEND=nornicdb go test ./internal/backendconformance -run Live
```

It is opt-in because it fails against NornicDB **by design** — that is what it is
for — and the live-conformance gate blocks merges. Left in the default corpora it
would red-line every unrelated change in the repo until upstream lands a fix,
which is a heavy toll for a defect already documented in five upstream issues and
in this note.

Nothing about the pair is weakened by the gate. Measured both ways against a live
pinned NornicDB:

| | Exit | Case |
| --- | ---: | --- |
| opt-in unset | 0 | not run |
| opt-in set | 1 | `read case "value-flow cloud sink aggregation and subscript projection" returned 0 rows, want at least 1` |

Run it to check whether upstream has landed a fix: the day it exits 0 with the
opt-in set, the defects are gone and the gate can come off.

Nobody has to remember to. CI runs both lanes on every change to this package,
to the production query, or to either Compose file — see
[Running it in CI](#running-it-in-ci) below.

No-Regression Evidence: the cases are data appended to `DefaultReadCorpus` and
`DefaultWriteCorpus`. They are evaluated only under the existing
`ESHU_BACKEND_CONFORMANCE_LIVE` opt-in, so the default test path does no
additional work and its runtime is unchanged; `go test
./internal/backendconformance -count=1` passes. Against a live backend the pair
adds one seeded chain of six nodes and five edges plus one read, which is the
same order as every other case in the corpus.

Observability Evidence: failure is reported by the existing live-conformance
runner, which names the failing case and the row shortfall — `read case
"value-flow cloud sink aggregation and subscript projection" returned 0 rows,
want at least 1`.

One environment variable is introduced, `ESHU_BACKEND_CONFORMANCE_VALUE_FLOW`,
and *omission* is reported as deliberately as failure. Because the pair is
absent from the corpora rather than skipped, a run without the variable would
otherwise pass silently while proving strictly less. Two signals prevent that:
`scripts/verify_backend_conformance_live.sh` prints whether the pair is
INCLUDED or OMITTED before it runs anything, and `TestLiveBackendConformance`
logs the same fact — which is why that script now passes `-v`, without which
the log would be invisible on a pass. No metric or span is added; this is a
test-lane surface with no runtime component.

Verified both directions on live backends: `ESHU_GRAPH_BACKEND=neo4j` passes
`TestLiveBackendConformance` with exit 0; `ESHU_GRAPH_BACKEND=nornicdb` fails
with exit 1 naming this case.

**Re-run at the current revision, both lanes, on this machine.** An earlier
revision of this note recorded exit codes from a run that predated the restored
opening clause (`WHERE fn.uid IN $function_uids`) and the parameter rename from
`function_uid` to `function_uids`. Those were a memory of an earlier query, and a
wrong parameter key empties the result on *both* backends — so that red could not
distinguish a backend defect from a broken fixture. Both lanes have now been re-run
against the statement and parameters in this branch:

```
Running live backend conformance for neo4j on bolt://localhost:7687 database neo4j
  value-flow cloud sink pair: INCLUDED (ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is set)
  ok  github.com/eshu-hq/eshu/go/internal/backendconformance  3.703s      exit 0

Running live backend conformance for nornicdb on bolt://localhost:7687 database nornic
  value-flow cloud sink pair: INCLUDED (ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is set)
  read case "value-flow cloud sink aggregation and subscript projection"
    returned 0 rows, want at least 1                                      exit 1
```

Same fixture, same statement, same bound parameters, one backend apart. **Neo4j
green is what makes the NornicDB red mean something**: it rules out the broken-fixture
explanation, which the hermetic guards cannot do on their own. Images:
`neo4j:5-community` and `eshu-nornicdb-pr290:3722b483c02c` (the commit-pinned build
this repo's compose default builds), each in its own throwaway Compose project, torn
down with `down -v` after the run.

This is the positive control #6192 exists to make permanent. Run by hand it proves the
pair today; wired into CI it stops the next person having to take this paragraph on
trust.

## Running it in CI

That wiring now exists. `.github/workflows/value-flow-conformance-expectation.yml`
runs both lanes in one job with the expectation inverted, through
`scripts/verify-value-flow-conformance-expectation.sh`:

- the NornicDB lane must FAIL, and the run must name this read case, and that
  must be the only failure in the run,
- the Neo4j lane must PASS, in the same job, as the positive control.

So the job is normally green, and green means "still broken upstream, exactly as
this note says". It goes red the moment either half stops being true, which is
the only moment anyone needs to know.

The nornicdb lane matches on the message rather than the exit code, deliberately.
A broken fixture, a failed seed, and a refused Bolt connection all exit non-zero,
and an expected-fail that accepts any of them is a false green wearing the costume
of a gate. Matching the message is not enough on its own either: the lane also
rejects a run that reports the documented failure and a second one behind it,
which `TestLiveBackendConformance` can do from a deferred closure — the corpus
cleanup and the driver close both fail the test after the read-corpus failure is
already recorded. `scripts/test-verify-value-flow-conformance-expectation.sh`
drives the gate with stub lanes to prove every verdict is reachable, and
reachable only for its own reason; it needs no backend and runs anywhere.

The measurement below is a real run through that gate script on a developer
machine, both lanes, not a CI run — CI cannot have one until this lands. Pin it
to the tree it measured: it was taken on the working tree that became commit
`7ccf9ed9b5`, before that commit existed, so it predates every later head on
this branch. Read it as evidence for that tree and not for the branch tip.

One caveat when you read it. The transcript does satisfy the second-failure
check described above — one `--- FAIL:` line, no cleanup or close failure — but
it was recorded before that check existed, so it is not a run of the gate as it
now stands. The first CI run is.

What CI changes is that the numbers stop being a memory: the job re-measures both
lanes on every run, so this section cannot go stale the way the paragraph above
it did.

```
== value-flow conformance expectation: neo4j lane ==
Running live backend conformance for neo4j on bolt://localhost:7788 database neo4j
  value-flow cloud sink pair: INCLUDED (ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is set)
--- PASS: TestLiveBackendConformance (13.07s)
ok  github.com/eshu-hq/eshu/go/internal/backendconformance  13.328s
neo4j lane: observed exit code 0

== value-flow conformance expectation: nornicdb lane ==
Running live backend conformance for nornicdb on bolt://localhost:7788 database nornic
  value-flow cloud sink pair: INCLUDED (ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is set)
  live_test.go:101: run nornicdb live read corpus: read case "value-flow cloud
    sink aggregation and subscript projection" returned 0 rows, want at least 1
--- FAIL: TestLiveBackendConformance (3.23s)
nornicdb lane: observed exit code 1
```

Images: `neo4j:2026-community` (Neo4j 2026.07.1, the current Compose default —
the earlier by-hand run above used `neo4j:5-community`, so the Neo4j side of this
holds across both major lines) and `eshu-nornicdb-pr290:3722b483c02c`. Each ran in
its own throwaway Compose project on a non-default Bolt port, torn down with
`down -v` before the next came up, because both lanes bind the same port.

No-Regression Evidence: nothing here runs at runtime. The gate lives in CI and
in two shell scripts; the only Go edit outside a test file is the description
string on the `ESHU_BACKEND_CONFORMANCE_VALUE_FLOW` entry in
`go/internal/envregistry/entries.go`, which now names the third consumer of the
variable. No query, write, worker, lease, batch, or knob changed, so there is no
before/after to measure. The two live lanes above are the runtime cost the job
itself adds, and they run only on the nine paths its registry entry lists — a
change to `go/internal/query` or to a docs page does not select it, which
`TestValueFlowExpectationIsRequiredForItsOwnTriggers` holds as a control.

No-Observability-Change: the failure signal is the one the live-conformance
runner already emits, naming the case and the row shortfall, plus the observed
exit code the gate script prints for each lane. No metric, span, or log is
added; this is a test lane with no service in it.

**When upstream lands.** The NornicDB lane going green is the signal, and the
repair is one change: delete `valueFlowCasesEnabled` and its callers in
`corpus_value_flow.go` so the pair joins the default corpora and comes back under
the blocking e2e live-conformance gate on both backends. The expectation gate,
its script, its test mirror, its workflow, and its registry entry have nothing
left to assert at that point and come out with it. The gate script prints exactly
this when it sees the lane pass, so nobody has to find this paragraph first.
