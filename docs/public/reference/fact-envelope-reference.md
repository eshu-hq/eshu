# Fact Envelope Reference

This page defines the fact contract that collectors, storage, projectors, and
reducers share. Use it when adding a collector family, changing fact identity,
or deciding whether a source observation is allowed to become graph truth.

For schema-version compatibility rules, use
[Fact Schema Versioning](fact-schema-versioning.md). For what the reducer
promises about delivery, ordering, generation supersession, unknown fact
kinds, and dead-letter visibility, use
[Reducer Guarantees](reducer-guarantees.md). For optional component
trust policy, use [Plugin Trust Model](plugin-trust-model.md) and
[Component Package Manager](component-package-manager.md).

## Contract

Collectors observe source truth and emit versioned facts. They do not write the
canonical graph, run durable-store DDL, or synthesize truth across sources.
Projection and reducer code own those later steps.

The Go source of truth is `go/internal/facts`. The durable envelope fields are:

| Field | Meaning |
| --- | --- |
| `FactID` | Durable row identity for one emitted fact. |
| `ScopeID` | Source-local scope, such as a repository, state snapshot, account/region/service, registry target, or documentation source. |
| `GenerationID` | Source observation generation. Consumers use it to separate current facts from stale rows. |
| `FactKind` | Domain kind, such as `terraform_state_resource` or `oci_registry.image_manifest`. |
| `StableFactKey` | Idempotency key inside a scope/generation. Re-emitting the same source fact must converge to the same stable key. |
| `SchemaVersion` | Semantic version for the fact payload contract. |
| `CollectorKind` | Collector family that emitted the fact, such as `git`, `terraform_state`, `aws`, `secrets_iam_posture`, `oci_registry`, `package_registry`, `ci_cd_run`, or `documentation`. |
| `FencingToken` | Claim or ownership token used by hosted collector paths to reject stale writers. |
| `SourceConfidence` | How Eshu learned the fact: `observed`, `reported`, `inferred`, `derived`, or compatibility-only `unknown`. |
| `ObservedAt` | Source observation time. |
| `Payload` | Fact-family payload. Sensitive source values must be redacted before the fact is emitted. |
| `IsTombstone` | Marks a deletion or retraction fact. |
| `SourceRef` | Source-local record reference with source system, scope, generation, fact key, URI, and source record ID. |

Facts must be idempotent under at-least-once delivery. A retry, duplicate
delivery, or collector restart must not create conflicting truth for the same
source observation.

## Source Confidence

`source_confidence` is required for new fact output.

| Value | Use |
| --- | --- |
| `observed` | Read directly from a source artifact, repository, document, state object, or fixture. |
| `reported` | Returned by an external system or provider API. |
| `inferred` | Concluded by correlating other evidence. |
| `derived` | Materialized from existing Eshu facts. |
| `unknown` | Legacy or system fallback only. New collectors and components must not emit it intentionally. |

Documentation claim candidates can be `observed` and still non-authoritative.
They prove a document says something; they do not prove the claim is
operationally true.

## Core Fact Families

The facts package owns the accepted core kinds and schema-version helpers. The
current families are:

| Family | Collector kind | Current fact kinds |
| --- | --- | --- |
| Documentation | `documentation` for hosted documentation and derived documentation truth, `git` for repo-hosted documentation source facts | `documentation_source`, `documentation_document`, `documentation_section`, `documentation_link`, `documentation_entity_mention`, `documentation_claim_candidate`, `documentation_finding`, `documentation_evidence_packet` |
| Terraform state | `terraform_state` for collected state, `git` for safe repo-local candidates | `terraform_state_candidate`, `terraform_state_snapshot`, `terraform_state_resource`, `terraform_state_output`, `terraform_state_module`, `terraform_state_provider_binding`, `terraform_state_tag_observation`, `terraform_state_warning` |
| AWS cloud | `aws` | `aws_resource`, `aws_relationship`, `aws_tag_observation`, `aws_dns_record`, `aws_image_reference`, `aws_security_group_rule`, `aws_iam_permission`, `aws_resource_policy_permission`, `aws_warning` |
| Secrets/IAM posture | `secrets_iam_posture` | `aws_iam_principal`, `aws_iam_trust_policy`, `aws_iam_permission_policy`, `aws_iam_policy_attachment`, `aws_iam_permission_boundary`, `aws_iam_instance_profile`, `aws_iam_access_analyzer_finding`, `gcp_iam_principal`, `gcp_iam_trust_policy`, `gcp_iam_permission_policy`, `k8s_service_account`, `k8s_rbac_role`, `k8s_rbac_binding`, `k8s_workload_identity_use`, `k8s_gcp_workload_identity_binding`, `k8s_service_account_token_posture`, `eks_irsa_annotation`, `eks_pod_identity_association`, `vault_auth_mount`, `vault_auth_role`, `vault_acl_policy`, `vault_identity_entity`, `vault_identity_alias`, `vault_kv_metadata`, `vault_secret_engine_mount`, `secrets_iam_coverage_warning` |
| S3 bucket posture | `aws` | `s3_bucket_posture`, `s3_external_principal_grant` |
| RDS posture | `aws` | `rds_instance_posture` |
| EC2 posture | `aws` | `ec2_instance_posture` |
| OCI registry | `oci_registry` | `oci_registry.repository`, `oci_registry.image_tag_observation`, `oci_registry.image_manifest`, `oci_registry.image_index`, `oci_registry.image_descriptor`, `oci_registry.image_referrer`, `oci_registry.warning` |
| Package registry | `package_registry` | `package_registry.package`, `package_registry.package_version`, `package_registry.package_dependency`, `package_registry.package_artifact`, `package_registry.source_hint`, `package_registry.vulnerability_hint`, `package_registry.registry_event`, `package_registry.repository_hosting`, `package_registry.warning` |
| CI/CD runs | `ci_cd_run` | `ci.pipeline_definition`, `ci.workflow_image_evidence`, `ci.run`, `ci.job`, `ci.step`, `ci.artifact`, `ci.trigger_edge`, `ci.environment_observation`, `ci.warning` |
| SBOM and attestations | collector-specific SBOM or attestation source | `sbom.document`, `sbom.component`, `sbom.dependency_relationship`, `sbom.external_reference`, `attestation.statement`, `attestation.slsa_provenance`, `attestation.signature_verification`, `sbom.warning` |
| Vulnerability intelligence | collector-specific vulnerability source | `vulnerability.source_snapshot`, `vulnerability.cve`, `vulnerability.affected_product`, `vulnerability.affected_package`, `vulnerability.os_package`, `vulnerability.epss_score`, `vulnerability.known_exploited`, `vulnerability.reference`, `vulnerability.warning`, `vulnerability.go_module_evidence`, `vulnerability.go_call_reachability` |
| Provider security alerts | `security_alert` | `security_alert.repository_alert` |
| Incident context | `pagerduty` for PagerDuty source collection | `incident.record`, `incident.lifecycle_event`, `change.record` |
| Incident routing | source collector that observed the routing evidence, including `terraform_state` and optional live `pagerduty` config validation | `incident_routing.applied_pagerduty_resource`, `incident_routing.applied_alert_route`, `incident_routing.observed_pagerduty_service`, `incident_routing.observed_pagerduty_integration`, `incident_routing.coverage_warning` |
| Jira work items | `jira` | `work_item.record`, `work_item.transition`, `work_item.external_link`, `work_item.project_metadata`, `work_item.issue_type_metadata`, `work_item.status_metadata`, `work_item.workflow_metadata`, `work_item.field_metadata`, `work_item.metadata_warning` |
| Observability | source collector that observed the evidence, including `git` for declared IaC/GitOps | `observability.source_instance`, `observability.declared_folder`, `observability.declared_dashboard`, `observability.declared_datasource`, `observability.declared_alert_rule`, `observability.declared_scrape_config`, `observability.declared_metric_rule`, `observability.declared_metric_route`, `observability.declared_log_route`, `observability.declared_trace_route`, `observability.applied_resource`, `observability.applied_sync_state`, `observability.observed_dashboard`, `observability.observed_target`, `observability.observed_rule`, `observability.observed_log_signal`, `observability.observed_trace_signal`, `observability.coverage_warning` |
| Semantic evidence | `semantic_extraction` | `semantic.documentation_observation`, `semantic.code_hint` |

Most current core families use schema version `1.0.0`.
`documentation_section` uses `1.1.0` because section payloads can carry
source-native content for updater diff generation. Check the fact-family helper
before emitting rows.

Terraform backend discovery remains exact-only: unresolved Git backend expressions emit `terraform_state_warning` with `collector_kind=git`, safe source metadata, line number, expression class, and opaque expression hash, without raw backend values, full locators, absolute paths, credentials, or state bytes.

Semantic evidence facts are optional provenance emitted by semantic extraction
jobs. `semantic.documentation_observation` preserves an LLM-assisted
documentation observation with source, chunk, provider profile, model, prompt
version, extraction mode, redaction version, policy state, confidence,
freshness, missing evidence, unsupported reason, and admission state.
Observation admission can be pre-reducer provenance
(`provenance_only`, `documentation_finding_candidate`) or reducer-owned
documentation admission (`exact`, `partial`, `ambiguous`, `stale`, `unsafe`,
`unsupported`). Only reducer-admitted exact documentation observations can
become exact documentation findings.
`semantic.code_hint` preserves a possible code relationship or entity hint with
the same replay and safety provenance plus subject/object code entity refs,
corroboration state, and
`promotion_policy=requires_deterministic_evidence`. These facts never prove
service, deployment, runtime, vulnerability, or infrastructure truth by
themselves. Reducers and query surfaces must require deterministic parser,
reducer, or provider evidence before presenting a hint as corroborated truth.
Payloads must not contain raw provider keys, prompt payloads, bearer tokens,
secret values, or private provider responses.

Repository-hosted documentation ingestion is part of Git collection. It emits
source-neutral documentation facts for Markdown, lightweight text, HTML,
notebook narrative, DOCX, CSV/TSV, XLSX, PPTX, ZIP/TAR documentation packets, and
default-off media transcript helper output
under the repository scope with repository `linked_entities` targets for
scoped readback. DOCX sections contain bounded heading, paragraph, and table text;
comments and tracked changes stay metadata-only.
Spreadsheet sections contain headers, row/column/sample counts, bounded samples,
truncation warnings, formula hashes, and redacted sensitive-looking cells. Hidden
XLSX sheets and legacy `.xls` cell bytes stay metadata-only. PPTX sections contain
visible slide title, body, and table text; hidden slides, notes, comments,
embedded objects, and external relationships stay metadata-only.
ZIP, tar, and gzip-compressed tar packets emit one outer `documentation_document` plus contained facts for allowed members.
Contained facts use `archive.ext!/member` identities and archive member metadata; `SourceRef`
still points at the outer archive. Unsafe paths, symlinks, special files,
nested archives, credential-looking members, unsupported formats, resource limits, and
compression-ratio hazards are warning metadata, not extracted content.
Deterministic `doctruth` extraction may add entity-mention and claim-candidate
facts from bounded sections, but those claims remain `document_evidence` only.
Media transcript helpers emit `documentation_document` and timestamped
`documentation_section` facts only from reviewed local transcript engine output
after media preflight; they do not enable repository media discovery, external
transcription, speaker identity, or operational truth.
Reducers and query surfaces decide later findings or drift evidence. Local
`eshu docs verify` checks local Markdown claims against caller-supplied truth
sources and emits findings or evidence packets instead of ingesting docs.

S3 bucket posture facts are metadata-only AWS collector evidence.
`s3_external_principal_grant` carries public, cross-account, AWS service, and
unsupported-principal metadata derived from a transient bucket-policy parse. It
never carries raw policy JSON, statement bodies, actions, resources,
conditions, ACL grants, object keys, or object data; reducers own any later
graph projection.

Secrets/IAM posture facts are source evidence emitted with
`collector_kind=secrets_iam_posture`. The AWS IAM source lane preserves
provider-native IAM identities, normalized role trust statements, normalized
identity permission statements, managed-policy attachments, permissions
boundaries, instance-profile role membership, OIDC provider identities, optional
Access Analyzer finding metadata, and explicit coverage warnings. The
Kubernetes source lane preserves redacted ServiceAccount, token posture, RBAC
role and binding, workload identity usage, IRSA annotation, EKS Pod Identity,
and coverage-warning metadata. The Vault metadata source lane preserves
redacted auth mount, auth role, ACL policy, identity entity, identity alias, KV
metadata, secret-engine mount, and coverage-warning metadata. These facts never
carry raw policy JSON, statement bodies, condition values, AWS credentials,
session tokens, OIDC client IDs, OIDC thumbprints, raw OIDC provider URLs, raw
ServiceAccount names, RBAC subject names, Secret names, projected tokens,
resourceVersion values, RBAC resourceNames, nonResourceURLs, Vault tokens,
AppRole secret IDs, raw Vault paths, key names, Vault policy bodies, Vault
policy names, custom metadata values, entity IDs, alias names, private URLs, or warning
messages. Provider URLs, Kubernetes identity values, and Vault metadata
identities are represented by fingerprints, bounded counts, and join keys where
needed. Reducers own effective-permission analysis, effective RBAC
interpretation, Vault policy interpretation, trust-chain joins, posture
classification, and graph promotion; source facts alone do not assert
privilege-escalation truth.

Conditioned IAM trust and permission-policy statement facts include normalized
condition key/operator names in their stable identity so duplicate or blank Sid
statements stay distinct without persisting condition values. Unconditioned
statement facts keep the same stable identity they had before condition
summaries were added.

Incident-routing facts preserve routing evidence before reducer-owned
comparison. Terraform-state applied evidence is emitted as
`incident_routing.applied_pagerduty_resource` for allowlisted PagerDuty
resources and `incident_routing.applied_alert_route` for allowlisted AWS
alert-routing resources that identify PagerDuty. Unsupported PagerDuty state
resources emit `incident_routing.coverage_warning` instead of persisting
unknown attributes. These facts carry Terraform state address, resource type,
module/provider address, scope, state generation, serial, lineage, locator
hash, and bounded resource identifiers. Secret-bearing endpoint values, SSM
parameter values, IAM policy documents, integration keys, private URLs, and
user emails are omitted or represented by redaction flags and fingerprints.
Optional live PagerDuty configuration validation emits
`incident_routing.observed_pagerduty_service` and
`incident_routing.observed_pagerduty_integration` for bounded service and
service-integration metadata. These observed facts carry provider-native IDs,
comparison state, update timestamps, and redaction flags; names, integration
keys, routing keys, private URLs, and token-like URL parameters are omitted,
sanitized, or fingerprinted.

`vulnerability.source_snapshot` may include cache lifecycle metadata such as
cache artifact version, snapshot digest, cache update time, expiration,
freshness, and mode. These fields describe source observation state only; they
do not prove vulnerability impact without reducer-owned package, image, or
deployment evidence.

`vulnerability.os_package` carries one installed OS package observation from
an Alpine apk, Debian dpkg, or RPM-family queryformat snapshot. The payload
preserves distro, distro version, package manager, name, epoch, upstream
version, distro release (including Alpine `-rN`, Debian `~debNuM` /
`+debNuM`, and RPM `el9_2`-style release suffixes), arch, source package,
source version, repository URL, repository class (`vendor`, `third_party`,
`unknown`), vendor advisory source (`alpine`, `debian`, `redhat`, `fedora`,
`amazonlinux`, `rocky`, `alma`, `centos`, or empty), PURL, BOMRef identity,
and the verbatim `installed_version_raw`.
The `fixed_version_source` field is left empty by the collector; reducers
populate it after joining a matching vendor advisory. Reducers MUST NOT
compare `installed_version_raw` against an upstream OSV/NVD advisory's
fixed version: vendor backports (for example
`openssl 3.0.11-1~deb12u2`) often patch the upstream fix without changing
the upstream version number, and using upstream evidence would produce
false positives against the vendor-published build.

`rds_instance_posture` carries one metadata-only security and operations
posture observation per RDS DB instance or Aurora DB cluster, derived from the
existing RDS describe pass: `publicly_accessible`, encryption and KMS key, IAM
database authentication, backup retention, multi-AZ, deletion protection,
Performance Insights configuration (enabled, retention, PI-KMS key),
parameter/option-group identity, a curated set of security-relevant parameters,
and the CA certificate identifier. It never carries database contents, master
usernames, connection secrets, snapshot payloads, log bodies, or Performance
Insights samples, and it emits no graph edges. Reducers own KMS,
parameter/option-group, and internet-exposure projection from this evidence.

`s3_bucket_posture` carries metadata-only S3 bucket posture, including bucket
identity, block-public-access booleans, derived policy summary booleans, and
server-access-log target bucket identity. It never carries raw bucket policy
JSON, ACL grant bodies, object keys, or object contents. Reducers own LOGS_TO
edge projection and the conservative exposed / not_exposed / unknown internet
exposure node-property projection from this evidence.

`aws_resource_policy_permission` is the resource-side analog of
`aws_iam_permission`: one derived, metadata-only statement from a resource-based
policy (an S3 bucket policy or KMS key policy) attached to the resource it
controls. The payload carries the attached `resource_arn` / `resource_type`,
`policy_source = "resource"`, the statement `effect`, the normalized
`actions` / `not_actions` / `resources` / `not_resources` patterns, a
condition key/operator NAME summary (`condition_keys`, `condition_operators`,
`condition_operator_count`, `has_conditions`), the
`is_wildcard_action` / `is_wildcard_resource` flags, and the derived grantee
principal facts (`principal_account_ids`, `principal_arns`, `principal_types`,
`is_public`, `is_cross_account`). It NEVER carries the raw policy JSON body, the
statement Sid or body, or condition VALUES. A resource with no attached policy
emits no fact. The S3 scanner derives it from the same transient
`GetBucketPolicy` parse that feeds `s3_bucket_posture`; the KMS scanner reads the
key policy with `GetKeyPolicy` (one bounded control-plane read per key policy
name). It emits no graph edge from the collector; the reducer consumes it as the
resource-policy source for conservative CAN_PERFORM projection under issue
#1134.

Conditioned `aws_iam_permission` and `aws_resource_policy_permission` facts
include the normalized condition key/operator summary in their stable identity
so statements with the same action/resource patterns and empty or duplicate Sid
values remain separate facts. Unconditioned statement facts keep their existing
stable identity.

`ec2_instance_posture` carries one metadata-only security and operations posture
observation per EC2 instance, derived from the existing DescribeInstances pass:
IMDS settings (`imds_v2_required`, `imds_http_endpoint`, `imds_http_put_hop_limit`),
user-data PRESENCE (`user_data_present`, a boolean only), detailed monitoring,
EBS optimization, public-IP association, the attached instance-profile ARN,
per-volume block-device metadata, and tenancy / Nitro-enclave state. It NEVER
carries the user-data content (which can embed secrets), instance console output,
environment variables, or any other instance payload, and it emits no graph
edges and no `aws_resource` inventory fact for the instance. Reducers own the
USES_PROFILE join to the IAM instance profile (#1146), the block-device KMS
posture projection (#1304), and the derived internet-exposed flag (#1135).
Per-volume encryption is not reported by DescribeInstances, so each block
device's `encrypted` stays unset (`null`) here. The EC2 scanner separately emits
metadata-only `aws_ec2_volume` resource facts and volume-to-KMS relationship
facts from one boundary-scoped DescribeVolumes pass; reducers resolve
block-device/KMS posture by joining posture block-device volume ids to that
volume evidence without per-instance API fan-out at scan time.

`security_alert.repository_alert` preserves repository-scoped provider alert
state, Dependabot alert ID/number, dependency ecosystem/name, manifest path,
dependency scope, relationship, GHSA/CVE IDs, vulnerable range, patched version,
severity, CVSS, EPSS, CWE summary, timestamps, and sanitized source URL. It is
reported provider evidence only. Reducers compare it with owned dependency and
impact facts through `security_alert_reconciliation`; they do not turn provider
state into `supply_chain_impact` truth.

`incident.record`, `incident.lifecycle_event`, and `change.record` preserve
provider-reported incident state, incident timeline entries, and related
change-event evidence. PagerDuty emits these as reported source evidence.
The incident-context read model may present provider state, timeline entries,
declared/applied/live PagerDuty routing slots, fallback change candidates, and
explicit missing path slots directly from these facts and the incident-routing
source lanes. Reducers and enrichment collectors must prove runtime artifact,
image, build/deploy, commit, pull-request, and work-item evidence before those
path slots become non-missing. Runtime and image slots require explicit
service-catalog operational-link evidence to the PagerDuty service URL plus
reducer-owned catalog, container-image, or Kubernetes correlation facts; similar
service names are not enough. Build/deploy and commit slots require
reducer-owned CI/CD run correlations tied to the selected image digest or
reference, and tag-only matches stay derived until immutable artifact evidence
exists. Pull-request slots require provider merged-PR evidence tied to the
selected commit. Jira remote links to that provider-verified PR, direct
PagerDuty incident links, or issue-key evidence can enrich work-item slots, but
Jira-only PR URLs do not verify PR identity. Missing Jira links are valid
incident evidence state and must not block incident collection.

`observability.applied_resource` and `observability.applied_sync_state` preserve
metadata-only Argo CD and Kubernetes applied-state evidence. They identify
source class, source kind, app, namespace, cluster, cluster-server fingerprint,
resource identity, resource class, generation, UID fingerprint, sync and health
state, operation phase, freshness, and outcome. They do not contain raw status
messages, labels, managed fields, dashboard payloads, query bodies, Secret data,
raw Kubernetes UIDs, or raw cluster URLs. Reducers own any later comparison
between declared, applied, and observed observability state.

`work_item.record`, `work_item.transition`, and `work_item.external_link`
preserve Jira work-item state, changelog IDs, and remote-link IDs as provider
evidence. They do not imply incident ownership, deployment cause, code change,
or pull-request truth unless a reducer or query later proves that path through
separate source evidence. A `work_item.external_link` to a GitHub PR URL is
source evidence only until GitHub/provider PR evidence verifies the commit-to-PR
hop. The Jira source boundary, identity keys, freshness semantics, and fixture
matrix are defined in [Jira Evidence Contract](jira-evidence.md). Jira payloads
carry `redaction_policy_version=jira_work_item_v1`; private summaries, user
identifiers, raw Jira URLs, remote-link URLs, remote-link titles, and
remote-link summaries are represented by presence booleans or URL fingerprints,
not raw values. A confidently typed GitHub PR or GitLab MR
`work_item.external_link` also carries `linked_repository_id`, the canonical
repository id resolved from the link URL before redaction; it is the same
identifier Eshu stores for every repository and carries no raw URL or secret,
and un-canonicalizable or ambiguous links omit it.
`work_item.record`, `work_item.transition`, `work_item.external_link`, and the
Jira metadata fact kinds preserve Jira work-item state, changelog IDs,
remote-link IDs, project/status/workflow context, custom-field schema classes,
and metadata warnings as provider evidence. They do not imply incident
ownership, deployment cause, code change, or pull-request truth unless a reducer
or query later proves that path through separate source evidence. A
`work_item.external_link` to a GitHub PR URL is source evidence only until
GitHub/provider PR evidence verifies the commit-to-PR hop. The Jira source
boundary, identity keys, freshness semantics, and fixture matrix are defined in
[Jira Evidence Contract](jira-evidence.md). Jira payloads carry
`redaction_policy_version=jira_work_item_v1`; private summaries, user
identifiers, raw Jira URLs, metadata names/descriptions, custom-field IDs,
remote-link URLs, remote-link titles, and remote-link summaries are represented
by presence booleans, bounded categories, or fingerprints, not raw values.

Declared PagerDuty module and tfvars evidence from Terraform source is emitted
through ordinary `content_entity` facts with `entity_type=PagerDutyDeclaration`
and `source_class=declared` in entity metadata. These facts preserve repo path,
environment/workspace, module source fingerprint, module name, bounded input
values, redaction state, and duplicate/malformed/unsupported outcomes. They do
not require live PagerDuty credentials, do not run Terraform, and do not replace
the observed `incident.*` or `change.*` facts emitted by the PagerDuty
collector. In incident-context reads, they fill the intended-routing slot only;
applied and live routing still require Terraform-state or PagerDuty API source
evidence.

Declared Grafana, Prometheus/Mimir, Loki, and Tempo observability evidence from
repository source is emitted by the Git collector as
`observability.source_instance`, `observability.declared_folder`,
`observability.declared_dashboard`, `observability.declared_datasource`,
`observability.declared_alert_rule`, `observability.declared_scrape_config`,
`observability.declared_metric_rule`, `observability.declared_metric_route`,
`observability.declared_log_route`, `observability.declared_trace_route`, and
`observability.coverage_warning` facts.
The parser supports Helm values, GrafanaFolder and GrafanaDashboard resources,
dashboard ConfigMaps, folder, datasource, and alert provisioning, Prometheus
Operator ServiceMonitor, PodMonitor, PrometheusRule, and ScrapeConfig
resources, kube-prometheus-stack and Mimir Helm values, Promtail client routes,
OTel metric, log, and trace pipelines, OTel Prometheus receiver scrape configs,
Loki and Tempo gateway values, Grafana Tempo datasource links, chart
ServiceMonitor settings, and Terraform
`grafana_folder`, `grafana_dashboard`, `grafana_data_source`, and
`grafana_rule_group` resources. These facts preserve repo path, source revision
when available, overlay or environment, resource identity, folder UID/title
fingerprint, dashboard UID/title fingerprint, datasource UID/type/name
fingerprint, alert UID/title fingerprint, datasource refs, selector keys and
selector fingerprints, rule group identity, metric/log route backend,
trace route backend, trace tag keys, datasource trace links, redaction state,
route destination fingerprints, tenant-scope state, and
unsupported/malformed/duplicate outcomes. They do not run Terraform, call
Grafana, Prometheus, Mimir, Loki, or Tempo, or store dashboard JSON, query
bodies, raw PromQL, LogQL, or TraceQL, scrape target addresses, remote-write
URLs, Loki or Tempo route URLs, tenant header values, tenant IDs, datasource
URLs, secret datasource fields, contact addresses, log lines, spans, traces,
raw trace IDs, request attributes, or high-cardinality trace tag values.
Live Tempo metadata facts use the same observability family with
`collector_kind=tempo`. The live collector emits
`observability.source_instance`, `observability.observed_trace_signal`, and
`observability.coverage_warning` facts from bounded tag-name, tag-value, and
freshness metadata only. It stores tag-value counts and fingerprints within
configured cardinality limits; it does not store spans, traces, raw trace IDs,
request attributes, TraceQL bodies, tenant IDs, raw tag values, private URLs, or
provider response bodies.

Live Grafana observed metadata is emitted by the Grafana collector package as
`observability.source_instance`, `observability.observed_dashboard`,
`observability.observed_rule`, and `observability.coverage_warning` facts. These
facts preserve provider UIDs, resource classes, folder UIDs, datasource type,
rule group identity, match-state hints, freshness state, redaction state,
manual-provider drift candidates, and bounded coverage warnings. They do not
store dashboard JSON, panel definitions, raw dashboard or datasource URLs, alert
query models, PromQL expressions, contact points, notification destinations,
credentials, screenshots, private URLs, or token values. Live Grafana facts are
reported provider evidence for no-IaC fallback, drift, and freshness validation;
they do not replace current declared source-controlled evidence.

Live Prometheus and Mimir observed metadata is emitted by the
Prometheus/Mimir collector package as `observability.source_instance`,
`observability.observed_target`, `observability.observed_rule`, and
`observability.coverage_warning` facts. These facts preserve provider UIDs,
scrape pool, health, label-key lists, rule group/name/type, freshness state,
redaction state, tenant presence, tenant fingerprints, manual-provider drift
candidates, and bounded coverage warnings. They do not store metric samples,
exemplars, profile data, raw PromQL, scrape target URLs, target label values,
discovered label values, annotations, tenant IDs, tenant secrets, credentials,
private URLs, or token values. Live metric facts are reported provider evidence
for no-IaC fallback, drift, and freshness validation; they do not replace
current declared source-controlled evidence.

Live Loki observed metadata is emitted by the Loki collector package as
`observability.source_instance`, `observability.observed_log_signal`,
`observability.observed_rule`, and `observability.coverage_warning` facts.
These facts preserve provider UIDs, label-key lists, bounded label-value
counts, label-value fingerprints, series fingerprints, rule namespace/group/name
metadata, freshness state, redaction state, tenant presence, tenant
fingerprints, manual-provider drift candidates, and bounded coverage warnings.
They do not store log lines, raw LogQL, private URLs, unbounded label values,
tenant IDs, tenant secrets, credentials, token values, or provider response
bodies. Live Loki facts are reported provider evidence for no-IaC fallback,
drift, and freshness validation; they do not replace current declared
source-controlled evidence.

## Promotion Rules

Facts are source evidence, not automatic graph truth.

- Documentation facts do not override code, deployment, runtime, or graph
  truth. API contract documentation sections describe observed operations,
  channels, schemas, and SDL fields, but reducers must corroborate ownership
  before any service or interface truth is promoted.
- Terraform-state facts must be redacted before emission. Terraform-state
  resource rows do not create cloud ownership by themselves.
- AWS facts are reported provider evidence. Reducers must corroborate workload,
  deployment, ownership, and environment truth before promotion.
- OCI registry tag observations are mutable evidence. Digest-addressed
  manifest, index, descriptor, and referrer facts are stronger identity anchors.
- Package registry source hints are not repository ownership or publication
  truth until reducer correlation admits them.
- CI success, environment names, SBOM metadata, vulnerability feeds, and
  provider alert state remain provenance until consumers correlate them with
  stronger runtime, package, image, or repository evidence.
- Incident and change facts remain provenance until consumers correlate them
  with stronger runtime, deployment, image, commit, pull-request, or work-item
  evidence.
- Observability facts remain source evidence until reducers compare declared
  IaC, applied state, and live provider evidence for the same service or
  runtime target.
- Semantic documentation observations and code hints remain provenance until
  reducers or query consumers explicitly admit them. Model output must not
  directly promote service, deployment, runtime, vulnerability, or
  infrastructure truth.
- GCP cloud collector facts are fixture-driven source evidence; only admitted
  facts become platform truth. GCP resource, relationship, tag, and
  image-reference evidence have reducer consumers; raw IAM policy snapshots,
  DNS records, and collection warnings remain provenance/audit evidence. Shared
  and provider-specific baselines live in the multi-cloud, GCP, and Azure cloud
  collector contracts.

ACL summaries and source-native documentation bodies are sensitive source
evidence. Do not emit them through logs or metrics. Evidence packet APIs must
fail closed unless a permission decision says the viewer can read the source.

## Change Checklist

When adding or changing a fact family:

1. Add or update the family metadata in
   `specs/fact-kind-registry.v1.yaml`, then regenerate the generated registry
   contract in `go/internal/facts`.
2. Set `CollectorKind`, `SourceConfidence`, `ScopeID`, `GenerationID`, and
   `StableFactKey` deliberately in the emitter.
3. Redact sensitive payload values before emission.
4. Prove idempotency for duplicate delivery, retries, and stale generations.
5. Add the reducer, projector, or query consumer contract before presenting the
   fact as active platform truth.
6. Update this page and [Fact Schema Versioning](fact-schema-versioning.md) if
   the compatibility contract changes.
7. If an optional component emits the fact family, update its manifest contract
   in [Component Package Manager](component-package-manager.md).

## Related

- [Fact Schema Versioning](fact-schema-versioning.md)
- [Reducer Guarantees](reducer-guarantees.md)
- [Collector Authoring](../guides/collector-authoring.md)
- [Collector And Reducer Readiness](collector-reducer-readiness.md)
- [Multi-Cloud Runtime Collector Contract](multi-cloud-collector-contract.md)
- [GCP Cloud Collector Contract](gcp-cloud-collector-contract.md)
- [Azure Cloud Collector Contract](azure-cloud-collector-contract.md)
- [Secrets And IAM Posture Collector Contract](secrets-iam-posture-collector-contract.md)
- [Jira Evidence Contract](jira-evidence.md)
- [Component Package Manager](component-package-manager.md)
- [Plugin Trust Model](plugin-trust-model.md)
