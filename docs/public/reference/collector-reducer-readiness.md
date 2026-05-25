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
- remote-E2E-gated vulnerability intelligence collection
- claim-driven scanner-worker warning facts for isolated analyzer execution
- webhook listener intake for Git provider events and AWS freshness triggers

The scanner-worker lane is deployed as an isolated analyzer boundary. It
defines claim input, target scope, resource limits, source fact output,
retry/dead-letter payloads, telemetry names, Compose wiring, and an opt-in Helm
Deployment. The built-in warning analyzer proves source-fact emission without
claiming a target is clean.

Do not add chart values for design-only collectors. A Helm knob is an operator
promise; only chart collectors whose binary, fact contract, configuration, and
runtime status path exist.

Claim-driven collectors require an active workflow coordinator. The public Helm
chart rejects Terraform-state, AWS cloud, package-registry, SBOM-attestation,
scanner-worker, or vulnerability-intelligence Deployments unless all of these
are true:

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
| Package registry | `eshu-collector-package-registry` is claim-driven and can collect configured package targets or coordinator-derived npm targets from active owned dependency facts. | Package source correlation classifies source hints without ownership promotion and admits manifest-backed package consumption from package identity plus Git dependency evidence. Package-native dependency and publication facts are safe as provenance/read-model evidence. | Expand ownership correlation only after exact, derived, ambiguous, unresolved, stale, and rejected cases are proven. |
| SBOM and attestations | `eshu-collector-sbom-attestation` is claim-driven and can collect configured CycloneDX/SPDX SBOMs, in-toto statements, or OCI referrer documents without parsing inside the OCI registry collector. | Typed `sbom.*` and `attestation.*` facts feed `sbom_attestation_attachment`. Reducer attachment requires explicit subject digest evidence; parse warnings, verification status, and source document identity stay separate from attachment truth. API and MCP reads surface reducer attachment decisions through `list_sbom_attestation_attachments`. | Prove live or fixture document collection, source-URI redaction, parse-warning surfacing, reducer drain, API/MCP attachment reads, and subject-digest match/mismatch behavior in the target environment. |
| Vulnerability intelligence | `eshu-collector-vulnerability-intelligence` has source clients for CISA KEV, FIRST EPSS, OSV, and NVD. It can collect configured targets, configured mirror/fallback endpoints, cached/offline source artifacts, or coordinator-derived OSV npm targets for exact owned dependency versions. | Source-truth `vulnerability.*` facts exist. Source-cache metadata is carried on `vulnerability.source_snapshot`; durable target freshness/checkpoint/retry state is carried in `vulnerability_source_states` and surfaced through status/API/MCP readiness. Neither is a finding. Impact reducers require owned package-manifest, lockfile, repository, image, or SBOM evidence before publishing user-facing impact findings. Exact lockfile versions can prove observed package impact; manifest ranges stay partial evidence and are skipped for exact OSV target derivation. They must not infer reachability from CVSS, EPSS, KEV, product-only CPEs, cache freshness, or package-registry facts alone. | Prove live or offline source collection, source snapshot freshness/API/MCP visibility, source-state retry/freshness visibility, then package/image/deployment impact joins after upstream collectors are proven together. |
| Provider security alerts | `security_alert` currently has synthetic GitHub Dependabot fixture normalization and a bounded allowlisted request client shape, not a hosted collector lane. | `security_alert.repository_alert` facts preserve provider alert state as source truth. `security_alert_reconciliation` reducer facts compare provider alerts with owned dependency and supply-chain impact evidence while keeping provider state separate from Eshu impact truth. | Prove hosted collection credentials, allowlists, rate limits, redaction, claim handoff, fact counts, reducer drain, API/MCP reads, and private-data handling before enabling live provider alert collection. |
| Scanner worker | `eshu-scanner-worker` is claim-driven and isolated from reducer lanes. The built-in warning analyzer emits `scanner_worker.warning` source facts until a concrete analyzer is configured. | Scanner workers emit source facts only. Reducers own vulnerability finding admission, priority, readiness, and graph truth. | Prove concrete analyzers with target count, fact count, runtime, CPU, memory, queue state, retry count, dead-letter count, pprof, and reducer/API truth before enabling them by default. |

The broader vulnerability architecture, including target/capability separation,
readiness states, provider-alert parity, local one-shot scanning, and
scanner-worker boundaries, is documented in
[Security Intelligence](security-intelligence.md).

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
| `supply_chain_impact` | Vulnerability impact findings come from explicit CVE/advisory to package/component to repository/image evidence paths. Source-only vulnerability intelligence is retained as facts but stays out of user-facing impact findings until it joins to owned package-manifest, lockfile, repository, image, or SBOM evidence. Package-lock evidence preserves the dependency path, depth, and direct/transitive flag when npm gives Eshu enough chain data. Package-registry version facts are source metadata, not installed-version proof. |
| `security_alert_reconciliation` | Provider alert state is compared with owned dependency and impact evidence. Rows can be matched, unmatched, stale, dismissed, fixed, or provider-only, but they do not promote provider state into canonical supply-chain impact truth. |

Workflow completeness depends on reducer-owned phase publications only for
collector families that declare required phases. Git and Terraform-state have
required graph projection phases. AWS, OCI registry, package registry,
SBOM-attestation, and documentation currently publish fact-backed or read-model
truth without required workflow phase gates.

## Gated Source Families

Do not present these as deployed collector lanes until their hosted runtime,
fact contract, reducer contract, fixtures, telemetry, and chart path are all
implemented:

| Source family | Current state |
| --- | --- |
| Kubernetes live | No hosted collector runtime or charted workload. |
| Concrete scanner analyzers | The `eshu-scanner-worker` runtime, warning analyzer, Compose service, and opt-in Helm Deployment exist. Concrete analyzers beyond the warning analyzer remain gated until target count, fact count, runtime, CPU, memory, queue state, retry count, dead-letter count, pprof, and reducer/API truth are proven. |
| CI/CD runs | Fixture normalizer and reducer correlation exist; hosted provider polling is not a deployed lane. |
| Service catalog, observability, incident/change, secrets/IAM posture, GCP, Azure, multi-cloud | Design or research only for deployed collector readiness. |

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
