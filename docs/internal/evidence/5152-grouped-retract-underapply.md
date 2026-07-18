# #5152 — grouped retract under-apply on NornicDB v1.1.11

The #4367 cloud-edge slice routed only the `*ByUIDs` retract variants through the
sequential `dispatchRetract` (plain `Execute` per statement). Seven live
whole-scope retract paths still dispatched their DELETE/REMOVE through the grouped
`dispatch` (`ExecuteGroup`, a managed transaction), which under-applies on the
pinned production NornicDB `timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174a`.
This routes all seven through `dispatchRetract`; MERGE-shaped writes stay grouped.

Rerouted (all live, confirmed by caller search):
- `azure`/`gcp`/`aws` CloudResource edge `RetractCloudResourceEdges`
- code-taint `RetractCodeTaintEvidence` + `RetractStaleCodeTaintEvidence`
- `ec2_block_device_kms` + `ec2_internet_exposure` node retracts (added
  `dispatchRetract` to both)

Classification: **Correctness win** + **Performance win**.

## Live before/after (pinned v1.1.11, Bolt driver, DB nornic, 200 seeded/trial)

`managed` = `session.ExecuteWrite(tx.Run)` (what `ExecuteGroup` does); `autocommit`
= `session.Run` (what `Execute` does). Same retract Cypher constants shipped by the
writers.

### Correctness — survivors after retract (want 0)

| retract | grouped (managed) | sequential (autocommit) |
| --- | ---: | ---: |
| DELETE edges (`retractCloudResourceEdgesCypher`) | **200** (deletes 0) | **0** (all deleted) |
| REMOVE props (`retractEC2InternetExposureNodesCypher`) | **200** (removes 0) | **0** (all removed) |

The grouped DELETE under-applies intermittently (matches the documented
"inconsistent by internal label-iteration state"); the grouped REMOVE under-applies
**deterministically** (removed 0 across every run). Either way the grouped path
silently leaves stale edges/properties; the sequential path deletes/removes all,
reliably, every run. The REMOVE result confirms the two EC2 node writers are real
bug fixes, not defensive consistency.

### Performance — retract wall-time (lower is better)

| retract | grouped (managed) | sequential (autocommit) | delta |
| --- | ---: | ---: | --- |
| DELETE edges (median of 5, interleaved orders) | 58.5ms | 45.1ms | seq ~1.3x faster |
| REMOVE props | 15.5–19.2ms | 8.9–12.0ms | seq ~1.6x faster |

The sequential auto-commit retract is both correct and consistently faster: the
managed transaction pays begin/commit round-trip overhead (and, for DELETE, does
the wrong thing anyway). Auto-first and managed-first orderings were interleaved to
cancel warmup bias; the direction (sequential faster) held both ways.

## Verification

- `go test ./internal/storage/cypher -run TestNonUIDRetractsRouteThroughAutocommitExecute -count=1`
  — each of the 7 retracts makes exactly one `Execute` and zero `ExecuteGroup`
  calls on a `GroupExecutor`-capable recorder. Proven RED on the grouped path
  (reverting one reroute fails the subtest).
- Backend-required live proof above, on the pinned v1.1.11 image.

Performance Evidence: On pinned NornicDB v1.1.11, the sequential (auto-commit)
retract path measured ~1.3–1.7x faster wall-time than the grouped
(managed-transaction) path for the CloudResource-edge DELETE (45.1ms vs 58.5ms
median of 5, interleaved) and the EC2 internet-exposure property REMOVE (8.9–12.0ms
vs 15.5–19.2ms), for 200 seeded edges/nodes, while also correctly removing all
target rows where the grouped path left 200 stale. Input cardinality: 200 target
edges/nodes; backend: timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174a; measured
via the Bolt driver mirroring Execute vs ExecuteGroup.

Observability Evidence: The retract path already emits per-statement execution
through the shared executor; routing changes the transaction mode (managed →
auto-commit) but not the statement metadata (phase/entity-label/summary keys are
preserved on every retract Statement), so operator-facing per-phase execution
telemetry is unchanged in shape while the retract now actually applies.
