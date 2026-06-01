# Collector And Reducer Readiness

Use this page to decide whether a source family is ready for deployed
collection, reducer materialization, and API or MCP reads. Eshu is facts-first:
collectors and webhooks observe source systems and commit facts; the resolution
engine owns graph truth, read-model truth, retries, dead letters, and completion
state.

A collector is not production-ready just because its binary exists. The
deployment path must also prove bounded collection, durable facts, reducer
drain, and operator-visible status.

## Current Contract

The implemented deployed collector lanes are:

- direct Confluence collection
- direct OCI registry collection, with claim-aware runtime support outside the
  public chart path
- claim-driven Terraform-state collection
- claim-driven AWS cloud collection
- claim-driven package-registry collection
- claim-driven provider security-alert collection
- claim-driven Jira work-item evidence collection
- remote-E2E-gated vulnerability intelligence collection
- claim-driven scanner-worker warning facts, bounded SBOM generation, and
  configured OS package rootfs extraction for isolated analyzer execution
- claim-driven PagerDuty incident-context source collection through the hosted
  binary, with public chart support pending
- webhook listener intake for Git provider events plus AWS, PagerDuty, and Jira
  freshness triggers

The scanner-worker lane is deployed as an isolated analyzer boundary. It
defines claim input, target scope, resource limits, source fact output,
retry/dead-letter payloads, telemetry names, Compose wiring, and an opt-in Helm
Deployment. The built-in warning analyzer proves source-fact emission without
claiming a target is clean. The bounded `sbom_generation` analyzer emits
CycloneDX-compatible SBOM source facts from configured repository manifest
targets and otherwise emits an explicit warning. The `os_package_extraction` analyzer
parses configured, already-extracted Alpine or Debian rootfs metadata into
`vulnerability.os_package` and `vulnerability.warning` source facts without
matching advisories or publishing findings.

Do not add chart values for design-only collectors. A Helm knob is an operator
promise; only chart collectors whose binary, fact contract, configuration, and
runtime status path exist.

Claim-driven collectors require an active workflow coordinator. The public Helm
chart rejects Terraform-state, AWS cloud, package-registry, SBOM-attestation,
provider security-alert, scanner-worker, or vulnerability-intelligence
Deployments unless all of these are true:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`
- `workflowCoordinator.collectorInstances` contains at least one instance

The runtime has the same guardrail: active coordinator mode requires claims to
be enabled and at least one enabled claim-capable collector instance. Individual
claim-driven collectors also reject missing, disabled, or non-claimable
instances.

## Implemented Collector Lanes

| Source family | Runtime state | Reducer and read state | Readiness gap |
| --- | --- | --- | --- |
| Git and repository | Ingester and Git collector paths emit repository, parser, relationship, and follow-up facts. | Workload identity, deployment mapping, code-call, semantic entity, SQL relationship, inheritance, package-source, and shared projection domains exist. | Prove sync, fact commit, queue drain, graph projection, and API/MCP truth on the target cluster. |
| Confluence documentation | `eshu-collector-confluence` reads one bounded Confluence scope. | Emits documentation source, document, section, link, and optional claim-candidate facts. Documentation facts remain evidence unless a reducer domain admits them. | Prove the configured Confluence scope, credentials, metrics, and status in the target environment. |
| OCI registry | `eshu-collector-oci-registry` reads registry targets. Runtime code supports direct and claim-aware modes. | Container image identity is digest-first. Explicit digests and single tag observations can become reducer image identity facts; ambiguous, unresolved, or stale tags stay diagnostic. | Prove registry collection in the target environment and keep image reads digest-bound before vulnerability impact work. |
| Terraform state | `eshu-collector-terraform-state` is claim-driven. | Terraform-state facts feed graph projection and `config_state_drift`. Drift v1 emits bounded counters and structured logs; graph/read-model promotion remains separate. | Prove live local or S3 state collection, redaction policy version, claim handoff, reducer drain, and management-status reads together. |
| AWS cloud | `eshu-collector-aws-cloud` is claim-driven. | AWS facts feed cloud-asset and AWS runtime-drift domains. AWS runtime drift writes durable reducer facts and bounded Postgres reads; graph shape remains reducer-owned. | Prove read-only AWS collection, claim-scoped credentials, AWS service coverage, reducer drain, drift reads, and status visibility in the target environment. |
| AWS freshness | The shared `eshu-webhook-listener` runtime handles Git provider webhooks and AWS EventBridge/AWS Config freshness deliveries. AWS deliveries persist durable wake-up triggers in Postgres; the listener does not collect AWS facts or write graph truth. | The workflow coordinator coalesces accepted freshness triggers into normal AWS collector claims. Scheduled scans remain the baseline completeness path. | Prove one live AWS EventBridge or AWS Config sample through webhook intake, trigger handoff, AWS work creation, and final status. |
| Incident-source freshness | The shared `eshu-webhook-listener` runtime accepts signed PagerDuty and Jira webhook deliveries as scoped wake-ups. It stores bounded provider, event, delivery, configured scope, and resource identifiers in Postgres, never provider payloads or facts. | The workflow coordinator authorizes each trigger against durable PagerDuty or Jira collector configuration, then creates normal claim-driven collector work for the matching `scope_id`. Polling remains the authoritative backfill path, and stale or unauthorized scopes fail explicitly. | Prove live signed PagerDuty and Jira samples through webhook intake, duplicate delivery coalescing, trigger handoff, scoped collector work creation, polling recovery, and final status. |
| Package registry | `eshu-collector-package-registry` is claim-driven and can collect configured package targets or coordinator-derived npm targets from active owned dependency facts. Derived package-registry targets are package-level and rotate through bounded full-corpus slices. | Package source correlation classifies source hints without ownership promotion and admits manifest-backed package consumption from package identity plus Git dependency evidence. Package-native dependency and publication facts are safe as provenance/read-model evidence. | Expand ownership correlation only after exact, derived, ambiguous, unresolved, stale, and rejected cases are proven. |
| SBOM and attestations | `eshu-collector-sbom-attestation` is claim-driven and can collect configured CycloneDX/SPDX SBOMs, in-toto statements, or OCI referrer documents without parsing inside the OCI registry collector. | Typed `sbom.*` and `attestation.*` facts feed `sbom_attestation_attachment`. Reducer attachment requires explicit subject digest evidence; parse warnings, verification status, and source document identity stay separate from attachment truth. API and MCP reads surface reducer attachment decisions through `list_sbom_attestation_attachments`. | Prove live or fixture document collection, source-URI redaction, parse-warning surfacing, reducer drain, API/MCP attachment reads, and subject-digest match/mismatch behavior in the target environment. |
| Vulnerability intelligence | `eshu-collector-vulnerability-intelligence` has source clients for CISA KEV, FIRST EPSS, OSV, and NVD. It can collect configured targets, configured mirror/fallback endpoints, cached/offline source artifacts, or coordinator-derived OSV npm, Swift, and Hex targets for exact owned dependency versions. Swift targets use Eshu's canonical `swift` ecosystem internally and are sent to OSV as `SwiftURL`; Hex targets use Eshu's canonical `hex` ecosystem. Derived vulnerability targets are package-version-level, grouped into bounded OSV querybatch work items when safe, and rotate through bounded full-corpus slices. | Source-truth `vulnerability.*` facts exist. Source-cache metadata is carried on `vulnerability.source_snapshot`; durable target freshness/checkpoint/retry state is carried in `vulnerability_source_states` and surfaced through status/API/MCP readiness. Neither is a finding. Impact reducers require owned package-manifest, lockfile, repository, image, or SBOM evidence before publishing user-facing impact findings. Exact lockfile versions, including Swift Package Manager `Package.resolved` remote source-control pins and Hex `mix.lock` entries, can prove observed package impact directly from active Git dependency facts; package-registry completion is enrichment, not the hard gate. Manifest ranges, branch-only pins, local/path pins, revision-only Swift pins, and Hex git dependencies stay partial or provenance-only evidence and are skipped for exact OSV target derivation. They must not infer reachability from CVSS, EPSS, KEV, product-only CPEs, cache freshness, or package-registry facts alone. | Prove live or offline source collection, source snapshot freshness/API/MCP visibility, source-state retry/freshness visibility, then package/image/deployment impact joins after upstream collectors are proven together. |
| Provider security alerts | `eshu-collector-security-alerts` is claim-driven for GitHub Dependabot repository alerts. It requires explicit credentials through `token_env`, repository allowlists, bounded `repository_alert_limit`, and bounded `max_pages` before issuing provider requests. | `security_alert.repository_alert` facts preserve provider alert state as source truth. `supply_chain_impact` can admit open provider alerts only when active owned dependency evidence matches the same canonical repository, package, and manifest path. Provider-scoped repository IDs are preserved as provider evidence and are not treated as canonical repository truth unless owned dependency evidence proves one unambiguous match. `security_alert_reconciliation` still records matched, unmatched, stale, fixed, dismissed, and provider-only outcomes, including missing or ambiguous evidence reasons. | Prove the configured GitHub repository allowlist, credential environment, rate-limit behavior, redaction, claim handoff, fact counts, reducer drain, API/MCP reads, and private-data handling in the target environment. |
| PagerDuty incident context | `eshu-collector-pagerduty` is claim-driven for PagerDuty incidents, incident log entries, related change events, and optional live service/integration configuration validation. It requires explicit credentials through `token_env`, bounded incident/log/change limits, an incident lookback window, optional service allowlists, and bounded `config_resource_limit` before issuing provider requests. Public Helm chart support is pending. | `incident.record`, `incident.lifecycle_event`, and `change.record` facts preserve PagerDuty incident state as source truth. Optional `incident_routing.observed_pagerduty_service`, `incident_routing.observed_pagerduty_integration`, and `incident_routing.coverage_warning` facts preserve live PagerDuty routing evidence without overwriting Terraform declared/applied evidence. The incident-context API/MCP read returns provider incident state, timeline entries, fallback service/time change candidates, and explicit missing slots for build/deploy, commit, pull request, and Jira/work item evidence. Deployable, image, and runtime artifact slots are filled only when a service-catalog operational link exactly names the PagerDuty service URL and reducer-owned catalog, image identity, or Kubernetes correlation facts prove the hop. Build/deploy and commit slots are filled only from reducer-owned CI/CD run correlations tied to the selected image digest or reference; tag-only matches stay derived. Pull-request slots use provider merged-PR evidence tied to the selected commit. Jira remote links or issue keys can enrich work-item slots but do not verify PR identity by themselves. | Prove the configured PagerDuty target, credential environment, rate-limit behavior, redaction, optional config validation, claim handoff, fact counts, reducer correlation follow-up, API/MCP reads, and private-data handling in the target environment. |
| Jira work items | `eshu-collector-jira` is claim-driven for Jira Cloud issue scopes. It requires explicit credentials through `token_env`, optional `email_env`, bounded JQL, bounded issue/changelog/remote-link limits, and an updated-window lookback before issuing provider requests. | `work_item.record`, `work_item.transition`, and `work_item.external_link` preserve Jira source truth. They can enrich incident context when linked, but they are not required for PagerDuty incidents and do not create deployment, code, or pull-request truth by themselves. | Prove credential resolution, permission-hidden/deleted/archived/rate-limit classification, redaction, empty-window commits, claim handoff, fact counts, reducer drain, API/MCP reads, and private-data handling in the target environment. |
| Scanner worker | `eshu-scanner-worker` is claim-driven and isolated from reducer lanes. The built-in warning analyzer emits `scanner_worker.warning` source facts until a concrete analyzer is configured. The bounded `image_unpacking` analyzer (`internal/collector/scannerworker/imageanalyzer`) reads configured local image rootfs metadata or ordered OCI layer tar streams and emits installed OS package facts only when apk/dpkg package database proof exists. The bounded `sbom_generation` analyzer (`internal/collector/scannerworker/sbomgenerator`) emits CycloneDX-compatible `sbom.document`, `sbom.component`, and `sbom.warning` source facts for repository, image, or artifact targets when the runtime source has enough subject evidence, and falls back to `scanner_worker.warning` with `reason="sbom_generator_source_not_configured"` until a runtime-owned source is wired. The `os_package_extraction` analyzer parses configured Alpine or Debian rootfs targets into OS package source facts. | Scanner workers emit source facts only. Reducers own vulnerability finding admission, priority, readiness, and graph truth. Scanner-generated SBOM documents flow through `sbom_attestation_attachment` exactly like collector-fetched SBOM documents; they cannot bypass attachment truth. OS package extraction and image unpacking do not match advisories or publish findings. | Prove concrete analyzers with target count, fact count, runtime, CPU, memory, queue state, retry count, dead-letter count, pprof, and reducer/API truth before enabling them by default. Bounded SBOM generation must additionally prove reducer attachment admission and the safe `unknown_subject` fallback when no subject digest is derivable. |

The broader vulnerability architecture, including target/capability separation,
readiness states, provider-alert parity, local one-shot scanning, and
scanner-worker boundaries, is documented in
[Security Intelligence](security-intelligence.md).

PagerDuty incident-routing evidence is landing in stages. Terraform-state
applied PagerDuty/AWS alert-route facts and optional live PagerDuty
service/integration observations are source-fact lanes today; Terraform-source
declared evidence, broader live resource classes, and reducer/API/MCP
comparison remain follow-up implementation paths. See
[PagerDuty Evidence Contract](pagerduty-evidence.md). These source facts do not
promote production readiness by themselves until reducer truth and read surfaces
are proven together.

## Reducer Truth Boundaries

Collector readiness depends on the reducer admitting explicit evidence, not on
the collector naming something truth. Current reducer-owned surfaces include:

| Domain | Operator contract |
| --- | --- |
| `cloud_asset_resolution` | Cloud asset identity is admitted from source, applied, and observed resource layers. |
| `config_state_drift` | Terraform config-vs-state drift v1 emits bounded counters and logs; it is not a graph write. |
| `package_source_correlation` | Package source hints stay provenance until exact, derived, ambiguous, unresolved, stale, and rejected cases are proven. |
| `container_image_identity` | Image identity is digest-keyed; weak, ambiguous, unresolved, or stale tag observations stay diagnostic. |
| `aws_cloud_runtime_drift` | AWS drift findings are durable reducer facts with bounded Postgres reads; graph shape remains reducer-owned. |
| `ci_cd_run_correlation` | Exact CI/CD correlation requires artifact identity evidence, not CI success alone. |
| `service_catalog_correlation` | Catalog names, owners, and labels remain provenance until explicit repository evidence admits correlation. |
| `sbom_attestation_attachment` | SBOM and attestation attachment requires explicit subject digest evidence; parse validity and verification trust stay separate. |
| `supply_chain_impact` | Vulnerability impact findings come from explicit CVE/advisory to package/component to repository/image evidence paths. Source-only vulnerability intelligence is retained as facts but stays out of user-facing impact findings until it joins to owned package-manifest, lockfile, repository, image, or SBOM evidence. Package-lock and Mix lockfile evidence preserve the dependency path, depth, and direct/transitive flag when the source gives Eshu enough chain data. Package-registry version facts are source metadata, not installed-version proof. |
| `security_alert_reconciliation` | Provider alert state is compared with owned dependency and impact evidence. Rows can be matched, unmatched, stale, dismissed, fixed, or provider-only. Raw provider repository identity is preserved separately from canonical Eshu repository identity. Open alerts may also seed supply-chain impact only after the dependency evidence gate matches exactly one repository, package, and manifest path. |

Workflow completeness depends on reducer-owned phase publications only for
collector families that declare required phases. Git and Terraform-state have
required graph projection phases. AWS, OCI registry, package registry,
SBOM-attestation, provider security alerts, and documentation currently publish
fact-backed or read-model truth without required workflow phase gates.

## Gated Source Families

Do not present these as deployed collector lanes until their hosted runtime,
fact contract, reducer contract, fixtures, telemetry, and chart path are all
implemented:

| Source family | Current state |
| --- | --- |
| Kubernetes live | No hosted collector runtime or charted workload. |
| Concrete scanner analyzers | The `eshu-scanner-worker` runtime, warning analyzer, configured `image_unpacking` image/rootfs analyzer, configured repository-manifest `sbom_generation` source, `os_package_extraction` rootfs parser, Compose service, and opt-in Helm Deployment exist. Secret, license, source, and misconfiguration analyzers are not enabled by default until target count, fact count, runtime, CPU, memory, queue state, retry count, dead-letter count, pprof, and reducer/API truth are proven in the target environment. |
| Kubernetes live | Foundation only: `eshu-collector-kubernetes-live` lists a read-only core resource set (namespaces, pods, deployments, replicasets, services, ingresses) and emits `kubernetes_live.pod_template`, `kubernetes_live.relationship`, and `kubernetes_live.warning` source facts through `collector.Service`. No claim-driven runtime, watch mode, reducer projection, drift read model, or charted workload yet; the #388 correlation/drift work and Helm path remain pending. |
| Concrete scanner analyzers | The `eshu-scanner-worker` runtime, warning analyzer, bounded `image_unpacking` rootfs/layer analyzer, bounded `sbom_generation` fallback, `os_package_extraction` rootfs parser, Compose service, and opt-in Helm Deployment exist. Concrete analyzers are not enabled by default until target count, fact count, runtime, CPU, memory, queue state, retry count, dead-letter count, pprof, and reducer/API truth are proven in the target environment. |
| CI/CD runs | Fixture normalizer and reducer correlation exist; hosted provider polling is not a deployed lane. |
| Service catalog, observability, incident/change correlation, secrets/IAM posture, GCP, Azure, multi-cloud | Design or research only for deployed collector readiness. PagerDuty source facts, reducer-owned image-to-build/commit evidence, provider PR provenance, and Jira work-item enrichment exist, and the [Observability Evidence Contract](observability-evidence.md) defines the declared/applied/observed source-fact contract for future Grafana-stack work. Broader root-cause and cross-provider incident correlation remains gated. |

## Promotion Proof

Before treating a collector lane as production-ready, capture evidence for the
same deployment shape operators will use:

1. Render or deploy only implemented collectors plus the API, MCP, ingester,
   reducer, and coordinator runtimes needed for the proof.
2. Prove `/healthz`, `/readyz`, `/admin/status`, and `/metrics` for each
   enabled runtime that exposes the shared admin surface.
3. For claim-driven collectors, prove active coordinator claims, claim leases,
   heartbeat behavior, expired-claim reaping, and no duplicate open target work.
4. Confirm facts in Postgres by `collector_kind`, `fact_kind`, scope, and active
   generation.
5. Confirm reducer queues drain to zero without dead letters.
6. Confirm graph truth, read-model truth, API truth, and MCP truth agree for
   the source family being promoted.
7. Record wall time, fact count, queue count, retry count, dead-letter count,
   backend, chart values shape, image digest, and commit SHA.

Keep the detailed test matrix with the package that owns the behavior. Start
with the collector, workflow, reducer, query, MCP, telemetry, and storage
package READMEs instead of duplicating their local contracts here.

If any step fails, fix the owning layer instead of adding a broader fallback.
Collection bugs belong in collectors or workflow planning. Truth bugs belong in
reducers, graph projection, or read-model stores. Operator visibility bugs
belong in status, telemetry, or runtime wiring.

## Maintainer Details

Implementation details live with the owning packages:

- `go/internal/collector/README.md`
- `go/internal/workflow/README.md`
- `go/internal/reducer/README.md`
- `go/internal/query/README.md`
- `go/internal/storage/postgres/README.md`
