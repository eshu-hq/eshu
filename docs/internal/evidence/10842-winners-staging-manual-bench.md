# Winners staging manual bench protocol (informational, not a CI gate)

This protocol watches uniform statement-cost drift of the direction-C
staging INSERT and the collapse merge without quiet iron, shared runners,
or CI enforcement. It is release-time and informational: CI neither runs
nor thresholds it.

## One-time setup

```bash
docker run -d --name bench-pg -e POSTGRES_PASSWORD=benchproof \
  -p 127.0.0.1:15433:5432 postgres:18
```

## Run (one command, three env vars)

```bash
export ESHU_POSTGRES_TEST_DSN='postgres://postgres:benchproof@127.0.0.1:15433/postgres?sslmode=disable'
export ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN="$ESHU_POSTGRES_TEST_DSN"
export ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE=1
cd go && go test ./internal/storage/postgres/ \
  -run 'TestSupplyChainImpactWinnersStagingCollapseWorstPartitionCostLive' \
  -count=1 -v
```

No separate bootstrap step: the harness creates its own disposable database
and applies the full schema. No quiet-host requirement: there is no bound
to void, only numbers to record.

## Fixed seed shape

The worst-partition cell from the collapse live test: 24,300 distinct
staged keys (`pkg:worst:%05d`) inserted via the production capture INSERT
in batches of 180, then one unbounded collapse covering both the fact and
scope arms with conservation asserted (24,300 groups, 24,300 version sums).
Do not substitute the retired 60-reverse-K contended-upsert seed: direction C
ships a plain staging INSERT, and K-order now lives in the collapse.

## What to record per run

Run at least 5 times and report the median: the `worst_partition`
log line's collapse wall time, `heap_before` / `heap_after`, and
`dead_before` / `dead_after`. Capture the `EXPLAIN (ANALYZE, BUFFERS)`
output (or a plan hash) for both collapse SELECT arms alongside the
median — for example with the existing ExplainPins live test — so a plan
change is checkable from the protocol's own records. Record alongside
every number a hardware block: CPU model and count, RAM, runner type
(local vs CI), PostgreSQL version, and Go toolchain. Numbers without
the hardware block are not comparable — do not compare across
hardware classes.

## Reopen rule

Reopen bench-backstop work (collapse-first, coarse-threshold, DSN-gated)
when any of these holds: an `EXPLAIN` plan change on the staging INSERT
or either collapse arm; staging-depth growth that makes the collapse the
measured bottleneck; or capture/collapse timing more than 2x the recorded
baseline **on the same hardware class**. Never a global
`BENCH_REGRESSION_ENFORCE` flip: a flip needs a like-for-like baseline
recapture plus an explicit owner order.

## Non-gate clause

This protocol is informational / release-time. CI does not run it and
does not threshold it. A red CI gate is never the enforcement mechanism
for drift; the hermetic shape pins in
`supply_chain_impact_winners_dirty_upsert_test.go` are the CI backstop.
