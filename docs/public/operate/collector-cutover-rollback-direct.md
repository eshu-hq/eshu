<!-- docs-catalog
title: Roll back a direct in-tree collector
description: Runs the collector cutover rollback for an in-tree producer that has no collector instance and no work items, such as the documentation collector.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# Roll back a direct in-tree collector

Use this page with the [collector cutover rollback](collector-cutover-rollback.md)
runbook when the in-tree producer is a direct collector. A direct collector runs
as its own Deployment. It has no `collectorInstances` entry, no collector
instance ID, and it writes no `workflow_work_items` rows. The coordinator does
not plan it.

| Collector kind | In-tree producer | Rollback mode |
| --- | --- | --- |
| `jira` | Coordinator-planned collector instance | `--in-tree-instance` |
| `pagerduty` | Coordinator-planned collector instance | `--in-tree-instance` |
| `documentation` | Direct collector Deployment (Confluence) | `--in-tree-direct` |

The mode is not tied to the kind. Use it for any collector that runs without an
instance and commits scope generations. The documentation collector is the only
collector with a rollback path today.

## Options

Replace `--in-tree-instance` with `--in-tree-direct`. The two options exclude each
other, and the default mode still refuses an empty `--in-tree-instance`.
`--in-tree-deployment` is still required: it is the Deployment that step 4 scales
back up. `--restore-applied-by-gitops` works as before.

```bash
scripts/collector-cutover-rollback.sh plan \
  --collector-kind documentation \
  --external-instance <external-instance-id> \
  --in-tree-direct \
  --in-tree-deployment <direct-collector-deployment> \
  <the other options from the runbook>
```

## What changes in each step

| Step | Direct mode |
| --- | --- |
| 1 | No change. The external activations, the extension worker, and the coordinator are handled as in the runbook. |
| 2 | Every claim of the kind counts as external, because the in-tree set is empty. The drain waits for all of them. |
| 3 | No change. |
| 4 | The restore scales `--in-tree-deployment` and waits for its rollout. The plan prints that command. The coordinator is not restarted a second time, because it does not plan a direct collector. The replay window starts at the exact database clock, with no skew margin. |
| 5 | Passes only when a scope generation of the kind was observed at or after the window start and has been activated. |
| 6 | No change. Fact truth must equal the baseline. |

### Dual authority

A direct collector holds no claim row, so the claim table cannot show it. The
script uses the restored Deployment instead. From step 5 on the Deployment is
running, so a claim of the kind beside it belongs to the external producer, and
the script exits 7. Before the restore, the same claim is a refusal (exit 4).
The script assumes the Deployment is running from step 5 on; it does not read
the replica count. Keep the Deployment at zero replicas until step 4 scales it
up, so that assumption holds. A standalone `verify` run reports that same
assumption for any claim of the kind.

### The step 5 signal

The script reads `scope_generations` joined to `ingestion_scopes` for the
collector kind. A generation counts when its status is `active` or `superseded`,
its `ingested_at` (the time the collector observed the source) is at or after the
window start, and its `activated_at` (the time the projector made it active) is
also at or after the window start. The query is one small SQL file,
`scripts/lib/collector-cutover-rollback-direct-commit.sql`.

The documentation collector emits each generation as pending, and the projector
sets `activated_at` later. `activated_at` alone would count a generation the
external producer observed before the rollback began and the projector made
active during it. The `ingested_at` check rejects that case. `ingested_at` comes
from the collector pod's clock and the window start from the database clock. A
collector clock behind the database clock by more than the poll delay only makes
step 5 time out. One ahead by more than the gap between stopping the external
producer and the window start could pass early; that gap is minutes.

Prometheus counters such as `eshu_dp_facts_committed_total` cannot be read from
the database connection the script already uses, and a counter resets when the pod
restarts. The generation row is durable and carries the time.

A source that did not change mints no new generation. The generation ID is a
content hash, and a published generation is never reopened. Step 5 then times out
with exit 5 instead of reporting a resume it did not see. Change a page in the
source (or wait for the next real change), then rerun from step 5 with
`--from-step 5 --since <replay_since from step 4>`.

## Verify and capture

`verify` and `--capture` take `--in-tree-direct` where the other pages show
`--in-tree-instance <id>`, and no `--in-tree-deployment`:

```bash
scripts/collector-cutover-rollback.sh verify \
  --collector-kind documentation \
  --external-instance <external-instance-id> \
  --in-tree-direct \
  --baseline baseline-documentation.txt
```

## Evidence to record

Record the step 4 `EVIDENCE` line (replay start, rollout, and
`coordinator='not restarted ...'`) and the step 5 line:
`direct_observed_generations=<n> latest_activated_at=<time>`.
