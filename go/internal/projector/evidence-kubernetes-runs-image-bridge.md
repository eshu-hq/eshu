<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Evidence: live Kubernetes workload → OCI image bridge enablement

## Change

Two scope-generation reducer intent builders were added to the projector so the
live-Kubernetes → OCI supply-chain bridge actually runs:

- `buildKubernetesWorkloadMaterializationReducerIntent` enqueues the
  `kubernetes_workload_materialization` additive domain so the `KubernetesWorkload`
  node (design #388) commits. The handler was registered and wired but never
  received an intent, so the node never materialized.
- `buildKubernetesCorrelationMaterializationReducerIntent` enqueues the
  `kubernetes_correlation_materialization` graph-write domain so an exact image
  correlation decision is promoted into the `RUNS_IMAGE` edge to the digest-pinned
  OCI manifest node. The edge intent carries the workload domain's acceptance-unit
  key so the canonical-nodes readiness gate matches the phase the workload domain
  publishes (mirrors `workload_cloud_relationship`).

Both fire at most once per scope generation that observed a live workload
(triggered by the pod-template fact), matching the existing per-scope
`kubernetes_correlation` builder.

## Performance

No-Regression Evidence: The two new builders are O(1) per scope generation — each
scans the generation's input facts once for the first `kubernetes_live.pod_template`
envelope (the same single-pass the pre-existing `buildKubernetesCorrelationReducerIntent`
already performs) and appends at most one bounded, per-scope reducer intent. No new
per-workload or per-image fan-out is introduced, no hot graph-write loop changes,
and the additive reducer domains were already registered (only the enqueue was
missing). Net steady-state cost is two extra scope-keyed work items per live-cluster
generation, which the reducer claim path already bounds by conflict key. The B-7
golden-corpus gate drains the full corpus (drain→maintenance→drain) in ~38s with
`fact_work_items_residual=0`, unchanged from the prior required-correlation set.

## Observability

No-Observability-Change: This change enqueues two already-instrumented additive
reducer domains; it adds no new metric, span, or log key. The existing reducer
work-item, drain, and per-domain dispatch signals (`eshu_dp_reducer_*`) already
cover the two domains — an operator sees the `kubernetes_workload_materialization`
and `kubernetes_correlation_materialization` work items flow through the same
claim/lease/complete instrumentation as every other reducer domain. No new
observability surface is required to diagnose the bridge at 3 AM.
