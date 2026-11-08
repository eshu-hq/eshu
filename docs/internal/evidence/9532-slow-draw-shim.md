# Slow-draw shim evidence for the #9532 HOLD (warm era, current cut)

Source: offline shim reported in
[issuecomment-6721451958](https://github.com/eshu-hq/eshu/issues/9532#issuecomment-6721451958);
decision: [draw-hold ruling](../arbiter/9532-draw-hold-ruling-2026-11-08.md)
(arbiter-eshu, 2026-11-08, HOLD — six shards, 30-minute cap, no retry, no
timeout raise). This note files the numbers; nothing landed with them.

## Method and pool

Throwaway scorer in `/tmp` reusing the committed refresher parser plus
`v2.Scorer`; scratch only, no repo change. Pool: 7 green Nov 7–8 main runs
(42410520335..42451082044), all warm on all 6 shards, 933 executed tests,
scored under the committed pooled-p75 weights (8-run pool
41911058891–41952209683).

## Scratch-only scorer tolerances (both validated)

- `LIVE_READINESS_EXTRA_SKIP` excuse lines filtered: lenient extraction proved
  byte-equal to strict parse wherever strict succeeds.
- The 1 skipped test (`TestRetirementDerivedStatementPlansLive`, shard 5)
  leaves the summary counting planned tests (planned = executed + excused).
- Cancelled Oct shard scored on its full 127-test slice; a partial-log basis
  would inflate its ratio.

## Results

- Warm per-shard job ratios (observed wall / predicted job s), n=42: p50 0.960,
  p90 1.043, p99 1.111, max 1.130, 0 over 1.15. Shard-1 slice, n=7: p50 1.071,
  p90 1.102, p99 1.127, max 1.130. Whole-pool max wall 1182 s.
- Cold-era contrast (era-mixed by rule, not a draw measurement): 5 Oct runs are
  pre-restore-step (cold) and score p50 1.298 under warm prices — the cold
  penalty, not a runner draw. All-cold Nov run 42476140787 (green): ratios
  0.985–1.421, max wall 1474 s < 1800. Cancelled 40915510776 shard-1 (cold):
  wall 1821 s, full-slice ratio >= 1.311, right-censored lower bound.
- Last-40 main runs: 37 success, 3 failure — all 3 are test failures at ~13–19
  min walls, none a cap cancel. Zero warm-era cap cancellations exist to score.
- Modeled per-job limit: 1800 / 1.111 = 1620 s (draw bound for the record, not
  a gate; the operative gate stays 1440 s predicted).
- Smallest fitting shard count: 6-way live plan, largest 1127.4 s <= 1620,
  spread 6.60% <= 15%, appended window clean 0..120 (>= 38), max row 483.3 <=
  608. The 5-way exact-search exhibit (guard bypassed, unsupported) lands at
  1225.3 s with ~60 s of cold margin (1225 x 1.42 = 1740); the v2 planner only
  supports 6. The 7-way exhibit (952.7 s) needs planner/gate/weights work.
- Costs: mean 999 s/job, ~100 runner-min per 6-shard run. A breach needs >=
  1.60x draw on the largest job; observed warm max is 1.130, cold max 1.421.
  Timeout 30→35 converts a future breach into a slow success but masks hangs;
  per-shard retry helps single-shard flakes only (cold hits all six shards, so
  retry-all is a full rerun); 7 shards cost ~+6–8% runner-min every run against
  a tail never observed warm.

## Arbiter corroboration of record

The arbiter independently reproduced the live plan (shard 1 = 1045.9 s,
largest = 1127.4 s, 934 tests, no budget errors), the last-40 outcomes
(37 success + 3 failure + 0 cancelled), the all-cold walls, and the config
state (6 shards, `timeout-minutes: 30`, restore-only cache). The interim
shard-1 re-run rule was dropped: it never triggered, and slow draws hit
shards 1–6 at the same rate (64% vs 65%).
