# Run An Extracted Collector

Use this page when a collector lives in its own repository and ships as its own
release image, and you want the Helm chart to run it through the
component-extension worker. The Eshu image stays the host. The collector's
binary and manifest arrive in the pod from a digest-pinned release image, so
you never override the image of a main container.

Each entry in `componentExtensionCollector.activations` is one running
collector instance. The chart renders one worker Deployment per entry, because
the worker runs exactly one activation and one collector kind.

## What You Need

- The collector release image, referenced by digest
  (`repository@sha256:<64 hex>`). A tag is rejected at render time.
- The component ID and the version in the manifest that image carries.
- The source system name and scope ID the collector reports as its claim
  identity. For a collector that replaces an in-tree one, use the same scope ID
  so existing facts and the new facts line up.
- A Kubernetes Secret for any credential. Credentials never go in values.
- The workflow coordinator in `active` mode with claims enabled, as for every
  claim-driven collector.

## The Seed Contract

The collector image must run `collector seed --dest <dir> --image-ref <ref>` and
write two files into that directory:

| File | Mode | Meaning |
| --- | --- | --- |
| `collector` | `0755` | The static collector binary the worker starts as a child process. |
| `manifest.yaml` | `0644` | The component manifest (`ComponentPackage`) for this release, with `--image-ref` written in as its pinned artifact image. |

The command must be safe to run again over an existing directory, replacing both
files. It needs no shell and no network. The chart passes the same digest-pinned
`seed.image` it validated as `--image-ref`, so a manifest never carries a
different image than the one that ran. If the image keeps the command
elsewhere, set `seed.command` (the chart appends `--dest <dir> --image-ref
<ref>`). Release images are `linux/amd64`, so the worker and coordinator nodes
that pull them must be amd64 too.

## Example

This sanitized example runs one PagerDuty-style collector. Names, hosts and
digests are placeholders.

```yaml
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true

componentExtensionCollector:
  enabled: true
  trustMode: allowlist
  allowIds: dev.example.collector.pagerduty
  allowPublishers: example
  extensionEgressPolicyJSON: >-
    {"mode":"restricted","extensions":[{"component_id":"dev.example.collector.pagerduty","decision":"allow"}]}
  pollInterval: 30s
  claimLeaseTTL: 5m
  heartbeatInterval: 30s
  activations:
    - instanceId: pagerduty-example
      componentId: dev.example.collector.pagerduty
      componentVersion: 0.1.0
      mode: scheduled
      seed:
        image: ghcr.io/example/collector-pagerduty@sha256:0000000000000000000000000000000000000000000000000000000000000000
      host:
        sourceSystem: pagerduty
        scope:
          id: pagerduty:account:example
          kind: pagerduty_account
      process:
        args: ["--sdk-stdio"]
        stdoutLimitBytes: 33554432   # 32 MiB; raise only if the collector emits large results
      config:
        api_token_env: PAGERDUTY_API_TOKEN
      grants:
        - kind: incident.record
          schemaVersions: ["1.0.0"]
          scope: pagerduty
          expiresIn: 720h
      extraEnv:
        - name: PAGERDUTY_API_TOKEN
          valueFrom:
            secretKeyRef:
              name: pagerduty-credentials
              key: api-token
```

The example grants one kind to stay short. Grant every core fact kind the
manifest declares, with the schema versions it emits:

- PagerDuty: `incident.record`, `incident.lifecycle_event`, `change.record`,
  `incident_routing.observed_pagerduty_service`,
  `incident_routing.observed_pagerduty_integration` and
  `incident_routing.coverage_warning`.
- Jira: `work_item.record`, `work_item.transition`, `work_item.external_link`,
  `work_item.project_metadata`, `work_item.issue_type_metadata`,
  `work_item.status_metadata`, `work_item.workflow_metadata`,
  `work_item.field_metadata` and `work_item.metadata_warning`.
- Confluence: `documentation_source`, `documentation_document`,
  `documentation_section` and `documentation_link`.

Add a second entry to run a second instance, for example one per Jira site.
Every entry needs its own `instanceId`.

## Activation Keys

| Key | Required | Meaning |
| --- | --- | --- |
| `instanceId` | yes | Lowercase DNS label, 30 characters or fewer, unique in the list. Names the Deployment (`<release>-cx-<instanceId>`). |
| `componentId` | yes | Component ID from the manifest. It must also appear in `allowIds`. |
| `componentVersion` | yes | Version from the manifest. The init container fails if the seeded manifest differs. |
| `mode` | no | Activation mode, default `scheduled`. Claims are always enabled. |
| `seed.image` | yes | Release image pinned by digest alone: `repository@sha256:<64 hex>`. A tag is rejected, even next to a digest, because the seed command's `--image-ref` would carry the tag. A registry port is fine (`registry.example:5000/org/name@sha256:...`). |
| `seed.pullPolicy`, `seed.command` | no | Pull policy (default `IfNotPresent`) and seed entrypoint (default `["/collector", "seed"]`). |
| `host.sourceSystem`, `host.scope.id`, `host.scope.kind` | yes | Claim identity the coordinator copies into planned work. An activation plans exactly one host scope, its `host.scope.id`, and a `vulnerability_intelligence` activation also plans the derived OSV scopes described in "Derived OSV Scopes". Other component configuration that names scopes is still never collected, so run one activation per host scope. |
| `process.args`, `process.env`, `process.stdoutLimitBytes`, `process.stderrLimitBytes` | no | How the worker starts the binary. The chart sets the command itself. The worker reads 4 MiB of collector output by default; raise `stdoutLimitBytes` for a collector that emits more (for example 33554432 for 32 MiB, 67108864 for 64 MiB). |
| `claimLeaseTTL`, `heartbeatInterval`, `pollInterval` | no | Per-worker claim timing. Each overrides the value of the same name under `componentExtensionCollector`. Durations use `h`, `m` and `s` (`pollInterval` also accepts `ms`). The heartbeat must be less than the lease. See "Set The Claim Lease". |
| `config` | no | Non-secret collector settings, passed to the collector as its config block. For a core collector kind with host-planned derived scopes, today `vulnerability_intelligence`, the coordinator also reads that kind's derive sections from this block (see "Derived OSV Scopes"); the block is still passed to the collector verbatim. |
| `grants` | no | Producer grants for core-owned fact kinds: `kind`, `schemaVersions`, `scope`, and `expiresIn` (default `720h`, a positive duration whose first number is not zero). Each `kind` must be an exact core fact kind such as `incident.record`; `eshu component grant` refuses anything else and the pod stays in `Init:Error`. The scope is the collector kind (for example `jira` or `documentation`), and a grant is needed for each core fact kind and schema version the manifest declares before install succeeds. |
| `replicas`, `resources`, `env`, `extraEnv`, `extraVolumes`, `extraVolumeMounts` | no | Per-worker overrides. `extraEnv` is where credentials go. |

The schema (`values.schema.json`) rejects unknown keys, so a misspelled key
fails the render instead of being ignored.

## What The Chart Renders

For each activation:

- A worker Deployment with two init containers and a pod-local registry
  (`emptyDir`). The first init container runs the collector image's seed command
  into `<componentHome>/runtime/<instanceId>`. The second runs the Eshu image and
  records the grants, installs the manifest, checks that the installed ID and
  version match `componentId` and `componentVersion`, then enables the instance
  with claims. Grants go first because a core-owned fact kind passes install
  only when its grant already exists.
- A metrics Service and a PodDisruptionBudget for that worker.

Shared across activations:

- One ConfigMap, `<release>-component-activations`, with one
  `<instanceId>.yaml` per activation holding the host block, the process
  command and the `config` block.
- The existing ServiceMonitor and NetworkPolicy, which select every worker by
  component label.

The workflow coordinator reads the same registry to plan work, so it gets the
same init containers (one pair per activation), a read-only registry, and the
same ConfigMap at the same path. The activation config handle hashes that path,
so the coordinator and each worker must see identical paths. A checksum
annotation restarts the coordinator and all workers together when the
ConfigMap changes.

A render with `componentExtensionCollector.enabled: false`, or with an empty
`activations` list, is unchanged. An empty list keeps the older single worker,
which still needs `instanceId` and hand-mounted `extraVolumes`.

## Derived OSV Scopes

A `vulnerability_intelligence` activation plans more than its host scope. The
coordinator reads `derive_from_owned_packages` and
`derive_from_installed_evidence` from the activation's `config` block and plans
one claim per derived OSV scope, the same way it does for an in-tree instance
(#9311):

- The coordinator reads the block from the file it already opens for `host`,
  and keeps it as an opaque `component_config` value for this kind only. The
  worker still hands the collector its own read of the block, unchanged.
- The derive sections follow the in-tree rules (`enabled`, `sources`,
  `ecosystems`, `planning_mode`, `target_limit`), with the same validation and
  error text, plus the explicit-list rule below. The coordinator checks the
  block when it loads its configuration, so an invalid value is a startup
  error, raised before any durable write. That holds for an activation read
  from the component registry and for an activation document passed through
  `ESHU_COLLECTOR_INSTANCES_JSON`, enabled or not. Unknown keys are ignored, as
  they are in-tree and in the collector.
- `target_limit` (default 100, at most 5000) and `planning_mode` (`rotating` or
  `single_pass`) are host-only: the coordinator applies them, and collector
  v0.1.0 ignores them. Rotation buckets on the coordinator's reconcile interval,
  because an activation sets no `scan_interval`. See
  [Derived Target Rotation](#derived-target-rotation) for how a window picks
  its targets.
- Derived scopes ride a second run beside the host-scope claim. The open-target
  guard admits each scope separately, so an open host-scope claim does not block
  them, and the host scope is never planned twice.
- `host.sourceSystem` and `host.scope.kind` must both be
  `vulnerability_intelligence` when derivation is enabled. Otherwise the
  coordinator refuses to start, and the error names both values.
- Egress policy applies first: a denied activation gets no derived scopes and
  no owned-package read.
- Every enabled derive section must list `sources` and `ecosystems`
  explicitly. Collector v0.1.0 resolves derived scopes against the union of
  `sources` and `ecosystems` across all of its enabled derive sections,
  including its own `derived_targets`, which the coordinator ignores. It
  defaults an empty union to `osv` and `npm`, and keeps only `osv` and the 16
  supported ecosystems. The coordinator defaults each section on its own, so
  a blank list could plan scopes the collector refuses. On an activation, a
  blank or absent list therefore fails coordinator startup with
  `<section>.<list>: an activation must list <list> explicitly`, naming the
  instance and no configured values. An in-tree instance keeps its blank-list
  defaults.
- Each reconcile pass applies the same checks again, as a backstop for a stale
  durable row. A refusal there logs one ERROR line, either value-free
  (`derive_section`, `derive_list`, `reason=derive_lists_not_explicit`) or with
  the `host_source_system` and `host_scope_kind` values. Like every reconcile
  error, it stops the coordinator process until the file is fixed.
- A derived scope the collector does not accept fails as `unknown_scope`,
  which is retryable: it is retried until the claim's attempt budget is spent,
  then marked terminal `attempt_budget_exhausted`. It is not `invalid_config`;
  the collector reports that only when it cannot start from its config block.

`eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total` counts
planned scopes by instance and ecosystem. A startup refusal shows as the
coordinator exiting with `workflow coordinator failed` and the error above. A
refusal caught by the reconcile backstop shows as
`eshu_dp_workflow_coordinator_reconcile_total{outcome="reconcile_error"}` with
the ERROR line above. The owned-package read shows in the store histograms
under `store="owned_package_targets"`.

### Derived Target Rotation

The owned-package read (shared by derived vulnerability and package-registry
targets) gives every distinct target a fixed hash slot (#9422):

- The slot is `sha256(key)` mod P. The key is `ecosystem|package|version` for
  vulnerability targets and `ecosystem|package` for package-registry targets,
  with the alias-folded ecosystem.
- P is the smallest power of two with `P * target_limit >= 2 * T`, where T is
  the number of distinct targets for the configured ecosystems. The mean slot
  then holds at most half the limit, so a slot fits in one read.
- Window `bucket` plans slot `bucket mod P`. The bucket is the
  interval-truncated clock that also names the plan key. Consecutive windows
  plan consecutive slots, and a change in T does not move a target to another
  slot.
- Every target is planned within P windows. With a 30-minute interval,
  `target_limit: 2000` and T up to 32,000, P is 32 (16 hours). Past 32,000, P
  is 64 (32 hours) and each window holds about half as many targets. At the
  default limit of 100, P is 1,024 for T near 30,000, so raise the limit with
  the image that ships this rule.
- `single_pass` always reads the first `target_limit` targets of the whole set
  (one slot). By design it logs `truncated=true` whenever T is above
  `target_limit + 1`, and reports the targets past the read limit as
  `derived_target_budget_exhausted`, because it never plans them.
- When the read limit cuts a slot short, the unread targets are reported as
  `derived_target_budget_exhausted` skip evidence.

Each read logs one INFO line, `workflow coordinator read owned package target
window`, even when the slot is empty (`slot_size` 0) or T is 0 (P is 1), with `total_targets` (T), `slot_count` (P), `slot`, `slot_size`,
`rows_read`, `truncated`, `selected`, `selected_ecosystems`, and
`selected_by_ecosystem`. The same values sit on the `owned_package_targets.read`
span and its `owned_package_targets.window` event. The read latency is
`eshu_dp_postgres_query_duration_seconds{store="owned_package_targets"}`,
whose buckets reach 300 s.

## Set The Claim Lease

The extension host stops the collector process when the claim's first lease
expires, and heartbeats do not extend that deadline. The worker's default lease
is 60 seconds, which cuts a long crawl short. Set `claimLeaseTTL` on every
activation (or once under `componentExtensionCollector`) to at least the
in-tree collector's value. The reference deployment runs a 5 minute lease with a
30 second heartbeat. The chart refuses a heartbeat that is not shorter than the
lease, counting the worker defaults (60 seconds and 20 seconds) when a value is
unset.

## Credentials

Values hold no secrets. Put the credential in a Secret, reference it through the
activation's `extraEnv`, and name the environment variable in `config` (the
example uses `api_token_env`). The collector process inherits the worker's
environment, so it reads the variable by name.

The chart refuses a config key that looks like a credential (names containing
`password`, `secret`, `token`, `api_key`, and similar, unless the key ends in
`env`, `_env_name`, or `ref`) and refuses values that start like known token
formats, in `config` and in the value of a `process.env` entry (`KEY=VALUE`).
These refusals are best-effort heuristics. They catch a pasted token or an
obvious name, not every secret, so do not rely on them: credentials belong only
in an activation's own `extraEnv` as a `secretKeyRef`. A top-level `extraEnv` is
refused in this mode, so one collector never inherits another's secret.

## Upgrade, Roll Back, Remove

- **Upgrade:** set the new `seed.image` digest and `componentVersion`, then
  deploy. The pod restarts, the registry starts empty, and the seed command
  replaces the binary before the worker starts. Activations that share a
  `componentId` must share one `componentVersion` and one `seed.image`.
- **Roll back:** restore the previous digest and version. Nothing from the newer
  release survives in the pod.
- **Remove an activation:** delete its entry. The chart stops rendering its
  Deployment, Service and PodDisruptionBudget, and the coordinator pod restarts
  without it.
- **Revoke a component:** add its ID to `revokeIds` (or its publisher to
  `revokePublishers`). Install then refuses it, so the `reconcile-<instanceId>`
  init container fails and the pod stays in `Init:Error`. That is the expected
  rollback signal: the system fails closed and no worker runs for the revoked
  component. Remove the activation entry to clear the failed pod.
- **Write the trust lists:** `allowIds`, `allowPublishers`, `revokeIds` and
  `revokePublishers` are comma-separated. The chart ignores whitespace around
  the commas and skips blank entries, so `dev.a, dev.b` and `dev.a,dev.b` mean
  the same thing.

## Limits To Know

- The registry lives in the pod, so grants are issued again on every pod start
  and `expiresIn` counts from that start. Keep `expiresIn` longer than the
  longest time a pod is expected to run; the default is `720h`. A grant that
  expires inside a long-lived pod fails the next emission closed.
- The coordinator runs every seed container, because it needs each manifest, so
  it copies each binary as well. Pull access for the collector registry must
  cover the coordinator's nodes too. A manifest-only seed mode could remove the
  copy later.
- One NetworkPolicy covers every worker, so all activations share the same
  egress classes.
- Each worker has a PodDisruptionBudget with `minAvailable: 1`. With the default
  one replica, that budget blocks a voluntary node drain until you scale the
  worker up, delete the activation, or accept the disruption. Set `replicas: 2`
  on a worker that must survive routine node maintenance.
- Strict trust mode is not charted. Use `trustMode: allowlist`.
- Each manifest declares exactly one collector kind.

## Check It

Render before you deploy:

```bash
helm template eshu deploy/helm/eshu -f values.yaml \
  | rg -n 'name: (seed|reconcile)-|kind: Deployment|eshu-cx-'
```

After the rollout, read the init container logs for one worker:

```bash
kubectl logs deploy/eshu-cx-pagerduty-example -c reconcile-pagerduty-example
```

A healthy run prints the grant, install and enable results. If the seeded
manifest does not match `componentId` and `componentVersion`, the init container
exits non-zero and says which pair it expected.

See [Helm Collector And Webhook Values](helm-collector-and-webhook-values.md)
for the rest of the collector values and
[Component Package Manager](../../reference/component-package-manager.md) for the
CLI the init container uses.
