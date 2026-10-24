# Component Extension Activations In Helm Evidence (#8894)

Evidence for `componentExtensionCollector.activations`, the Helm values that run
extracted collectors through the component-extension worker. Part of #6707.
The how-to is
[Run An Extracted Collector](../../public/deploy/kubernetes/run-an-extracted-collector.md).

## Performance evidence

No-Regression Evidence (#8894): the change is chart-only. It adds no Go runtime
code, query, queue, lease, claim, batch or graph-write path.

- **Baseline:** `helm template` of the default values and of the legacy
  single-worker values (hand-mounted registry volume), captured on the commit
  before the change (`origin/main` at the branch point).
- **After:** the same two renders on the final commit are byte-identical to the
  baseline (`cmp` exit 0 for both). With `activations` empty or
  `componentExtensionCollector.enabled: false`, nothing new renders and the
  coordinator pod is unchanged.
- **Backend and tools:** Helm v4.3.1 and kubeconform v0.8.0 (41 resources, 0
  invalid, 6 skipped for lack of a schema). The runtime proof used a private
  PostgreSQL 18.6 cluster and binaries built from this branch.
- **Input shape:** two activations (a PagerDuty-style one with a grant, a
  Jira-style one without) render two worker Deployments, two metrics Services,
  two PodDisruptionBudgets, one ConfigMap, and four init containers on the
  coordinator. Each worker pod gets two init containers that run once at pod
  start.
- **Runtime proof of the real path:** the init container script the chart
  renders ran against the real `eshu` CLI, with the reference PagerDuty
  collector as the seeded component. Run 1: grant, install and enable all
  exit 0. Run 2 in the same registry: `activation ... already enabled;
  continuing`, exit 0, and the registry is identical once timestamps are
  ignored. A wrong `componentVersion` stopped the script with exit 1 and a
  message naming the expected pair. Then the real workflow coordinator and the
  real component-extension worker started on that registry. Terminal state: one
  `collector_instances` row (`pagerduty-ref`, enabled, claims enabled), one
  `workflow_work_items` row with status `completed` and `attempt_count` 1, and 6
  `fact_records` rows, one per declared fact kind, under the scope ID set in
  `host.scope.id`. The worker's `/readyz` returned 200. With the component ID
  revoked, the worker refused to start with `no trusted claim-capable component
  activation found for instance "pagerduty-ref"`.
- **Not exercised:** the seed container. No collector image ships `collector
  seed` yet, so the proof copied the binary and manifest into the runtime
  directory by hand, which is what the seed command is specified to write. A
  Kubernetes cluster was not used.
- **Why safe:** init containers run once per pod start and touch only a
  pod-local `emptyDir`. Workers keep the existing claim, lease and retry path
  unchanged. Each worker has its own registry, so there is no shared mutable
  volume.

Verified by `go test ./internal/runtime -run
'TestHelmComponentActivation|TestComponentActivationExample' -count=1`. The tests
cover render shape, init-container order, credential isolation, coordinator
wiring, per-activation lease timing and parity. They also run the rendered
reconcile script under `sh` with a stub `eshu` (first run, restart, other enable
failures, version mismatch among several components, missing seed files). Every
validation case renders twice, once as installed and once with
`--skip-schema-validation`, so the template guards are tested without the schema
hiding them. A separate test checks that every example grant kind in the docs,
values comments and fixtures is a core fact kind.

## Observability evidence

No-Observability-Change (#8894): the change adds no metric, span, log key or
status field. Each activation worker serves the same `/metrics`, `/healthz`
and `/readyz` as the single worker. Its metrics Service carries an
`eshu.dev/component-instance` label, and the init containers print the grant,
install and enable results to the pod log. An operator at 3 AM reads
`kubectl logs <pod> -c reconcile-<instanceId>` for the registry steps, then the
existing workflow coordinator metrics, `/admin/status` and `workflow_work_items`
rows for claim progress.
