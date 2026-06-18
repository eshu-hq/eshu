# Collector And Reducer Readiness

Use this page to decide whether a source family is ready for deployed
collection, reducer materialization, and API or MCP reads. Eshu is facts-first:
collectors and webhooks observe source systems and commit facts; the resolution
engine owns graph truth, read-model truth, retries, dead letters, and completion
state.

A collector is not production-ready just because its binary exists. The
deployment path must also prove bounded collection, durable facts, reducer
drain, and operator-visible status.
Reducer claim-path changes that affect readiness gating or domain-count growth
must satisfy the
[Reducer Claim-Latency Gate](reducer-claim-latency-gate.md) before production
support is claimed.
Resource reducer conflict policy is explicit: only
`aws_resource_materialization` is promoted to a versioned hashed
`cloud_resource_node` conflict family today. GCP, Azure, EC2, Kubernetes, and
security-group node materializers remain risky resource-scope fallbacks until
partition-filtered handler proof exists. Relationship, posture, IAM, S3, RDS,
Kubernetes-correlation, and security-group reachability domains stay blocked
behind a hashed `resource_scope` fallback while their handlers still load,
write, or retract whole scope generations. Queue conflict keys must not store
raw provider locators, paths, credential-shaped values, provider payload
excerpts, or IP address-shaped values. The buyer-facing version of this
promotion line, including the AWS-first cloud posture statement and the
gated/roadmap surfaces, lives in the
[roadmap promotion-readiness tables](../roadmap.md#promotion-readiness) and the
[Supply-Chain Traceability](../supply-chain-traceability.md) entry point.

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
| SBOM and attestations | `eshu-collector-sbom-attestation` is claim-driven and can collect configured CycloneDX/SPDX SBOMs, in-toto statements, or OCI referrer documents without parsing inside the OCI registry collector. | Typed `sbom.*` and `attestation.*` facts feed `sbom_attestation_attachment`. Reducer attachment requires explicit subject digest evidence; parse warnings, verification status, and source document identity stay separate from attachment truth. API and MCP reads surface reducer attachment decisions through `list_sbom_attestation_attachments` by source repository, workload, service, image digest, or document anchor, with missing repository/workload/service-to-image and image-to-SBOM evidence explicit. | Prove live or fixture document collection, source-URI redaction, parse-warning surfacing, reducer drain, scoped API/MCP attachment reads, and subject-digest match/mismatch behavior in the target environment. |
| Vulnerability intelligence | `eshu-collector-vulnerability-intelligence` has source clients for CISA KEV, FIRST EPSS, OSV, and NVD. It can collect configured targets, configured mirror/fallback endpoints, cached/offline source artifacts, or coordinator-derived OSV npm, Pub, Swift, and Hex targets for exact owned dependency versions. Swift targets use Eshu's canonical `swift` ecosystem internally and are sent to OSV as `SwiftURL`; Pub targets keep canonical `pub` and use OSV's Pub ecosystem; Hex targets use Eshu's canonical `hex` ecosystem. Derived vulnerability targets are package-version-level, grouped into bounded OSV querybatch work items when safe, and rotate through bounded full-corpus slices. | Source-truth `vulnerability.*` facts exist. Source-cache metadata is carried on `vulnerability.source_snapshot`; durable target freshness/checkpoint/retry state is carried in `vulnerability_source_states` and surfaced through status/API/MCP readiness. Neither is a finding. Impact reducers require owned package-manifest, lockfile, repository, image, or SBOM evidence before publishing user-facing impact findings. Exact lockfile versions, including Pub `pubspec.lock` hosted versions, Swift Package Manager `Package.resolved` remote source-control pins, and Hex `mix.lock` entries, can prove observed package impact directly from active Git dependency facts; package-registry completion is enrichment, not the hard gate. Manifest ranges, Pub git/path/private-hosted rows, Pub dependency overrides, branch-only pins, local/path pins, revision-only Swift pins, and Hex git dependencies stay partial or provenance-only evidence and are skipped for exact OSV target derivation. They must not infer reachability from CVSS, EPSS, KEV, product-only CPEs, cache freshness, or package-registry facts alone. | Prove live or offline source collection, source snapshot freshness/API/MCP visibility, source-state retry/freshness visibility, then package/image/deployment impact joins after upstream collectors are proven together. |
| Provider security alerts | `eshu-collector-security-alerts` is claim-driven for GitHub Dependabot repository alerts. It requires explicit credentials through `token_env`, repository allowlists, bounded `repository_alert_limit`, and bounded `max_pages` before issuing provider requests. Remote E2E also runs `--preflight-provider-access` before workflow fanout so bad provider access fails before work items are claimed. | `security_alert.repository_alert` facts preserve provider alert state as source truth. `supply_chain_impact` can admit open provider alerts only when active owned dependency evidence matches the same canonical repository, package, and manifest path. Provider-scoped repository IDs are preserved as provider evidence and are not treated as canonical repository truth unless owned dependency evidence proves one unambiguous match. `security_alert_reconciliation` records matched, unmatched, stale, fixed, dismissed, provider-only, unsupported, and ambiguous outcomes with row-level reason codes and missing-evidence details. | Prove the configured GitHub repository allowlist, credential environment, bounded provider-access preflight, rate-limit behavior, redaction, claim handoff, fact counts, reducer drain, API/MCP reads, and private-data handling in the target environment. |
| PagerDuty incident context | `eshu-collector-pagerduty` is claim-driven and charted through `pagerDutyCollector` for PagerDuty incidents, incident log entries, related change events, and optional live service/integration configuration validation. It requires explicit credentials through `token_env`, bounded incident/log/change limits, an incident lookback window, optional service allowlists, and bounded `config_resource_limit` before issuing provider requests. Signed PagerDuty webhooks can wake matching `scope_id` targets through the shared webhook listener, but polling remains the completeness path. | `incident.record`, `incident.lifecycle_event`, and `change.record` facts preserve PagerDuty incident state as source truth. Optional `incident_routing.observed_pagerduty_service`, `incident_routing.observed_pagerduty_integration`, and `incident_routing.coverage_warning` facts preserve live PagerDuty routing evidence without overwriting Terraform declared/applied evidence. The incident-context API/MCP read returns provider incident state, timeline entries, intended Terraform-source routing, applied Terraform-state routing, live PagerDuty routing, fallback service/time change candidates, and explicit missing slots for build/deploy, commit, pull request, and Jira/work item evidence. Reducer graph materialization writes `IncidentRoutingEvidence` only for exact declared/applied/live convergence or exact live-only no-IaC evidence; unsafe routing outcomes remain provenance-only. Deployable, image, and runtime artifact slots are filled only when a service-catalog operational link exactly names the PagerDuty service URL and reducer-owned catalog, image identity, or Kubernetes correlation facts prove the hop. Build/deploy and commit slots are filled only from reducer-owned CI/CD run correlations tied to the selected image digest or reference; tag-only matches stay derived. Pull-request slots use provider merged-PR evidence tied to the selected commit. Jira remote links or issue keys can enrich work-item slots but do not verify PR identity by themselves. | Prove the configured PagerDuty target, credential environment, rate-limit behavior, redaction, optional config validation, claim handoff, fact counts, routing read-model slots, reducer graph evidence counts, reducer correlation follow-up, API/MCP reads, and private-data handling in the target environment. |
| Jira work items | `eshu-collector-jira` is claim-driven and charted for Jira Cloud issue scopes. It requires explicit credentials through `token_env`, optional `email_env`, direct `jql` or env-backed `jql_env`, bounded issue/changelog/remote-link limits, and an updated-window lookback before issuing provider requests. Helm supports polling-only mode through `jiraCollector` and webhook-enabled freshness mode through the shared webhook listener plus a matching Jira `scopeId`. | `work_item.record`, `work_item.transition`, and `work_item.external_link` preserve Jira source truth. They can enrich incident context when linked, but they are not required for PagerDuty incidents and do not create deployment, code, or pull-request truth by themselves. The completion boundary and fixture matrix live in [Jira Evidence Contract](jira-evidence.md). | Prove credential resolution, JQL env resolution when configured, permission-hidden/deleted/archived/rate-limit classification, redaction, empty-window commits, claim handoff, fact counts, reducer drain, API/MCP reads, and private-data handling in the target environment. |
| Live observability | `eshu-collector-grafana`, `eshu-collector-prometheus-mimir`, `eshu-collector-loki`, and `eshu-collector-tempo` are claim-driven and charted. They require explicit live targets in `ESHU_COLLECTOR_INSTANCES_JSON`; Grafana requires `token_env`, while Prometheus/Mimir, Loki, and Tempo can use unauthenticated endpoints or optional `token_env` plus optional tenant envs. | Live collectors emit metadata-only `observability.*` source facts for source instances, dashboards, rules, targets, log signals, trace signals, and coverage warnings. Declared IaC/GitOps evidence remains preferred when current. Live facts are fallback and validation evidence for no-IaC, drift, freshness, and effective target/rule/signal state; reducers and read surfaces own graph truth and comparison outcomes. | Prove each configured target, credential resolution, permission-hidden/rate-limit/stale/partial/failure classification, status visibility, fact counts, reducer drain, API/MCP reads, private-data handling, and no log-line/span/query-body leakage in the target environment. |
| Scanner worker | `eshu-scanner-worker` is claim-driven and isolated from reducer lanes. The built-in warning analyzer emits `scanner_worker.warning` source facts until a concrete analyzer is configured. The bounded `image_unpacking` analyzer (`internal/collector/scannerworker/imageanalyzer`) reads configured local image rootfs metadata or ordered OCI layer tar streams and emits installed OS package facts only when apk/dpkg package database proof exists. The bounded `sbom_generation` analyzer (`internal/collector/scannerworker/sbomgenerator`) emits CycloneDX-compatible `sbom.document`, `sbom.component`, and `sbom.warning` source facts for repository, image, or artifact targets when the runtime source has enough subject evidence, and falls back to `scanner_worker.warning` with `reason="sbom_generator_source_not_configured"` until a runtime-owned source is wired. The `os_package_extraction` analyzer parses configured Alpine or Debian rootfs targets into OS package source facts. | Scanner workers emit source facts only. Reducers own vulnerability finding admission, priority, readiness, and graph truth. Scanner-generated SBOM documents flow through `sbom_attestation_attachment` exactly like collector-fetched SBOM documents; they cannot bypass attachment truth. OS package extraction and image unpacking do not match advisories or publish findings. | Prove concrete analyzers with target count, fact count, runtime, CPU, memory, queue state, retry count, dead-letter count, pprof, and reducer/API truth before enabling them by default. Bounded SBOM generation must additionally prove reducer attachment admission and the safe `unknown_subject` fallback when no subject digest is derivable. |

The broader vulnerability architecture, including target/capability separation,
readiness states, provider-alert parity, local one-shot scanning, and
scanner-worker boundaries, is documented in
[Security Intelligence](security-intelligence.md).

PagerDuty incident-routing evidence is landing in stages. Terraform-state
applied PagerDuty/AWS alert-route facts and optional live PagerDuty
service/integration observations are source-fact lanes today; Terraform-source
declared evidence is available through `PagerDutyDeclaration` content rows. The
API/MCP read model compares declared, applied, and live routing evidence, and
the reducer graph slice materializes exact `IncidentRoutingEvidence` only for
safe convergence or live-only no-IaC evidence. Broader live resource classes and
alert-route-to-service comparison remain follow-up implementation paths. See
[PagerDuty Evidence Contract](pagerduty-evidence.md). These source facts do not
promote production readiness by themselves until reducer truth, graph evidence,
and read surfaces are proven together.

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
| `deployable_unit_correlation` | Candidate evaluation publishes phase state today; canonical deployable-unit edges remain gated by the [Deployable-Unit Correlation](deployable-unit-correlation.md) admission contract and must not be inferred from phase state alone. |
| `sbom_attestation_attachment` | SBOM and attestation attachment requires explicit subject digest evidence; parse validity and verification trust stay separate. |
| `supply_chain_impact` | Vulnerability impact findings come from explicit CVE/advisory to package/component to repository/image evidence paths. Source-only vulnerability intelligence is retained as facts but stays out of user-facing impact findings until it joins to owned package-manifest, lockfile, repository, image, or SBOM evidence. Package-lock and Mix lockfile evidence preserve the dependency path, depth, and direct/transitive flag when the source gives Eshu enough chain data. Package-registry version facts are source metadata, not installed-version proof. |
| `security_alert_reconciliation` | Provider alert state is compared with owned dependency and impact evidence. Rows can be matched, unmatched, stale, dismissed, fixed, provider-only, unsupported, or ambiguous. Raw provider repository identity is preserved separately from canonical Eshu repository identity. Open alerts may also seed supply-chain impact only after the dependency evidence gate matches exactly one repository, package, and manifest path. |
| `incident_routing_materialization` | PagerDuty incident-routing graph evidence is exact-only: declared/applied/live convergence and live-only no-IaC can materialize `IncidentRoutingEvidence`; drifted, ambiguous, stale, permission-hidden, derived, rejected, unresolved, and missing evidence remains provenance-only. |

Workflow completeness depends on reducer-owned phase publications only for
collector families that declare required phases. Git and Terraform-state have
required graph projection phases. AWS, OCI registry, package registry,
SBOM-attestation, provider security alerts, CI/CD runs, and documentation
currently publish fact-backed or read-model truth without required workflow
phase gates.

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
| CI/CD runs | Fixture normalizer, reducer correlation, bounded GitHub Actions runtime source, workflow planner, hosted binary, chart values, provider telemetry, and fact-backed central collector status evidence exist. The hosted runtime strips token-bearing artifact URLs before fact emission and requires explicit repository allowlists plus run/job/artifact limits. It is not fully promoted until live target proof captures health, readiness, metrics, status, claim leases, fact counts, queue state, and reducer/API truth for the deployed chart shape. |
| Service catalog | Repo-hosted Backstage, OpsLevel, and Cortex descriptors emit `service_catalog.*` facts through Git collection, and the provenance-only projector, reducer, API, and MCP read paths exist. Hosted Backstage/OpsLevel/Cortex API polling, credentials, provider rate-limit budgets, and charted catalog collector runtimes are not deployed lanes yet. |
| Google Workspace documentation | No hosted runtime, chart path, Compose profile, or Go collector package exists. The mock-only internal package was removed because a facade without a real provider implementation is not a collector readiness signal. Offline `google_workspace_export` manifest values remain import-source identifiers only, not a live provider collector. |
| Incident/change correlation, secrets/IAM posture | Design or research only for deployed collector readiness. PagerDuty source facts, reducer-owned image-to-build/commit evidence, provider PR provenance, Jira work-item enrichment, and live observability source facts exist. The [Secrets And IAM Posture Collector Contract](secrets-iam-posture-collector-contract.md) locks the source boundaries, scopes, redaction policy, fact families, reducer ownership, fixture gates, and graph-promotion non-goals for issue #25. Broader root-cause, cross-provider incident correlation, and secrets/IAM graph promotion remain gated. |
| GCP, Azure, and multi-cloud runtime collection | The [Multi-Cloud Runtime Collector Contract](multi-cloud-collector-contract.md) defines the IaC-first evidence model, explicit `gcp` and `azure` collector kinds, shared fact fields, reducer-owned `cloud_resource_uid` promotion, and query source states. GCP has fixture-driven Cloud Asset Inventory source facts, explicit claimed-live command wiring, Helm exposure, opt-in direct/effective tag API evidence, shared cloud inventory admission/readback, tag evidence admission, image identity admission, relationship resolution, and IAM trust facts. GCP remains gated for sanitized live smoke proof. Azure now has fixture-driven Resource Graph source facts, shared cloud inventory admission/readback, tag/identity/image/relationship evidence, explicit claimed-live command wiring (`-mode claimed-live`, fixture-proven claim handoff/idempotency/partial-scope, bounded `eshu_dp_azure_claims_total` claim metric), and default-off Helm exposure (deployment, metrics service, ServiceMonitor, render-time validation) mirroring GCP. Azure remains gated for sanitized live smoke proof: promotion to `implemented` requires an operator-run live proof recording fact counts, reducer drain, status/readback, and no private-identifier leakage against a real tenant; until then its promotion state is `partial`/`gated` (see issue #3024). See the [GCP Cloud Collector Contract](gcp-cloud-collector-contract.md) and [Azure Cloud Collector Contract](azure-cloud-collector-contract.md). |

Collector Performance Evidence: `go test ./internal/collector -run TestNoMockOnlyGoogleWorkspaceCollectorPackage -count=1`
guards the removed facade without adding runtime work.

Collector Observability Evidence: no runtime collector, worker, HTTP client, or
queue path exists for Google Workspace documentation.

Collector Deployment Evidence: no deployment artifact exists for Google
Workspace documentation.

No-Observability-Change: this removal adds no metric, span, log, status row,
database query, graph write, queue consumer, or hosted runtime path.

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

### Machine-Readable Promotion Proof Report

`/admin/status` now carries a deterministic, credential-safe promotion proof per
collector family or instance under `collector_promotion_proofs` (text surface:
`Collector promotion proofs:`). It is the machine-checkable evidence a reviewer
uses instead of stitching together claim, fact, reducer, and telemetry state by
hand. The report is generated from existing runtime/status/readback sources only;
it contains no credentials and no raw source payloads — only counts,
evidence-source labels, bounded source-system names, and safe blocker strings.

Each proof carries a closed `promotion_state`:

| State | Reviewer reading |
| --- | --- |
| `implemented` | Healthy, fresh, reducer-readback evidence. Promotable. |
| `partial` | Evidence exists but the implemented contract is unmet (readback pending, claims inactive, or a fixture-only lane). Not promotable yet. |
| `failed` | Runtime health degraded. Reject and fix the owning layer. |
| `stale` | Newest evidence older than the freshness window. Re-run before trusting. |
| `gated` | Claim-driven lane with claims disabled, or hidden by a runtime profile gate. Expected for preview lanes. |
| `disabled` | Registered but disabled or deactivated. |
| `permission_hidden` | Hidden from the caller by an active permission scope; metadata is redacted. |
| `unsupported` | Known family with no configured instance. |

**How a reviewer uses it to promote or reject a collector:**

1. Pull the full-fleet report (default catalog) and locate the collector family.
2. Promote only when `promotion_state` is `implemented`: this guarantees
   `reducer_readback: available`, non-degraded health, and fresh evidence.
3. Reject `failed`; fix the owning layer (collector/workflow for collection
   bugs, reducer/projection for truth bugs) rather than adding a fallback.
4. Treat `partial` as "in progress" — read `blockers` and `reducer_readback`
   for the exact gap. A lane with `fixture_only: true` is never `implemented`;
   live promotion still requires the fixture-to-runtime parity harness plus a
   live smoke against the real provider.
5. Treat `gated`/`disabled`/`unsupported`/`permission_hidden` as not-yet-live by
   design; confirm the gate or missing instance is intentional.

The catalog spine is `scope.AllCollectorKinds()`, so adding a collector
automatically adds a readiness lane — there is no separate checklist to drift.
The global `/admin/status` payload reports only collectors that are present; the
full-fleet enumeration (including `unsupported` no-instance lanes) is available
through the dedicated collector-readiness read model.

If any step fails, fix the owning layer instead of adding a broader fallback.
Collection bugs belong in collectors or workflow planning. Truth bugs belong in
reducers, graph projection, or read-model stores. Operator visibility bugs
belong in status, telemetry, or runtime wiring.

### Vulnerability Intelligence Promotion Proof

The `eshu-collector-vulnerability-intelligence` lane is remote-E2E-gated until a
live run records `promotion_state: implemented` for it in the machine-readable
report above. This is the lane-specific procedure to drive and record that
proof; it specializes the generic steps for the four sources (CISA KEV, FIRST
EPSS, OSV, NVD). Tracking issue:
[#3014](https://github.com/eshu-hq/eshu/issues/3014).

1. Bring up the stack with the collector enabled and its targets configured.
   KEV, EPSS, and OSV collect against their public endpoints (or a configured
   mirror/offline artifact). NVD is rate-limited: configure an NVD API key, or
   run KEV/EPSS/OSV live and treat NVD as key-gated and record it as such — do
   not silently skip it.
2. Per source, confirm source-truth facts in Postgres by `collector_kind`,
   `fact_kind`, scope, and active generation, and record the fact count.
3. Confirm `vulnerability.source_snapshot` cache freshness is visible through
   `get_capability_catalog` and the readiness API, and that
   `vulnerability_source_states` is populated and surfaced through status /
   API / MCP readiness (target freshness, checkpoint, retry state).
4. Confirm reducer queues drain to zero with no dead letters.
5. Confirm an end-to-end path: a published CVE joins owned package/manifest/
   lockfile/image/SBOM evidence and surfaces as a published impact finding
   through `list_supply_chain_impact_findings` / `explain_supply_chain_impact`
   — not from CVSS, EPSS, KEV, or product-only CPEs alone.
6. Read `collector_promotion_proofs` for the vulnerability-intelligence family
   from `/admin/status` and confirm `promotion_state: implemented`.

#### Recorded live run (2026-06-18)

Isolated minimal stack (project `eshu-3014-vulnproof`), live collection of
CVE-2021-44228 / Maven `log4j-core` 2.14.1 targets. KEV, EPSS, OSV collected
against their public endpoints; NVD ran key-gated (no API key configured — a
single CVE-by-ID lookup stayed inside the anonymous-tier rate limit). All four
`vulnerability_source_states` rows reached `terminal_status = succeeded` with no
error class; the four `vulnerability_intelligence` workflow work items completed
(0 pending/failed); `graph_projection_phase_repair_queue` depth 0.

| Source | Target | Fact count (live) | Source-state | Work-item drain | Readiness surface | Promotion state |
| --- | --- | --- | --- | --- | --- | --- |
| CISA KEV | `vuln-intel://cisa/kev` | 1623 `vulnerability.known_exploited` | `succeeded`, no retry error | completed | `collector-readiness` = `partial` | source-truth proven |
| FIRST EPSS | `vuln-intel://first/epss` | 1 `vulnerability.epss_score` | `succeeded`, no retry error | completed | `collector-readiness` = `partial` | source-truth proven |
| OSV | `vuln-intel://osv/maven/log4j-core` | 14 `vulnerability.affected_package` + 8 `vulnerability.cve` | `succeeded`, no retry error | completed | `collector-readiness` = `partial` | source-truth proven |
| NVD | `vuln-intel://nvd/cve` | 395 `vulnerability.affected_product` (+215 `vulnerability.reference`) | `succeeded` key-gated (anonymous tier) | completed | `collector-readiness` = `partial` | source-truth proven |
| `vulnerability.source_snapshot` | all four sources | 4 (one per source) | surfaced via `vulnerability_source_states` | n/a | readiness API | source-truth proven |
| CVE → impact end-to-end | CVE-2021-44228 | 0 published findings | n/a | n/a | impact read returns no finding | **not yet** — owned-package evidence required |

**Reading:** live source collection from all four feeds is proven, with
per-source freshness/retry state, fact counts, source snapshots, and clean
work-item drain. The lane's `collector-readiness` promotion state is **`partial`,
not `implemented`**: a *published* CVE→impact finding requires owned
package/manifest/lockfile/image/SBOM evidence for the affected package, which a
source-only stack does not carry. The impact read therefore returns no finding —
the correct, honest outcome (Eshu does not promote from advisory truth alone).
Flipping the lane to `implemented` requires re-running with owned-package
evidence (a repository declaring `org.apache.logging.log4j:log4j-core` 2.14.1)
so the OSV advisory joins it into a published impact finding. Until that
end-to-end run is recorded, the lane stays remote-E2E-gated.

## Maintainer Details

Implementation details live with the owning packages:

- `go/internal/collector/README.md`
- `go/internal/workflow/README.md`
- `go/internal/reducer/README.md`
- `go/internal/query/README.md`
- `go/internal/storage/postgres/README.md`
