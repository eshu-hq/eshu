# Suppression plan helper enrollment (#7533)

`go/internal/query/supply/chain/impact/suppression_plan_proof_live_test.go`
contains no `Test` root. Its four helper functions are called by
`TestSupplyChainSuppressionPathsPerformanceLive` in the already enrolled
`suppression_paths_performance_live_test.go`. The readiness inventory therefore
tracks the helper as one pinned source dependency of that consumer. It does not
add a Go invocation or a test root.

## Preimplementation proof

The current `main` run [38997937360](https://github.com/eshu-hq/eshu/actions/runs/38997937360)
checked out `271d54e54672deb73a8f1743761de19f4ab8163f`. Its four readiness
shard reports passed the production aggregate: 411 of 411 planned runs.
Shard 1 job 117294524860 ran the owner test exactly once with status 0. The
native job log reported `TestSupplyChainSuppressionPathsPerformanceLive: PASS`
with elapsed time 24.070 seconds and the impact package at 11 of 11 PASS.
The CI runner verified Go JSON events before publishing its derived report;
raw JSON was not retained for local replay.

The reviewed source SHA-256 pins were
`2f5767807ad4d47896193e2fe4bd291161aab845c61983dbfef8659338cfaa7e`
for the helper and
`0f9bba8d97de2d4cb3c32b9571ef86e1eab56486558cf4fc4765f26a0f1a32ad`
for the consumer. The readiness ledger guard checks those complete files,
package declarations, build tags, the absence of a helper `Test` root, and
exact consumer enrollment before Go starts. A source edit requires renewed
review and runtime proof before repinning.

## Enrollment boundary

The one helper ledger row moves from `scheduled` to `postgres_ci`, with the
existing readiness runner. The executable inventory remains 173 files and
411 roots; the helper adds one source file and zero roots. The package list,
selectors, shard assignment, Go commands, environment, workload, images, and
timeouts remain unchanged. The legacy scheduled ledger count changes from
418 to 417 for this row. This is one original-cohort disposition, not closure
of the #7533 inventory.

Hosted proof of the completed guard on the eventual PR head remains required.
