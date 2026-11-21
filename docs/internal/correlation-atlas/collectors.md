# Collectors

Part of the [Correlation Atlas](README.md).

Most collectors live in their own repository and run through the extension
host (`go/cmd/collector-component-extension`). This repository keeps their
record kinds, planning, storage, and correlations. The rows below were read
from each collector repository at the commit shown, and from this repository at
`7c2948825d`. Nothing was run. A row's scope shape was checked against the
collector's code where the "Checked" column says so; the rest come from one
read-only scan and were not re-read.

## Who makes a snapshot, and when it is skipped

These rules apply to every collector that runs through the extension host.

- The planner in this repository mints one snapshot id per scheduled interval
  for each configured collector (`coordinator/planner/component/extension/planner.go`,
  `coordinator/scheduled_work.go`). The collector echoes that id.
- The collector may return a freshness value with its records. When it returns
  none, every run that has records stores a new snapshot.
- The commit path decides whether to store. `Matches` in
  `storage/postgres/backfill/generation_guard.go` treats a snapshot as a
  duplicate when its freshness value is non-empty and equal to the newest
  pending or active snapshot's, and the source commit is equal. It does not
  compare records. A snapshot from a source other than git has no source
  commit, so the freshness value alone decides.
- A duplicate is still stored in a few cases (`DecideUnchangedGeneration` and
  `GenerationNeedsDuplicateCheck` in the same file): a default-branch change, a
  write-once snapshot (the NVD and GHSA windows), a matching snapshot whose
  stream dropped a file, a pending match that no worker can activate, and a
  reconcile that was requested as stale or as a reindex. A match with any other
  reconcile reason re-projects the active snapshot without storing a new one.

### Unchanged snapshots

Outside those cases, a collector that returns the same freshness value on
every run has every later snapshot skipped as unchanged.

| Collector | Freshness value | Effect read from code |
|---|---|---|
| Git | Content identity, with the source commit | A changed repository stores a snapshot |
| PagerDuty, Confluence, security alerts, package registry, image registry, vulnerability sources | A digest of the records | A changed source stores a snapshot. An unchanged one is skipped |
| Terraform state | The state's lineage and serial | Skipped while the serial is unchanged |
| Jira | A digest that includes the time window | Effectively never skipped |
| CI/CD runs, SBOM and attestation | None | Never skipped |
| AWS, GCP | None on a complete result. The collector itself can report "unchanged" from a previous digest | Not determined for a deployment |
| Kubernetes, Vault | The constant `complete`, or `partial` when coverage was partial | A complete snapshot after a complete one is skipped. A change between complete and partial stores |
| Grafana, Loki, Prometheus/Mimir, Tempo | A constant, for example `grafana_observed_metadata` | Every snapshot after the first is skipped |
| Scanner worker image, OS package, and SBOM analyzers | The constant `snapshot`. The Go reachability analyzer sends a digest of its input instead | Same, for a target whose scope already has an active snapshot |

Observed 2026-11-21, read-only, in the QA environment
([queries and counts](../evidence/correlation-atlas-observations-2026-11-21.md)):

- Grafana, Loki, Prometheus/Mimir, and Tempo each had exactly one stored
  snapshot, all from 2026-10-26. Kubernetes had one, from 2026-10-27. Vault's
  newest was from 2026-10-29.
- The Grafana, Loki, Kubernetes, and Vault collectors each ran four or five
  times in the two hours before the reading and returned records. Every run
  logged a skipped refresh with the reason `unchanged`, matched to that old
  snapshot. The Kubernetes runs returned 6,074 records each.
- Whether the observed content had changed in that time was not checked. The
  rule skips either way.

## Code and repository

| Collector | Lives in | One scope is | Snapshot | Record kinds | Checked |
|---|---|---|---|---|---|
| Git | This repository: `collector/repo/git`, `go/cmd/collector-git`, and the ingester | One repository's default branch (`git-repository-scope:<repository id>`), or one pinned ref | Full, or a cumulative delta against the newest full | `repository`, `file`, `content`, `content_entity`, `shared_followup`, and hook kinds: `codeowners.ownership`, `submodule.pin`, `runtime_declaration.version`, `service_catalog.*`, `documentation_*`, `observability.*` declarations, `ci.workflow_image_evidence`, `terraform_state_candidate`, code data-flow kinds | Yes |
| Confluence | `eshu-collector-confluence` at `f95028c` | One space or one root page tree | One per run | `documentation_source`, `documentation_document`, `documentation_section`, `documentation_link` | No |

## Supply chain

| Collector | Lives in | One scope is | Snapshot | Record kinds | Checked |
|---|---|---|---|---|---|
| Vulnerability sources | `eshu-collector-vulnerability-intelligence` at `3c60e83`; scopes and planning in `vulnsource` and `coordinator/vulnerability` | One source target, window, baseline chunk, or slot under `vuln-intel://`. OSV scopes carry package queries drawn from the organization's own dependencies, installed OS packages, and SBOM components. EPSS scopes (`vuln-intel://first_epss/_findings/<n>`) carry up to 100 vulnerability ids taken from the winners table. KEV is one scope and reads the whole catalog each run | Windows for NVD and GHSA, chunks for the rest | `vulnerability.cve`, `vulnerability.affected_package`, `vulnerability.affected_product`, `vulnerability.epss_score`, `vulnerability.known_exploited`, `vulnerability.reference`, `vulnerability.source_snapshot`, `vulnerability.changed_name`, and others | Partly |
| Scanner worker | `eshu-scanner-worker` at `90def6f`; manifest in `collector/scannercomponent` | An operator-configured target, a derived image (`image://derived/<digest>`), or a repository (`go-reachability://<repository id>`) | One per claimed run | `scanner_worker.analysis`, `vulnerability.os_package`, `vulnerability.warning`, `vulnerability.go_module_evidence`, `vulnerability.go_call_reachability`, `sbom.component`, `sbom.document` | No |
| Security alerts | `eshu-collector-security-alerts` at `7f17c1f` | One repository (`security-alert:github:<owner>/<repository>`). An organization target fans out per repository, capped at 100 | One per run. Open alerts only | `security_alert.repository_alert`. EPSS and CVSS values are passed through from the provider | Yes |
| Package registry | `eshu-collector-package-registry` at `43a6f54` | One package (`<ecosystem>://<registry>/<name>`) | One per run | `package_registry.*` | Yes |
| Image registry | `eshu-collector-oci-registry` at `231df4b` | One registry repository (`oci-registry://<registry>/<repository>`) | One per run. A tag observation and its manifest share a snapshot | `oci_registry.*` | Yes |
| SBOM and attestation | `eshu-collector-sbom-attestation` at `481c623` | One configured document | One per run | `sbom.*`, `attestation.*` | No |

An EPSS or KEV record names no package. `vulnerability.epss_score` has no
package field, and `vulnerability.known_exploited` carries vendor and product as
free text (`epss_envelope.go`, `kev_envelope.go` in the collector repository).
See [Supply chain](supply-chain.md#score-refresh) for what follows from that.

## Cloud and state

| Collector | Lives in | One scope is | Snapshot | Record kinds | Checked |
|---|---|---|---|---|---|
| AWS | `eshu-collector-aws-cloud` at `3b9f26b`; planning in `collector/awscloud` | Planned here as one account, region, and service kind | Not determined. No tombstones are emitted | `aws_resource`, `aws_relationship`, and posture kinds. `aws_ssm_host_application` and `aws_ssm_host_platform` have builders and no live caller | No |
| GCP | `eshu-collector-gcp-cloud` at `d430ed1`. `collector/gcpcloud` here duplicates part of it | One configured scope | Full | `gcp_cloud_resource`, `gcp_cloud_relationship`, and IAM kinds | No |
| Azure | `eshu-collector-azure-cloud` at `2e9752d` | One tenant, scope kind, provider scope, family, bucket, and lane (`azure:<tenant>:...`) | Not determined | `azure_cloud_resource`, `azure_cloud_relationship`, and six more | Partly |
| Terraform state | `eshu-collector-terraform-state` at `959816f`; scope built in `scope/tfstate.go` | One state file (`state_snapshot:<backend>:<hash of the locator>`) | One per read, on bootstrap and on a schedule, skipped while the serial is unchanged. A config-only git change triggers no read | `terraform_state_*` | Yes |
| Vault | `eshu-collector-vault-live` at `187f42b` | One Vault cluster and namespace | One per run, skipped when unchanged in coverage | Secrets and identity kinds read by `secrets_iam_trust_chain` | No |
| Kubernetes | `eshu-collector-kubernetes-live` at `90ccea4`. `collector/kuberneteslive` here has no program that runs it | One cluster | Full, skipped when unchanged in coverage | `kubernetes_live.namespace`, `kubernetes_live.pod_template`, `kubernetes_live.relationship`, `kubernetes_live.warning`, and others | No |

## Runtime and delivery

| Collector | Lives in | One scope is | Snapshot | Record kinds | Checked |
|---|---|---|---|---|---|
| CI/CD runs | `go/cmd/collector-cicd-run` here, and `eshu-collector-cicd-run` at `28b40ae`. Which one a deployment runs was not determined | One configured target repository | A window of the most recent runs | `ci.run`, `ci.job`, `ci.step`, `ci.artifact`, `ci.pipeline_definition`, `ci.trigger_edge`, `ci.environment_observation`, `ci.deployment_event`, `ci.warning` | No |
| Service catalog | Inside the git collector | The repository it was read from | Follows the repository snapshot | `service_catalog.*` | No |
| PagerDuty | `eshu-collector-pagerduty` at `5bb5254` | One account | A lookback window per run | `incident.*`, `change.record`, incident routing kinds | No |
| Jira | `eshu-collector-jira` at `0eafdcf` | One site | A lookback window per run | `work_item.*` | No |
| Grafana, Loki, Prometheus/Mimir, Tempo | `eshu-collector-grafana` at `a127c47`, `eshu-collector-loki` at `1450a45`, `eshu-collector-prometheus-mimir` at `c669732`, `eshu-collector-tempo` at `8521a4c` | One configured target | One per run | `observability.*` observed kinds | Yes, for the freshness value |

## Not found

- A collector record that queues `incident_repository_correlation`,
  `documentation_materialization`, or `cloud_asset_resolution`. No collector
  repository sets the payload field that routes a record to a correlation.
- Which process runs AWS, GCP, and CI/CD collection in a given deployment. The
  code exists in both places and the choice is deployment configuration.
- How deleted resources are represented by the cloud collectors.
