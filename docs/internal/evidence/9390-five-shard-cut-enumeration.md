# Exhaustive five-shard cut search for the live-plan spread window (#9390)

Moved from the [Live PostgreSQL readiness shards](../../public/reference/local-testing/live-postgres-readiness-shards.md)
page ("The live-plan spread window") so the page keeps room under its 500-line
cap for the figures each pooled refit records (#9519). The text is unchanged.

Classification: diagnostic-only. It records a past search on the five-shard
model; it measures no wall time and does not describe the current six-shard
weights.

## The search

The maintainers checked by exhaustive
enumeration at `613ee359ea`, before the #9436 refit and the #9435
enrollment. The figures in this part price that plan with the weights
committed then (from runs 40603534686 and 40585622864) and with a refit
candidate that was later rejected; no exact search has run on the current
weights. It lists every contiguous five-shard cut whose
five jobs each stay within 1440 s. For the sweeps over 0 to 60 appended tests
that is up to 5.3 million cuts per plan; the extension to 110 used an extra
sound prune. It prices them with the same `v2.Scorer` the production planner
uses, and tests the spread in integer tenths exactly as
`live_postgres_readiness_weights.py` does. The search tested only the 1440 s
cap and the 15% spread; the 675 s row check of that time never decided an outcome. It
searched only default-priced tests appended to the last package, not other
growth positions or priced tails.

Six sweeps (main, #9320, and #9227 plus main, each under the weights committed
then and the refit candidate, 0 to 60 appended tests) found that the planner
accepted exactly the plans that have a valid cut. On that main under the
weights committed then, the live-plan fit case failed from 45 through 65
appended tests. For 45
through 64 no valid contiguous cut existed, so the planner was right to raise
there. The sweeps covered 0 to 60; an independent enumerator checked 61 to 64
separately. At 65 a valid cut existed and the planner skipped it (below). A
cross-check on that main under the same weights agreed on the sampled rows (0,
44, 45, 56, 60, 64, 65, and 66). For the other plans the sweeps also ran 0 to
60; their edges beyond that come from the extension to 110.

The planner does skip a valid cut at the one place the sweeps found: the
re-entry edge, where the window is about to close. The planner picks the cut
with the highest smallest job, then the lowest largest job, then the lowest sum
of squares, and it checks the 15% spread afterwards on that one cut. It never
searches for the spread. At the edge, that cut splits a small package and pays
its first-use price twice, which pushes shard 5 just under the floor. On that
main with the weights committed then, at 65 appended tests, the planner's cut
had a 15.11% spread and a cut at 14.93% existed. The same edge appeared at 68
appended tests on #9320 and 62 on #9227 plus main (14.92% and 14.89% there).
The rejected candidate showed no such edge up to 110 appended tests. These
figures, the edges and the 110 bound come from the maintainers' exact search,
not from this repository; whether the current weights have such an edge near
72 appended tests, where the current window closes, is not measured. #9390 tracks the planner fix. Until it lands, a spread raise at the
edge of a window is not proof that no cut exists.
