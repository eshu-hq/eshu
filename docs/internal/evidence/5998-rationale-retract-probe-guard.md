# Evidence: guarding the rationale EXPLAINS retract behind an existence probe (#5998)

## What changed

`EdgeWriter` no longer issues the rationale `EXPLAINS` retract unconditionally.
Both retract shapes now run behind a bounded existence read that carries the
identical `MATCH`/`WHERE`, and the `DELETE` fires only when that read finds
something:

- **Whole scope** — `retractRationaleEdgesWithProbe` guards the repository-wide
  retract, one statement per `RetractEdges` batch binding every repository in
  that batch (batches are selected by partition-hash bucket, up to
  `defaultBatchLimit` = 100 rows), so a ~900-repository corpus issues on the
  order of 9-16 per generation rather than one each.
- **Delta** — `executeGuardedRationaleDeltaRetracts` guards the per-target-label
  retract, seven statements per batch — one per target label — on every
  incremental sync, each paired with its own probe from the same label list in
  the same order. The seven bind a union of `delta_file_paths` collected across
  every delta-flagged row in the batch (`collectDeltaFilePaths`), so the count
  is seven per `RetractEdges` call regardless of how many repositories that
  batch carries.

The probe projects `RETURN true LIMIT 1`. The fail-safe direction is toward
deleting: an executor with no `ProbeExecutor`, or a probe that errors, runs the
`DELETE` unconditionally. Only a definitive zero skips it.

"Unconditionally" means immediately, and that took a correction to become true.
An earlier revision of this change routed `ExecuteProbe` through the same
`RetryingExecutor` path as a write. Because `newReducerNeo4jExecutor` sets
neither `MaxRetries` nor `BaseDelay`, the defaults applied — four attempts with
50ms exponential backoff, each bounded by `ESHU_CANONICAL_WRITE_TIMEOUT` (30s by
default) — so a backend sustaining `TransientError` could have held the
partition lease for roughly two minutes before the fail-safe `DELETE` even
started, in order to avoid a single `DELETE` this guard exists to make cheaper.
Review (#6165) called that the worse of the two available trades, and it was
right: the fallback is cheap and always correct, so the worst case should be one
`DELETE`, not two minutes of waiting and then one `DELETE`.

`RetryingExecutor.ExecuteProbe` therefore does not retry at all. Every error
class — transient, non-transient, and the unsupported-capability error — falls
straight through to the unconditional `DELETE`. No transient-error resilience is
lost on the write path, because the `DELETE` that follows still retries
normally; only the read that decides whether to attempt it gives up early.
`probe_error` remains an outcome to watch, but a sustained non-zero value now
costs one redundant `DELETE` per batch rather than a lease held across four
attempts.

This is a mitigation, not a design feature. It exists because of a backend cost
defect described below; the root-cause track is
[orneryd/NornicDB#296](https://github.com/orneryd/NornicDB/issues/296). If that
is fixed, the guard becomes redundant rather than wrong.

## Evidence markers

Performance Evidence: baseline is the unguarded retract as shipped on main.
Whole repository, corpus-validation-host, NornicDB image
`eshu-nornicdb-pr290:3722b483c02c`: 18.603s / 17.653s / 18.071s on a store of
1,675,949 relationships (the count the ledger rows record; the run also
reported 1,016,634 nodes, which they do not), against 0.021s / 0.022s / 0.023s
on an empty store. The measurement bound a single-repository id list; in
production the statement binds the whole `RetractEdges` batch's list. That
difference is unmeasured, though it is not what the rows isolate -- the defect
is that cost tracks store size rather than input size or rows removed, and
every statement here removed ZERO rows
(`ledger:5998-zero-row-explains-delete-large-store`,
`ledger:5998-zero-row-explains-delete-empty-store`). After the guard, that case
costs the bounded probe instead, measured at 0.021s on the same large store
(`ledger:5998-explains-existence-probe-read`). Delta path, local-dev, same
image: seven per-label statements at 12.589s / 11.810s / 12.187s / 11.775s on a
190,000-relationship store against 0.291s / 0.270s empty
(`ledger:5998-delta-per-label-retract-seeded-rerun`,
`ledger:5998-delta-per-label-retract-empty`), guarded by seven probes costing
0.310s seeded and 0.284s empty
(`ledger:5998-delta-per-label-probe-seeded`,
`ledger:5998-delta-per-label-probe-empty`). Each path is compared only against
its own same-store, same-host pair.

No-Regression Evidence: graph truth is unchanged. The Ifá determinism matrix is
green across N=1/2/4 with digest
`aa0904cc09da0b95bf78a0f27dd1b5b0e2aec15c371e0077edb81312360a4998`, byte-identical
at every worker count AND identical to a run of the same matrix taken before
this change. The fault-injection matrix is green across 18 cells with every
rationale cell converging on the fault-free baseline digest
`280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052`. Terminal
queue state is the B-12 residual bound the gates already poll to, with zero dead
letters asserted per cell; the generation-2 rationale tuple is
`1|1|0|1|0|1|1|0` with one accepted generation.

Observability Evidence: the guard adds one instrument,
`eshu_dp_rationale_retract_probe_outcomes_total`, with two bounded labels,
`outcome` (`skipped`, `deleted`, `unsupported`, `probe_error`) and `scope`
(`whole_scope`, `delta_by_file_path`), plus a matching
`rationale_retract_probe` span event and a `rationale retract probe completed`
structured log carrying the same outcome and scope. `unsupported` and
`probe_error` are the operator signals that the guard has gone inert; both are
fail-safe, because those paths run the DELETE unconditionally.

Why the change is safe: the probe binds the identical parameters as the DELETE
it guards, only a definitive zero skips the DELETE, and every other outcome —
including a missing capability or a probe error — runs the DELETE exactly as
before. A redundant delete costs time; a skipped one would leave stale edges.

## The defect being mitigated

On the pinned NornicDB build a relationship `DELETE` whose `MATCH` selects
nothing costs proportional to the size of the store, not to the number of rows
removed. The identical `MATCH`/`WHERE` executed as a bounded read does not.

Two stores, same image, same statements, same parameters, every statement
deleting zero rows. Trials interleaved between the stores so cache warmth and
host load could not land entirely on one of them.

| statement (all match zero rows) | 1,675,949-relationship store | empty store | ledger |
| --- | --- | --- | --- |
| shipped whole-repository retract DELETE | 18.603s / 17.653s / 18.071s | 0.021s / 0.022s / 0.023s | `ledger:5998-zero-row-explains-delete-large-store`, `ledger:5998-zero-row-explains-delete-empty-store` |
| source-anchored DELETE, no predicates | 17.666s / 16.944s / 17.540s | 0.021s / 0.021s / 0.025s | `ledger:5998-zero-row-explains-delete-source-anchored` |
| untyped-both-ends DELETE, no predicates | 173.185s / 177.124s / 182.249s | not recorded | `ledger:5998-zero-row-explains-delete-untyped-both-ends` |
| same MATCH/WHERE as row 1, run as `RETURN rel LIMIT 1` | 0.021s (3 trials) | 0.021s (3 trials) | `ledger:5998-explains-existence-probe-read` |

The large store held 1,675,949 relationships (the figure the ledger rows
record; the run also reported 1,016,634 nodes, which they do not) with zero
`Rationale` nodes and zero `EXPLAINS` edges, so every `DELETE` above removed
nothing. What the rows isolate:

- **The `DELETE` clause, not the `MATCH`.** Row 4 is row 1's identical
  `MATCH`/`WHERE` executed as a bounded read and stays cheap on the large store
  (`ledger:5998-explains-existence-probe-read`) while row 1's `DELETE` over the
  same selection does not
  (`ledger:5998-zero-row-explains-delete-large-store`).
- **Not the predicates.** Row 2 drops both property predicates and is
  marginally faster. This is not the [#3624](https://github.com/eshu-hq/eshu/issues/3624)
  index-defeat shape, where the predicates were the problem; here the worst
  cell has none.
- **Anchoring the source label is worth roughly 10x.** Row 3 leaves both
  endpoints untyped and costs an order of magnitude more than the
  label-anchored row 2
  (`ledger:5998-zero-row-explains-delete-untyped-both-ends`).
- **Store size is the variable.** Rows 1, 2 and 4 each collapse to the same
  cheap cost on an empty store
  (`ledger:5998-zero-row-explains-delete-empty-store`, the empty-store trials
  in `ledger:5998-zero-row-explains-delete-source-anchored`'s note, and
  `ledger:5998-explains-existence-probe-read`, which measured both stores).
  Row 3 has no empty-store control in the ledger, so it is not claimed here:
  it establishes the source-anchoring cost against row 2 on the same store,
  which is all it is cited for.

Cite the ledger ids rather than restating the figures. The table keeps its
numbers because the shape of the comparison is the evidence; the ledger is what
stops those figures drifting across the other documents that mention them.

Row 4 is not the shipped probe. Review F11 changed the guard's projection from
`RETURN rel` to `RETURN true` after this table was measured, and the shipped
form was not separately timed. `RETURN true LIMIT 1` returns a literal instead
of serializing a relationship over Bolt, so row 4 bounds the shipped statement
from above rather than describing it exactly.

### The delta path, on its own same-store pair

The per-label delta retract carries the same store-size term, measured
separately on its own pair:

| statement set (all delete zero rows) | 190,000-relationship store | empty store | ledger |
| --- | --- | --- | --- |
| seven per-label delta DELETEs | 12.589s / 11.810s / 12.187s / 11.775s | 0.291s / 0.270s | `ledger:5998-delta-per-label-retract-seeded-rerun`, `ledger:5998-delta-per-label-retract-empty` |
| the seven probes that guard them | 0.310s / 0.307s | 0.284s | `ledger:5998-delta-per-label-probe-seeded`, `ledger:5998-delta-per-label-probe-empty` |

Both halves of each pair ran on the same host against the same image, so each
pair isolates store size on its own terms. One caveat on the DELETE row, stated
rather than glossed: its seeded figure is from the rerun session and its
empty-store control is from the earlier session — the same session whose seeded
sibling was withdrawn as irreproducible. The probe row is single-session. The
DELETE pair's direction is conservative (the earlier session ran contended, so
a same-session empty control would if anything be lower, widening the gap), and
the rerun interleaved an empty store whose figure was not recorded as its own
ledger row. These figures are deliberately **not**
compared against the whole-repository table above: that table was measured on a
different host against a different store, and a ratio across the two would not
mean anything. Each path is justified by its own same-store pair.

The second row is the finding that makes the guard worth having. The probes do
not carry the store-size term the `DELETE` carries — 0.310s seeded against
0.284s empty is indistinguishable — so guarding is close to free and stays free
as the store grows.

An earlier row for the seeded delta case, `ledger:5998-delta-per-label-retract-seeded`,
recorded 48.223s / 45.526s and did not reproduce. Four later trials on a
comparable store measured 11.775–12.589s. That row is superseded by
`ledger:5998-delta-per-label-retract-seeded-rerun` and retained only because the
ledger is append-only; do not cite it.

### Why this matters at corpus scale

The whole-repository retract fires once per `RetractEdges` batch, not once per
repository. The reducer selects intents by partition-hash bucket
(`defaultPartitionCount` = 8) up to `defaultBatchLimit` = 100 rows,
`planRepoWideRetractWork` routes every non-skipped refresh row into a single
retract set, and the worker makes one `RetractEdges` call for it; the statement
binds all of those repository ids in `$repo_ids` at once. A ~900-repository
corpus therefore issues on the order of 9-16 whole-scope statements per
generation. At the corpus host's own measured per-statement cost
(`ledger:5998-zero-row-explains-delete-large-store`) that is minutes of backend
work per full generation to delete nothing -- not the hours an earlier draft of
this document claimed by multiplying the per-statement figure by the repository
count. That arithmetic assumed one statement per repository and was wrong; the
correction is recorded here rather than quietly dropped, because it was the
headline justification for this change.

Minutes per generation to accomplish nothing is still worth removing, and the
per-statement defect is unchanged -- but the honest size of the whole-scope
prize is smaller than first stated, and it is bounded further by the skip
semantics below.

Batch binding also changes what a skip means. Because the probe binds the whole
batch, it answers "does ANY repository in this batch have a matching EXPLAINS
edge" -- so a single repository carrying one rationale edge makes the batch-wide
DELETE run for every repository in that batch. Nothing here measures the
resulting skip rate under production batch composition, and the live matrices
cannot distinguish an active guard from an inert one, so the whole-scope path's
realised saving is not proven. What IS proven is the per-statement cost and that
the probe is orders of magnitude cheaper than the statement it guards. The delta
path does not share this limitation to the same degree: its statements bind file
paths, and a sync that touched no rationale-bearing file skips all seven.

No corresponding corpus-scale figure is given for the delta path. Its
measurements come from a different host, and extrapolating them onto the corpus
host would be exactly the cross-host comparison the paragraph above avoids.

### Why the golden corpus could not have caught it

A 20-repository store is far too small for a store-size-proportional cost to
appear, so this class of regression passes replay gates green. Cost that scales
with the store needs a store-scale measurement, not a fixture.

## Correctness: probe and delete agree on the same rows

The lockstep unit test proves probe and delete share statement text and bound
parameters. It cannot prove the backend agrees on row visibility between a read
and a delete — that would be a gate agreeing with itself, and this repository
has a live prior for exactly that asymmetry (the v1.1.9 bounded-delete bug,
where a `DELETE` shape no-oped while reads found the rows). The dangerous
direction is a probe returning false while rows exist: the delete is skipped,
the stale edges survive, and nothing reports it. Both directions are therefore
proven against the backend rather than assumed.

### The probe's positive answers

Run against the pinned image `eshu-nornicdb-pr290:3722b483c02c`, seeding one
`Rationale-[:EXPLAINS]->Function` edge and exercising both shipped branches
(`IN $evidence_sources` and `= $evidence_source`):

| step | multi-source branch | single-source branch |
| --- | --- | --- |
| empty store | false | false |
| one matching edge present | **true** | **true** |
| rows returned (`LIMIT 1` honored) | 1 | 1 |
| after the paired DELETE | false | false |

The paired `DELETE` removed the edge (count 1 → 0), so probe and delete agree on
the same rows. `LIMIT 1` is asserted explicitly because `RETURN true` is a
literal projection rather than a bound variable, which is not safe to assume.

This is a semantics proof, not a timing proof, so it ran on a small local store
on the identical image ID rather than the corpus-scale host. Store size is
irrelevant to whether a `MATCH` matches; the store-size-dependent numbers are the
tables above, measured separately.

The expectation that this shape holds is not blind. `go/internal/storage/cypher/evidence-4367-rationale-delta-retract-per-label.md`
already recorded the per-label `MATCH` deleting on all seven labels live, and
recorded the target-label *disjunction* shape failing ("deleted 0, both edges
survived"). The whole-repository anchor the probe mirrors is the row #4367
recorded as working.

### The skip path is lossless

Proven directly on the pinned image with `EXPLAINS` edges seeded on two of the
seven target labels:

| label | probe | independent count |
| --- | --- | ---: |
| Function | true | 1 |
| Class | false | 0 |
| Struct | false | 0 |
| Interface | false | 0 |
| TypeAlias | false | 0 |
| Enum | true | 1 |
| File | false | 0 |

Three things follow, and the run could have failed on any of them:

- **probe-false is truthful.** Every false probe had a count of 0, taken with a
  different statement than the probe, so the check is not the probe confirming
  itself.
- **probes are per-label isolated.** Function and Enum returned true on their own
  row while five other labels returned false with `EXPLAINS` edges present
  elsewhere in the graph, so one label's rows cannot cause another's skip.
- **skipping is lossless.** Running deletes only where the probe said true left
  every label at 0 — the same graph an unguarded run produces.

## Concurrency

Stated precisely, because two earlier drafts of this section overclaimed.

The whole-scope guard does not bind exactly one repository. A batch is selected
by partition-hash bucket and carries up to `defaultBatchLimit` rows, and
`planRepoWideRetractWork` routes every non-skipped refresh row into one retract
set, so the ordinary whole-scope refresh already binds every repository in the
batch. `RetractEdges`' mixed-batch path likewise passes every repository in the batch whose
row is a whole-scope refresh row — it carries the refresh `intent_type` and no
`delta_projection` — a repository on a full generation whose refresh happened
to share a partition bucket with a delta-generation sibling. The `intent_type`
condition is load-bearing: without it an unmarked legacy per-edge row would
pull a whole-repository DELETE the pre-#5998 path never issued.
Both are safe for the same reason, which is not a one-repository guarantee: the
probe and the delete bind the identical set, built from the same slice in the
same call.

Edge writes carrying the `retract_via_refresh` marker are fenced behind the same
refresh that owns the retract, by same-batch ordering and by the durable
completed-intents gate, so a marked writer cannot insert a matching `EXPLAINS`
edge between a probe and its delete. Unmarked legacy rows bypass that fence
(`shared_projection_worker_refresh_fence.go`), so the residual is stated rather
than denied: an unmarked row landing inside the probe-to-delete window is no
worse under the guard than without it. Skip-then-write leaves a correct
current-generation edge, and write-then-delete is the pre-existing
[#2910](https://github.com/eshu-hq/eshu/issues/2910) behavior the guard does not
change.

The per-label delta statements already run sequentially, each in its own
transaction, for the NornicDB managed-transaction reason documented on
`executeCodeCallRetractStatements`. The guard does not change that.

## Observability

Both guarded paths emit through one dispatcher,
`observeRationaleRetractProbe` in `go/internal/storage/cypher/edge_writer_logging.go`,
which records a counter, a span event, and a structured log. They carry
overlapping rather than identical fields — the counter carries only `outcome`
and `scope`, `repo_count` and `sample_repo_id` go to the span event and the log,
and `probe_duration_seconds` only to the log — so the invariant worth relying on
is narrower than "the three agree": all three agree on `outcome` and `scope`,
because all three take them from the same call.

The counter is `eshu_dp_rationale_retract_probe_outcomes_total`, with two bounded
labels: `outcome` (`skipped`, `deleted`, `unsupported`, `probe_error`) and
`scope` (`whole_scope`, `delta_by_file_path`). `scope` exists because the two
paths fire at very different rates — one statement per `RetractEdges` batch per
generation against seven per batch on every incremental sync — so a collapsed
`outcome` count could not show which guard is doing the work, and a delta guard
that silently stopped engaging would hide behind the whole-scope path's counts.

The span event and log carry `repo_count` plus one `sample_repo_id` rather than
the full repository list, because batch size scales with `BatchLimit` (default
100). That mirrors `reportUnroutableRows`' bounded shape in the same package.

`unsupported` and `probe_error` are the signals to watch. Both are fail-safe —
the delete runs unconditionally, so graph truth stays correct — but a sustained
non-zero value on either scope means that scope's guard is inert and every
retract on it is paying full cost again. The usual cause is an executor wrapper
that does not forward the probe capability.

The metric distinguishes an *inert* guard from an *active* one. It does not
distinguish a correct skip from a wrong one: a repository that genuinely has no
rationale edges and a probe that wrongly reported none both increment `skipped`.
It is a performance signal, not a correctness one.

## Wrapper capability rule

`ProbeExecutor` is an optional executor capability, like `GroupExecutor`. Every
wrapper that forwards `GroupExecutor` to its inner executor must forward
`ProbeExecutor` the same way, and the `ExecuteOnly*` wrappers must hide both.

Forwarding one but not the other is the worst available failure shape: wrappers
above the gap still satisfy the interface, so a caller's type assertion succeeds
and every probe dead-ends in the middle of the chain with all tests green.
`probe_follows_group_test.go` enumerates the wrappers and asserts the rule
rather than leaving it to review. A companion test,
`probe_follows_group_repo_scan_test.go`, walks every non-test `.go` file under
`go/cmd` and `go/internal` for `ExecuteGroup` receivers and requires a
package-scoped `ExecuteProbe`, so a newly added wrapper anywhere in the tree
cannot be silently omitted from the table.

The rule is not universal in practice, and the exceptions are deliberate rather
than overlooked. That repo scan carries an allowlist, and three of its five
entries are production executors: `bootstrapNeo4jExecutor`,
`projectorNeo4jExecutor` and `ingesterNeo4jExecutor`. None of those chains ever
reaches a `RetractEdges` dispatch, so no rationale retract can run through them
and there is nothing for a probe to guard; each entry carries that reason in the
allowlist. The remaining two entries are test stubs. Read the rule as binding on
every wrapper in the EdgeWriter chain, with exemptions recorded where a chain
provably cannot reach the guard.

## What the live gates cover, and what they do not

This section has been wrong in both directions in earlier drafts, so it states
coverage precisely rather than gesturing at it.

- **Generation 1 never reaches a probe.** It runs on a fresh stack, so
  `skipFirstProjectionRetract` (the #3624 first-generation skip) excludes the
  refresh row from `retractRows` and `RetractEdges` is called with zero rows. No
  probe, no delete.
- **Generation 2 runs the guarded delta path end to end.** The delta cassette's
  refresh row carries `delta_projection`, so it takes the by-file-path branch,
  which `executeGuardedRationaleDeltaRetracts` guards per label. The determinism
  matrix asserts an exact one-surviving `EXPLAINS` record at generation 2
  (`ifa_rationale_delta_expected_tuple="1|1|0|1|0|1|1|0"`, survivor
  `content-entity:e_763200c9adc3`) against the records written at generation 1,
  and the matrix was green across N=1/2/4 with identical digests on this branch.
  So the guarded path executes against a real backend and still produces the
  correct graph, which is what the gate is for.

  Be careful about what that does NOT prove, because an earlier draft of this
  section claimed it. It does not prove the probe answered `true` over Bolt. An
  *inert* guard reaches the same end state: on `unsupported` or `probe_error`
  the DELETE runs unconditionally, the same edges are removed, and the cell goes
  green identically. The assertion cannot separate "the probe returned true"
  from "the guard was inert and the delete ran anyway" — which is exactly the
  distinction the Observability section says the counter exists to make. No live
  cell asserts the probe outcome today: the reducer's structured logs do not
  reach the drive log the cell captures, so there is nothing for a cell to grep.
  Making this claim true would mean surfacing `observeRationaleRetractProbe`'s
  `outcome`/`scope` where a cell can read them, and asserting on it.

Four limits on that coverage, stated rather than glossed:

- **No live cell distinguishes an active guard from an inert one**, per the
  paragraph above. The positive branch over the shipped Bolt transport
  (`QueryCypherExists`) is proven by the Bolt-transport run recorded below --
  not by either matrix, and not by the measurement tables above, which were
  issued over the HTTP transaction endpoint and therefore never exercised the
  driver path. Every unit test substitutes a fake `ProbeExecutor` returning a
  scripted bool.

- **Only the `Function` label has rows to remove live.** The delta cassette
  exercises the other six labels' statements against nothing, so the matrix
  never watches them delete. #4367's committed table already proved the
  identical per-label MATCH deletes on all seven labels live, and the probe
  differs from that statement only in its terminal clause — but that is an
  inference from a sibling measurement, not something this matrix shows.
- **The whole-scope guard has no live-gate coverage.**
  `retractRationaleEdgesWithProbe` is not reached by either matrix. Its
  correctness rests on the unit tests, which drive the real `RetractEdges` path,
  plus the backend proofs above.
- **The mixed-batch path has no live-gate coverage either.** A batch mixing a
  delta-flagged repository with a whole-scope sibling is a partition-collision
  shape the cassettes do not produce. It is covered by unit tests at the
  `RetractEdges` dispatch layer, including the negative case that an unmarked
  legacy per-edge row must NOT pull a whole-repository delete.

## The probe answers correctly over Bolt, not only over HTTP

The measurement tables above were issued as scratch statements to the pinned
image's HTTP transaction endpoint. That proves the statement text and its match
semantics at the backend, but it never runs `QueryCypherExists`
(`go/cmd/reducer/neo4j_wiring.go`), which is the code production actually uses
and which decides the answer from `result.Next(ctx)`. The probe projects a
literal (`RETURN true LIMIT 1`) rather than a bound value, and this document
says elsewhere that a literal projection is not safe to assume — so proving it
on the wrong transport would have left the single assertion the guard rests on
uncovered. If Bolt materialised no record for that literal, `hasNext` would be
false, the guard would read a definitive zero, and every rationale retract would
be skipped silently: stale `EXPLAINS` edges, no error, no dead letter, and the
counter recording an ordinary `skipped`. Every unit test scripts the bool, so
the whole suite would stay green through exactly that failure.

It was therefore run against the pinned image over Bolt, driving the shipped
statements through the real `QueryCypherExists`. The statements were obtained
from the exported builders (`BuildProbeRationaleEdges`,
`BuildProbeRationaleEdgeStatementsByFilePath`) rather than retyped, so the text
under test is the shipped constant:

| shape | empty store | seeded | after deleting the seed |
| --- | --- | --- | --- |
| `probeCanonicalRationaleEdgesCypher` (multi-source) | `false` | `true` | `false` |
| `probeRationaleEdgesCypher` (single source) | `false` | `true` | — |
| the seven delta per-label probes | all seven `false` | `target:Function` probe `true` | — |

The delta line carries a second result worth keeping: with a single
`(:Rationale)-[:EXPLAINS]->(:Function {path})` edge seeded, the `Function` probe
answered `true` and the other six answered `false`. That is the per-label
pairing argument demonstrated at the backend rather than argued from the
statement text — one shared probe really would answer a narrower question than
six of the seven deletes it guarded.

Backend: `eshu-nornicdb-pr290:3722b483c02c`, the pin in `docker-compose.yaml`,
started with `NEO4J_BOLT_PORT=7699`. The harness was a scratch `package main`
test in `go/cmd/reducer` (so it could reach the unexported
`neo4jSessionRunner`), run with `SCRATCH_BOLT_URI=bolt://localhost:7699` and
deleted afterwards, in the same "not checked in as a harness" style as the
measurement runs. No result here is a timing claim, so nothing from this run
enters the measurement ledger.

What it still does not cover: it exercises single-node NornicDB, so it cannot
exercise the routed-follower case that motivates `AccessModeWrite` — the
in-source comment on `QueryCypherExists` already says so, and the direction of
that change is strictly safer either way.

## Verification

Run on this branch, on this machine, after the final source edit. Both live
matrices were re-run once the last edit landed rather than reusing an earlier
green: the gates compile their own binaries at start (`ifa_det_build_bin`), so a
run that began before an edit is proving the wrong tree even when the edit is
only a comment.

Credential-free:

```
cd go && go build ./...                                       clean
cd go && go vet ./...                                         no output, exit 0
cd go && go test ./internal/storage/cypher ./cmd/reducer \
  ./internal/reducer ./internal/telemetry \
  ./internal/graphbackpressure ./internal/ifa -count=1        6 ok lines
precommit-go.sh lint <45 changed files>                       5 package(s) from 45 path(s), 0 issues
precommit-go.sh filecap <same files>                          pass (positive control: 601-line file -> exit 1)
scripts/verify-telemetry-coverage.sh                          coverage doc and instruments.go agree
scripts/dev/precommit-go.sh measurement-citations             no uncited or unknown measurement claims
scripts/verify-ci-gates-registry.sh                           PASS: registry integrity + drift check
scripts/test-generate-ci-gates-doc.sh                         14/14
scripts/test-verify-ifa-determinism.sh                        pass (static mirror)
mkdocs build --strict --clean                                 0 warnings, 0 errors
git diff --check                                              clean
```

Live, both against a real Postgres + NornicDB stack:

```
scripts/verify-ifa-determinism.sh        exit 0
  PASS across N=1 2 4, digest aa0904cc09da0b95bf78a0f27dd1b5b0e2aec15c371e0077edb81312360a4998
  (identical at every worker count, and identical to a pre-fix run of the same
  matrix, so the guard did not move graph truth)

scripts/verify-ifa-fault-injection.sh    exit 0
  PASS, 18 cells
  baseline                digest=280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052
  killworkerrationale     digest=280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052
  failgraphwriterationale digest=280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052
```

The fault-injection digests are the part worth reading. `killworkerrationale`
blocks a `rationale_materialization` row mid-claim, `kill -9`s the live reducer,
starts a fresh one, and drains; `failgraphwriterationale` fails the production
`EXPLAINS` MERGE once and lets the retry succeed. Both land on the same graph
digest as the fault-free baseline, which is an idempotency proof for the guarded
retract: probe-then-delete re-executed after a crash or a failed write converges
to the same graph rather than to a differently-retracted one. The kill cell also
printed `non-vacuous: 1 blocked claimed/running row(s) observed`, so the fault
actually landed instead of the cell passing without exercising anything.

The `deltaretract` cell asserted the rationale generation-2 durable tuple
`1|1|0|1|0|1|1|0` and matched the exact delta-generation survivor record, and
the final cell asserted `domain=rationale_edges expected=3 full edge records
matched exactly`.

## Reproduce

Unit and contract coverage, credential-free:

```bash
cd go && go test ./internal/storage/cypher ./cmd/reducer ./internal/telemetry -count=1
```

The focused sets the performance reference cites:

```bash
cd go && go test ./internal/reducer -run \
  'TestRationaleHandlerEmitsIntentsWithDeltaRefresh|TestBuildRationaleRetractRowsKeepsMalformedDeltaScoped|TestLoadRationaleMaterializationFactsUsesSingleLegacyFallback' \
  -count=1
```

The backend proofs above are not checked in as a harness. They were run as
scratch statements issued directly to the pinned image's transaction endpoint;
the statement text is the shipped constant in
`go/internal/storage/cypher/edge_writer_rationale_labels.go`, and the ledger
rows record the image id and the commit the constants came from.
