# #10972 winners keyed-cap squeeze evidence (temporary)

Refs #10972. Temporary drain act: `defaultWinnersKeyedMaxIdentifiers`
50000 -> 5000 in `go/internal/storage/postgres/supply_chain_impact_winners_keyed.go`,
plus a matching field-doc line in
`supply_chain_impact_canonical_winners_rebuild.go`. `MaxKeyFraction` stays
0.5. No query, lock, batching, or concurrency change: ticks that trip the cap
run the existing guarded full rebuild in the same transaction (`overKeyedCap`
-> `resweepFull`). Revert immediately after the drain; the revert restores the
constant, its comment, and the field doc together.

## No-Regression Evidence:

Baseline (QA logs, pre-act): keyed tick durations 10.6-214.5s per
200-identifier chunks; `overKeyedCap` never firing at ~6,400 identifiers
against the 50,000 cap, so ticks paid the keyed path per chunk. Input shape
during the drain: ~6,400-identifier expansions, unchanged code path inputs.
Expected post-squeeze: every non-empty tick trips the existing `overKeyedCap`
guard and takes the guarded full path until the revert; empty ticks are
unchanged. Backend: production Postgres (same version/topology as the QA
baseline); no migration, no index, no plan change, so no EXPLAIN delta is
claimed. Terminal counts: dirty-log depth drains to 0 and the winners table
matches a fresh full recompute (the standing keyed-equals-full invariant);
post-act drain measurements (tick durations, full-transaction wall time, final
counts) to be recorded on #10972. Abort criteria: any full transaction
exceeding its bound, or holder-read / watermark-stale spikes beyond the
pre-act baseline. Safe because the full path is the long-standing backstop
the keyed tick already falls back to, byte-for-byte the same statement.

## No-Observability-Change:

No metric, span, log, or wait kind added or removed. Ticks keep reporting the
existing resweep signals: `path` (`full` vs `keyed`) on the result and spans,
`eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds` phase
timers, resweep duration/keys/identifiers/capture-rows/backstop-repairs and
`dirty_log_depth` / `dirty_log_oldest_age_seconds`, the
`pg_try_advisory_xact_lock` resweep single-flight, and the `winners_watermark`
advisory wait kind with its in-transaction wake. The drain watch reads the
committed winners watermark (`Watermark()`), `dirty_log_depth` and
`dirty_log_oldest_age_seconds` for lag, and the holder-read / watermark-stale
wait counters against the pre-act baseline.
