# 7009 unscoped entity search: measured figures and contract completion

## Context

Issue eshu-hq/eshu#7009, entity row. #8036 shipped the budgeted walk for
`content_entities` with the file figures (probe 200, step 500, phase one
0.55 x budget, tail cap 0.50 x budget, floor 150 ms), never measured on that
table. #8044 folded that walk into one engine with two table shapes and no
behavior change. This change gives the entity shape its own figures, measured
by the #7009 S1 shim in the QA environment (triple A: probe 1,000, step 5,000,
phase one 0.20 x budget, tail cap 0.80 x budget, floor 150 ms, at the 800 ms
default), labels the six unscoped-search instruments by table, refuses a
cursor sent to the other route, and states the MCP byte-cap precedence.
Figures marked "S1" come from `7009-entity-shim-s1-results-20261012.md`;
figures marked "fake clock" come from the committed unit tests, which run a
synthetic, scaled key space and are not deployed walls.

## Deviation from the ruling: the 160 ms large-value row

Arbiter ruling round 2, R2.3 row (ii), predicted that at a 160 ms budget the
entity tail cancel lands inside the 13.1 MB value's recheck with `overrun_ms`
over 100, so the reason is `budget_exceeded_on_large_document`. The S1c lag
table does not support that.

S1c, QA warm, cancel lag past the timeout (ms), for the three patterns whose
tail candidates hold the 13.1 MB value:

| Timeout (ms) | decode | process_entries | from_event |
|---|---|---|---|
| 60 | 100.2 to 116.6 | 111.8 to 127.3 | 107.6 to 124.8 |
| 90 | 70.6 to 72.2 | 83.2 | 77.7 to 80.8 |
| 120 | 40.2 to 41.0 | 50.8 to 51.1 | 48.2 to 48.7 |
| 150 | 10.7 | 20.5 to 21.0 | 17.6 to 18.0 |

The block ends about 170 ms into the tail (timeout plus lag is 160 to 187 ms
inside it). At a 160 ms budget the entity plan scales to phase one 32 ms, tail
cap 128 ms, floor 30 ms. One probe and one step cost about 27 ms (fake clock),
so the tail timeout is `min(160 - 27, 128) = 128 ms`. The cancel lands at about
170 ms: `overrun = 170 - 128 = 42 ms`, under the 100 ms threshold, so the
reason stays `candidate_budget_exceeded`. An overrun over 100 ms needs a tail
timeout under about `170 - 100 = 70 ms`.

The coordinator corrected the ruling and the tests pin three rows:

| Row | Budget | Tail timeout | Overrun | Reason |
|---|---|---|---|---|
| (i) | 800 ms | about 638 ms, outside the block | 3 ms | `candidate_budget_exceeded` |
| (ii-a) | 160 ms | 128 ms, inside the block | 42 ms | `candidate_budget_exceeded` |
| (ii-b) | 100 ms (MinBudget), 40 ms probe | 60 ms, inside the block | 110 ms | `budget_exceeded_on_large_document` |

With these figures the tail timeout is about 0.8 x budget, so budgets under
about 220 ms put the tail cancel inside the block (extra wait up to about
140 ms), and the large-document reason needs a tail timeout under about 70 ms
(S1c, QA warm). The docs page states both.

## Performance and observability summary

Performance Evidence (#7009): the entity figures are triple A of the S1 shim
on `content_entities` in the QA environment. S1 section 4: an ordered window
costs 0.3 ms plus 0.34 us per row plus 10.2 ns per byte, so 160 ms walks about
35,000 rows. S1 section 5: rare bitmap tails cost 400 to 462 ms warm. S1
section 6, model at limit 10: every class that is exact today stays exact,
warm and cold, with a 1.25 uniform cost margin; the file figures turn
`process_entries` (exact in 462 ms today) and `FeaturesTab` (400 ms) partial
at 804 ms. The one disclosed trade (R2.2): `decode` at limit 200 is a partial
of 149 rows at 790 ms (model) that one resume completes; limits 10, 50 and
100 stay exact in one call. Proof in this change: `TestEntityPolicyClasses`
pins 21 classes on a fake clock (scaled key space, synthetic positions), with
outcome, rows, `tail_ran`, `tail_cancelled`, `ended_in`, an elapsed band, an
overrun band and the reason; run on the old figures it fails 16 of them,
including `process_entries` partial with 2 rows and `FeaturesTab` partial
with 4 rows. Seven seeded REDs (each run once and reverted) prove the guard
rows: triple B turns the R2.2 row exact; a 120 ms tail cap with a 50 ms floor
moves row (i) into the block; a 40 ms large-document threshold flips row
(ii-a); a walk that never reports the large-document reason fails row (ii-b);
a 200 ms tail cap cancels the selective tail; skipping the tail after a match
turns the early-match-selective class partial; a 480 ms tail cap cancels the
half-early-rare tail. The disposable PostgreSQL live tests pass on the new
figures: the five file live tests, the three entity live tests (row equality
with the old statement, plan shapes, edge rows) and
`TestResumeChainCrossesLargeDocumentLive`. The edge-row test
(`TestSearchEntitiesUnscopedEdgeRowsAreExactLive`) reads the probe and step
sizes from the span attributes of a zero-match walk, plants its edge token on
the last row of the probe window and of every step window and on the row after
each (rows 1,000, 1,001, 6,000, 6,001, 11,000 and 11,001 of the 12,000-row
fixture), and requires the probe, at least two steps and a completed tail to
return the old statement's six rows. The plant positions follow the figures, so
a later figure change moves them. An earlier fixture planted the token at the
200-row probe and 500-row step edges, which are not window edges under these
figures: an edge OFFSET past the window and an inclusive keyset each passed it.
With the token on the new edges both mutants fail it (walk rows 5 against 6,
and 7 against 6) and the clean tree passes; with the figures changed to 700 and
3,000 rows both mutants still fail. The budget sweep of this test is now a
1.2x geometric climb from 100 ms to 10 s, because with 5,000-row steps only a
narrow budget band runs the probe, two steps and then the tail, and the band
moves with host speed. Fixture walls of
`TestSearchEntitiesUnscopedMatchesOldStatementLive` (small seeded table, three
interleaved rounds, not a production figure): selective 169 to 203 ms and zero
match 167 to 198 ms with the new figures against 228 to 246 ms and 206 to
278 ms with the old figures; dense, wildcard, medium and upper-case classes
stay at 2 to 15 ms. A rare class pays phase one before its tail, as S1
section 6 states. A scratch fake-store benchmark (not committed, not a
production figure) put the CPU cost of one modelled call at about 0.02 ms for
`decode` at limit 10 under both figures, and 1.1 to 1.2 ms against 17 to
71 ms for the limit-200 and `FeaturesTab` classes, because 5,000-row steps
issue fewer statements.

The entity golden changes only by figure-driven values: statement window
arguments (200 to 1,000), statement timeouts, the phase outcomes those drive,
and `search.phase_one_cap_ms`. Two fixtures that assumed a 200-row probe now
size their probe cost for 1,000 rows. The file golden is byte-identical
through the figure change.

Observability Evidence: the six unscoped-search instruments
(`eshu_dp_content_search_unscoped_total`, `..._unscoped_duration_seconds`,
`..._unscoped_overrun_seconds`, `..._tail_cancel_total`,
`..._step_cancel_total`, `..._no_progress_total`) now carry
`table` = `content_files` or `content_entities` from the walk's shape, through
the new closed dimension `MetricDimensionTable`. The span adds `search.want`
(offset + limit + 1). `TestUnscopedSignalsCarryTheTableLabel` sees the label
on every data point of all six instruments on both routes; both goldens
change only by the label and the attribute. At 3 AM an operator reads
`eshu_dp_content_search_unscoped_total{table="content_entities",outcome="partial"}`
to see whether the entity budget cuts searches short, and the no-progress
counter by table for dead-end chains. No signal carries the search pattern.

## Contract completion

- A cursor that carries `start_line` or `entity_id` sent to the file route is
  HTTP 400; a non-zero cursor without `entity_id` sent to the entity route is
  HTTP 400 (ruling round 1, 2.3). A zero cursor starts at the first row on
  either route.
- When the MCP byte cap trims a partial entity page, one reply carries
  `partial` and `next_offset`. The tool text, the OpenAPI description and the
  docs page say: page with `next_offset` first, keeping the same cursor;
  follow `partial.cursor` only from a reply without `next_offset`. A dispatch
  test reads every row once in that order; the reversed order lost 42 of 100
  rows when run as a seeded RED.
- The OpenAPI entity search gains its request `cursor` and response `partial`.

## NOT_CHECKED

- A deployed read of these figures (ruling round 2, R2.5, and the collision
  ruling RA-1 to RA-5): that is the acceptance read after the roll.
- Cold costs, p95 under load, concurrency of partial chains, the HTTP wall.
- Any S1 figure beyond what the S1 note reports; no statement was rerun.
