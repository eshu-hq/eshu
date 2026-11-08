# 9740 retest early check 2026-11-08 (~18:05Z, moved-up clock)

Lane: mode 2b then mode 2a with `--end now` on QA Prometheus
(`PROM_URL=http://127.0.0.1:9090`, tunnel verified healthy via `/`-`healthy`
before use). Script: `docs/internal/arbiter/9740-retest-eval.sh`.
Issue comment: `https://github.com/eshu-hq/eshu/issues/9740#issuecomment-6725090768`.
#9740 left open (follow-up PR unmerged).

## Mode 2b (trailing 48h: 2026-11-06T18:03:16Z .. 2026-11-08T18:03:16Z)

- Q1 series-with-samples per outcome (`..._impact_wait_total`): empty result.
- Q2 48h counts per outcome: empty result.
- Q3 reconciliation execution denominator (same 48h): `0`.
- Verdict: condition 2 NOT_CHECKED — no same-generation reconciliations ran,
  so deferred_in_flight cannot be compared with any pre-rollout rate.

## Mode 2a (14d: 2026-10-25T18:03:18Z .. 2026-11-08T18:03:18Z, 5m steps)

- Peak ratio **0.5891** at 2026-11-02T15:48:18Z (flagged=191.13, exec=324.46;
  beside reopened=38.23, pending_noop=2366.98), with a 30m strict-rise window
  ending at/after it.
- **8 firing steps**, 2026-11-02T15:38:18Z .. 2026-11-03T15:33:18Z.
- 871 strict 30m rise windows in the 14d span.
- Today 2026-11-08 (00:00–18:00Z): max **0.1691** (216 steps). Trailing 48h
  max: **0.2108** at 2026-11-06T21:08:18Z.
- Verdict: condition 1 fires 11-02/11-03 only; NO FIRE today or in the
  trailing 48h. Rollout-wave/drained status of the 11-02 burst NOT_CHECKED
  in this lane (ruling item (d) belongs to the coordinator).

## Lane verdict

Neither trigger condition fires today. No build, no close from this lane.
