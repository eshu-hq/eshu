# Evidence: per-item projection failure isolation (#4464)

Moved from the bootstrap-index README (which is capped at its grandfathered
length) so the README keeps only the contract. The contract itself is
described in the README section "Per-item projection failure isolation (#4464)".

Performance Evidence: reproduced on a full local compose run of 909 repositories
against the NornicDB branch build (arm64-metal-bge, orneryd/NornicDB#230),
runtime `v0.0.3-pre-release-17`, filesystem repo source. Baseline (pre-fix): a
single `structural_edges` Helm `REFERENCES` MERGE hit the 30s
`ESHU_CANONICAL_WRITE_TIMEOUT`, canceled the shared context, failed all 8
projector workers, and crashed bootstrap-index at ~repo 87; projection
throughput fell from ~3 items/min to 0 and stayed there, with 155 `source_local`
work items stuck in-flight (leases expired, never reclaimed) and 0 dead-letters
(`get_ingester_status` `work_item_status_counts`; `oldest_inflight` grew
620s→2451s). After: per-item failures route to `WorkSink.Fail`, the run drains
to completion, and failed items land in retry/dead-letter instead of wedging.

No-Regression Evidence: the happy path is unchanged —
`isolateBootstrapProjectorFailure` runs only on a work-item error; a
fully-succeeding drain calls `Ack` on every item exactly as before.
`go test ./cmd/bootstrap-index -race` green; `make pre-pr` all gates pass.

Observability Evidence: failed items are now visible as `retrying`/`dead_letter`
rows in `fact_work_items` (and via `get_ingester_status` `domain_backlogs`) plus
the existing `bootstrap projection failed` structured log
(`failure_class=projection_failure`), replacing a bootstrap-index process crash
that produced no queue signal. No new metric is introduced.

Why safe: mirrors the continuous projector (`internal/projector`
`Service.processWork`) failure-isolation pattern; only a Claim failure or a
`Fail`-path write failure remains fatal; genuine shutdown cancellation is not
dead-lettered. Backend: NornicDB (default). Terminal state after fix: queue
drains; failures bounded to retry/dead-letter.
