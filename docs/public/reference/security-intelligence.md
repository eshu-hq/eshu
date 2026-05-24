# Security Intelligence

Eshu security intelligence is a read-only evidence system. It collects facts
from code, package metadata, advisories, images, deployment state, and provider
signals, then reduces those facts into bounded findings that can explain why a
repository, package, image, service, or environment is affected.

The product goal is not "run a scanner and print whatever it says." The goal is
to prove the chain from source evidence to owned impact with enough context for
an operator or assistant to trust the answer.

## Decision

Security intelligence separates **targets** from **capabilities**.

- A target is something Eshu can observe, such as a repository dependency,
  package version, advisory, SBOM subject, container image digest, workload,
  environment, or provider-hosted alert.
- A capability is a reducer-owned question over collected evidence, such as
  vulnerability impact, readiness coverage, priority, remediation, or future
  secret/license/misconfiguration analysis.
- Collectors and scanner workers emit source facts only. They do not
  publish user-facing security truth by themselves.
- Reducers own admitted findings because reducers can see the cross-source
  evidence chain.
- A zero-finding result is meaningful only when the response also exposes
  coverage and readiness. "No finding" is not the same as "no target was
  collected."

This page is the public architecture contract for issue
[#599](https://github.com/eshu-hq/eshu/issues/599). Private validation inputs,
provider alert exports, repository names, package names, and URLs stay outside
the public repository.

## End-State Flow

```mermaid
flowchart LR
  source_targets["Security targets<br/>repos, packages, images, workloads"]
  source_collectors["Collectors and source clients<br/>read-only fact emission"]
  heavy_workers["Scanner workers<br/>bounded CPU/RAM analysis"]
  facts["Durable facts<br/>Postgres fact store"]
  readiness["Readiness model<br/>coverage and freshness"]
  reducers["Reducer capabilities<br/>impact and prioritization truth"]
  reads["API and MCP reads<br/>bounded findings and explanations"]

  source_targets --> source_collectors
  source_targets --> heavy_workers
  source_collectors --> facts
  heavy_workers --> facts
  facts --> readiness
  facts --> reducers
  readiness --> reads
  reducers --> reads
```

The first security capability is `supply_chain_impact`, Eshu's existing
vulnerability impact finding surface. Future capabilities can reuse the same
target and readiness model without changing collector ownership.

## Execution Modes

Security intelligence must work in two modes:

| Mode | User job | Runtime shape |
| --- | --- | --- |
| Hosted evidence graph | Continuously collect repositories, package metadata, advisories, images, workloads, and provider signals for an organization or team. | Normal Eshu API, MCP, ingester, reducer, coordinator, collector, Postgres, and graph services. |
| Local one-shot scan | Let a developer point the Eshu CLI at one repository and get vulnerability impact results without standing up the hosted control plane. | The CLI starts or attaches to local Eshu services, collects only the requested repository scope, fetches bounded advisory/package evidence, runs the same reducer-owned matching contract, and returns a local evidence envelope. |

The local developer experience should feel like a direct vulnerability scan
command. The initial implemented Eshu CLI shape is
`eshu vuln-scan repo [path]`. It uses an explicitly configured API when
`--service-url`, config, or `ESHU_SERVICE_URL` names one. Without a configured
API, it starts or attaches to the workspace-local authoritative service, launches
a short-lived loopback API reader attached to the same owner, runs the same local
source indexing and readiness proof as `eshu scan`, resolves the scanned
repository id, and reads reducer-owned impact findings from the bounded supply
chain impact API. It must not claim a clean result unless the scan reaches a
ready state and the impact read succeeds.

The local mode cannot be a separate truth engine. It should reuse the same
facts, target model, readiness states, matching rules, severity enrichment, and
output envelope as hosted Eshu. The main difference is scope: local mode bounds
collection to one filesystem repository and an explicit set of advisory or
package sources.

## Target Families

Security targets are evidence sources, not findings:

| Target family | Evidence Eshu may collect | Finding ownership |
| --- | --- | --- |
| Repository dependency facts | manifests, lockfiles, package ids, versions, dependency paths | Reducer joins to advisories and repository ownership. |
| Package registry metadata | package identity, version metadata, dependency metadata | Reducer treats registry data as source metadata unless owned evidence proves use. |
| Advisory sources | CVE, GHSA, OSV, GitLab Advisory Database (Gemnasium), CVSS v2/v3/v4, EPSS, KEV, CWE, affected ranges, fixed versions | Reducer joins advisories to owned packages, images, SBOMs, or workloads. Each source keeps its own fact provenance so reducers can detect cross-source disagreement on range, severity, or fixed version. |
| Provider-hosted alerts | alert state, affected dependency, advisory identifiers, manifest path | Reducer or verifier compares provider alerts to Eshu evidence without copying private alert data into docs. |
| SBOM and attestations | document subject, component inventory, verification and parse status | Reducer admits impact only when the subject is tied to an owned image, repository, or workload. |
| Container images | digest, repository, tags, config, observed runtime references | Reducer keeps digest identity separate from weak or stale tag observations. |
| Workloads and cloud/runtime state | deployment targets, images in use, service and environment evidence | Reducer connects package/image impact to deployed context only through explicit evidence. |

## Capability Families

Capabilities run over targets:

- `supply_chain_impact`: determine affected, possibly affected, known-fixed,
  unknown, and missing-evidence states for vulnerability impact findings. This
  is the capability behind the current supply-chain impact API and MCP reads.
- `coverage_readiness`: explain which target families were collected,
  skipped, stale, unsupported, or incomplete.
- `priority`: combine severity, exploitability, known exploitation, runtime
  exposure, ownership, and deployment evidence.
- `remediation`: recommend fixed versions, dependency paths, image rebuild
  targets, or ownership handoffs when the evidence supports them.
- `export`: emit evidence-backed findings, VEX-style statements, or audit
  packets after the impact chain is proven.
- Future heavy capabilities, such as secret scanning, license scanning,
  misconfiguration analysis, and OS package scanning, must use the same target
  and readiness contract.

## Reducer And Worker Boundaries

The reducer is the truth owner, but not every security task belongs in the
default reducer process. Vulnerability matching over already-collected facts can
start as a reducer capability. CPU-heavy or memory-heavy extraction must move
behind claim-driven scanner workers so repository indexing and normal reducer
projection stay healthy.

```mermaid
flowchart TB
  coordinator["Workflow coordinator"]
  local_cli["Local CLI one-shot"]
  default_lane["Default reducer lane<br/>normal graph and read models"]
  security_lane["Security reducer lane<br/>bounded matching and impact"]
  scanner_lane["Scanner worker lane<br/>SBOM, image, or source analysis"]
  fact_store["Fact store"]
  read_models["Read models and graph"]

  coordinator --> default_lane
  coordinator --> security_lane
  coordinator --> scanner_lane
  local_cli --> fact_store
  local_cli --> security_lane
  scanner_lane --> fact_store
  default_lane --> read_models
  security_lane --> read_models
  fact_store --> security_lane
```

Scaling rules:

- Add security-specific reducer lanes when matching work contends with normal
  graph projection.
- Add scanner workers when the work unpacks images, scans large source trees,
  creates SBOMs, or needs analyzer-specific CPU and memory limits.
- Do not hide non-idempotent writes by lowering worker counts. Fix the
  ownership or concurrency model first.
- Do not raise memory blindly. Use pprof, queue age, per-domain duration,
  retry counts, dead-letter counts, and target cardinality to decide where the
  bottleneck lives.

## Scanner-Worker Boundary

Scanner workers are a claim-driven isolation boundary for CPU-heavy or
memory-heavy security analysis. They do not replace reducers, and they do not
publish user-facing findings. They take one bounded claim, run an analyzer
inside explicit resource limits, and emit source facts back to the normal fact
store.

Current contract flow:

```mermaid
flowchart LR
  coordinator["Workflow coordinator<br/>plans scanner_worker work item"]
  workflow["workflow.WorkItem + workflow.Claim<br/>claim id and fencing token"]
  scanner_contract["scannerworker.ClaimInput<br/>target scope + resource limits"]
  analyzer["isolated analyzer process<br/>CPU and memory bounded"]
  source_facts["scanner_worker.* source facts<br/>fenced fact envelopes"]
  fact_store["Postgres fact store"]
  reducer["reducers<br/>finding admission and priority truth"]
  reads["API and MCP reads<br/>findings + readiness"]
  telemetry["OTEL metrics and spans<br/>queue age, duration, CPU, memory"]

  coordinator --> workflow
  workflow --> scanner_contract
  scanner_contract --> analyzer
  analyzer --> source_facts
  source_facts --> fact_store
  fact_store --> reducer
  reducer --> reads
  scanner_contract --> telemetry
  analyzer --> telemetry
```

The claim input contains:

- `work_item_id`, `claim_id`, `fencing_token`, `owner_id`, `attempt`, and claim
  timestamps copied from workflow state;
- `analyzer`, which must route to the `scanner_worker` lane;
- target scope: `target_kind`, `scope_id`, `acceptance_unit_id`,
  `source_run_id`, `generation_id`, and a safe `locator_hash`;
- resource limits: CPU millicores, memory bytes, timeout, maximum input bytes,
  maximum file count, and maximum emitted fact count.

The fact output contains `target_count`, `result_count`, and a list of fenced
`facts.Envelope` source facts. A scanner worker must emit either a source fact
or an explicit warning fact for a completed claim. Silent "clean" output is not
accepted because callers could not distinguish a proven clean target from an
analyzer that produced no evidence. Reducer-owned fact kinds such as
`reducer_*_finding` are rejected at the scanner-worker boundary.

Retry and dead-letter payloads carry only bounded diagnostic fields:
`work_item_id`, `claim_id`, `fencing_token`, `analyzer`, `target_kind`,
`target_locator_hash`, `failure_class`, disposition, retryability, attempt,
CPU seconds, and peak memory bytes. They must not include raw repository paths,
image names, registry URLs, package coordinates, bucket keys, or source
locators.

Analyzer lane ownership:

| Analyzer profile | Lane | Reason |
| --- | --- | --- |
| SBOM generation | `scanner_worker` | Can read large inputs and produce many component facts. |
| Image unpacking | `scanner_worker` | CPU, disk, and memory pressure must be isolated. |
| Source analysis | `scanner_worker` | Repository-size dependent CPU and memory cost. |
| OS package extraction | `scanner_worker` | Image/rootfs extraction belongs outside reducers. |
| Secret scanning | `scanner_worker` | High-cardinality file scanning with bounded output. |
| License scanning | `scanner_worker` | Repository-wide scan that should not block reducer drains. |
| Misconfiguration scanning | `scanner_worker` | Analyzer-specific CPU and memory limits are required. |
| Vulnerability matching | `reducer` | Reducers own joins across package, advisory, image, workload, and ownership evidence. |
| Coverage readiness | `reducer` | Readiness is a truth model over collected evidence. |
| Security priority | `reducer` | Priority needs reducer-owned impact, exploitability, runtime, and ownership context. |

## Resource And Deployment Guidance

The hosted `eshu-scanner-worker` runtime isolates scanner-worker claims from
the default reducer lane. It is available in the remote Compose proof stack and
as an opt-in Helm `scannerWorker` Deployment, but it is not enabled by default
in normal Helm installs. The built-in analyzer emits an explicit
`scanner_worker.warning` source fact when no concrete analyzer is configured;
that is a proof of claimed scanner-worker execution, not a clean finding.

Starting Kubernetes resource envelopes:

| Analyzer class | Request | Limit | Contract limits to start with |
| --- | --- | --- | --- |
| Repository source analysis, secret, license, or misconfiguration scan | `cpu=1`, `memory=2Gi` | `cpu=4`, `memory=4Gi` | `cpu_millis=4000`, `memory_bytes=4294967296`, `timeout=10m`, `max_files=250000`, `max_facts=50000` |
| SBOM generation or OS package extraction | `cpu=1`, `memory=2Gi` | `cpu=4`, `memory=8Gi` | `cpu_millis=4000`, `memory_bytes=8589934592`, `timeout=10m`, `max_input_bytes=2Gi`, `max_facts=50000` |
| Image unpacking | `cpu=2`, `memory=4Gi` | `cpu=6`, `memory=12Gi` | `cpu_millis=6000`, `memory_bytes=12884901888`, `timeout=15m`, `max_input_bytes=4Gi`, `max_facts=50000` |

Use a separate worker pool per analyzer class when those envelopes diverge.
Do not co-locate scanner workers with reducers until pprof and metrics prove
the analyzer cannot contend with reducer queue drain. In Compose proofs, keep
pprof bound to host loopback. In Kubernetes, expose pprof only through a
temporary port-forward or a protected debug path, never through the public
service.

Remote Compose starts a dedicated `scanner-worker` service with separate
resource-limit env vars:

- `ESHU_SCANNER_WORKER_CPU_MILLIS`
- `ESHU_SCANNER_WORKER_MEMORY_BYTES`
- `ESHU_SCANNER_WORKER_TIMEOUT`
- `ESHU_SCANNER_WORKER_MAX_INPUT_BYTES`
- `ESHU_SCANNER_WORKER_MAX_FILES`
- `ESHU_SCANNER_WORKER_MAX_FACTS`

Helm renders the same contract from the `scannerWorker` values block. Keep the
worker disabled unless `workflowCoordinator.enabled=true`,
`workflowCoordinator.deploymentMode=active`, and
`workflowCoordinator.claimsEnabled=true`; the chart rejects a scanner-worker
Deployment without that claim control plane.

## Scanner Observability

The hosted scanner-worker service records these signals:

- counters: `eshu_dp_scanner_worker_claims_total`,
  `eshu_dp_scanner_worker_retries_total`,
  `eshu_dp_scanner_worker_dead_letters_total`,
  `eshu_dp_scanner_worker_facts_emitted_total`;
- histograms: `eshu_dp_scanner_worker_queue_wait_seconds`,
  `eshu_dp_scanner_worker_scan_duration_seconds`,
  `eshu_dp_scanner_worker_target_count`,
  `eshu_dp_scanner_worker_result_count`,
  `eshu_dp_scanner_worker_cpu_seconds`,
  `eshu_dp_scanner_worker_memory_bytes`;
- spans: `scanner_worker.claim.process`, `scanner_worker.analyze`, and
  `scanner_worker.fact.emit_batch`;
- bounded dimensions: `analyzer`, `target_kind`, `limit_kind`,
  `failure_class`, `fact_kind`, `outcome`, and `result`.

Operators should be able to answer whether the bottleneck is waiting to claim,
running the analyzer, hitting a resource limit, producing too many facts,
retrying transiently, or dead-lettering terminally without reading raw target
names.

No-Regression Evidence: scanner-worker runtime behavior is covered by
`go test ./internal/collector/scannerworker ./cmd/scanner-worker ./internal/runtime -run 'Test(Service|DefaultResourceLimits|WarningAnalyzer|LoadRuntimeConfig|ScannerWorkerBinary|RemoteE2EComposeIncludesScannerWorker|HelmClaimDrivenCollectorDeployments)' -count=1`.
That proof covers successful source fact emission, retryable analyzer failure,
terminal dead-letter payload detail, silent-clean rejection, resource-limit
defaults, runtime config selection, binary packaging, Compose service wiring,
pprof overlay wiring, and Helm rendering. Remote Compose acceptance still must
record target count, fact count, runtime, memory, CPU, queue state, retry
count, dead letters, and pprof availability before concrete analyzer rollout is
accepted.

## Readiness Semantics

Every API or MCP security answer should carry enough readiness context for the
caller to tell "clean" from "not checked."

| State | Meaning |
| --- | --- |
| `not_configured` | No target source is enabled for the requested scope. |
| `target_incomplete` | Target collection started but did not reach terminal evidence state. |
| `evidence_incomplete` | Some target evidence exists, but a required join source is missing or stale. |
| `ready_zero_findings` | Required target evidence exists and the reducer found no matching impact. |
| `ready_with_findings` | Required target evidence exists and reducer-owned findings are available. |
| `readiness_unavailable` | Out-of-band signal returned when the readiness lookup itself fails; the findings page is still returned but coverage cannot be classified. |

An `unsupported` state is reserved for a future reducer that observes target
evidence Eshu does not yet know how to match. The state is not surfaced by
the current implementation; it will be added back to the API and MCP contract
when a real producer emits unsupported-target evidence.

Zero findings without readiness are unsafe. The API and MCP surfaces should
return coverage, freshness, unsupported target counts, and missing-evidence
reasons alongside findings.

### Vulnerability Impact Readiness Envelope

`GET /api/v0/supply-chain/impact/findings` and the MCP
`list_supply_chain_impact_findings` tool both attach a `readiness` envelope to
every response. The envelope is derived from existing source-fact and
reducer-fact counts so the answer never invents findings:

- `readiness_state` is one of the five classification states above, plus the
  out-of-band `readiness_unavailable` when the readiness lookup itself fails.
- `target_scope` echoes the bounded anchors the caller used (`cve_id`,
  `package_id`, `repository_id`, `subject_digest`, `impact_status`).
  `impact_status` alone is not a fact-anchor: the readiness store skips its
  Postgres scan and returns an empty snapshot for impact_status-only
  requests, because impact_status is a reducer-finding attribute that does
  not exist on source facts. The findings page is still returned.
- `evidence_sources[]` reports per-family fact counts, `latest_observed_at`,
  and `freshness` (`fresh`, `stale`, or `unknown`) for:
  `vulnerability.advisory`, `vulnerability.exploitability`,
  `package.consumption`, `package.registry`, `sbom.component`,
  `sbom.attestation`, and `container_image.identity`. Families with zero
  facts in the requested scope are omitted so the payload reflects only what
  Eshu actually has for the caller. `package.registry` is only counted when
  the request anchors on a specific `package_id` (registry data without a
  package anchor is global metadata, not proof of repository consumption).
- `source_snapshots[]` reports vulnerability source observation/cache metadata:
  source, ecosystem, cache artifact version, snapshot digest, cache update time,
  freshness, completion state, and bounded warning fields. Raw advisory bodies,
  package names, and source URLs are not returned.
- `missing_evidence[]` names absent required join families using the stable
  identifiers `advisory_sources`, `owned_packages`, `sbom_or_image_evidence`,
  `target_collection_incomplete`, and `readiness_unavailable`. The list is
  empty on `ready_*` states so callers cannot see contradictory "ready" +
  "missing" signals.
- `incomplete_reasons[]` carries collector-emitted reasons explaining why
  source collection is still in flight; only populated when
  `readiness_state` is `target_incomplete`.
- `freshness` summarizes the worst per-family freshness as one label.
- `counts` reports `findings_returned`, `findings_truncated`,
  `findings_by_status`, and `evidence_facts_total`. `findings_returned` and
  `findings_by_status` describe the returned page only; combine with
  `truncated` to know whether more pages exist.

Readiness reflects existing facts. It does not poll collectors, dispatch
reducer work, or change finding ownership. Where evidence is missing the
envelope says so instead of guessing. `target_incomplete` and `unsupported`
specifically depend on collector/reducer-emitted source facts; when those
signals are not present, missing evidence is surfaced through
`missing_evidence` rather than being inferred from absence.

#### Proven States

The current implementation proves the following:

- `not_configured` when no advisory or owned-evidence facts exist for the
  scope.
- `evidence_incomplete` when advisory facts exist but the required join
  family for the requested anchor is missing.
- `ready_zero_findings` when advisory and required owned evidence exist and
  the reducer returned no matching impact.
- `ready_with_findings` whenever the reducer returned at least one finding.
  `missing_evidence` is cleared on ready states so the envelope cannot
  report `ready_with_findings` and `missing advisory_sources` at the same
  time.
- `target_incomplete` when a `vulnerability.source_snapshot` fact carries
  `"complete": false` AND the requested scope has no advisory evidence yet.
  An in-flight snapshot for an unrelated source does not flip a scope whose
  advisory evidence is already collected, so cross-source ingestion noise
  cannot invalidate ready answers. `incomplete_reasons` carries the distinct
  collector-emitted `warning_message` values that justify the state.
- `readiness_unavailable` when the readiness lookup itself fails. The
  findings page is still returned so callers do not lose data, but
  `missing_evidence` carries `readiness_unavailable` and the state explicitly
  warns that coverage cannot be classified for this response.

The package.consumption family is sourced from the real
`reducer_package_consumption_correlation` facts and `content_entity` manifest
dependency facts (the same `content_entity` + `entity_metadata.config_kind =
'dependency'` discriminators used by other supply-chain reducers).
`package.registry` is only counted when the request anchors on a specific
`package_id`; repository-scoped requests cannot satisfy `owned_packages`
through global registry metadata.

#### Follow-Up Work

- The `unsupported` readiness state will be reintroduced once a reducer
  emits observed-but-unmatched target evidence. The field/state was dropped
  from the current contract to avoid surfacing a verdict the
  implementation cannot produce.
- Surface per-collector freshness windows separately when the collector
  contract carries source-specific staleness thresholds.

Performance Evidence: focused query tests
`go test ./internal/query -run 'SupplyChainImpactReadiness' -count=1` exercise
not-configured, evidence-incomplete, ready-zero-findings, ready-with-findings,
target-incomplete, source-snapshot cache metadata, and the Postgres query shape
contract. The readiness Postgres path runs one bounded CTE per response with
seven anchored counts and a source-snapshot roll-up; it adds one Postgres round
trip alongside the existing impact-finding read.

No-Observability-Change: the readiness path reuses the existing
`query.supply_chain_impact_findings` span. The handler does not start a new
graph query, queue claim, or reducer write, so the existing
`eshu_dp_postgres_query_duration_seconds` histogram and the impact-findings
HTTP/MCP envelope continue to expose latency, error, and truth metadata for
the bounded readiness read. Source-cache state is observable through
`vulnerability.source_snapshot` payload fields and the `source_snapshots[]`
readiness metadata.

## Advisory Source Coverage

Eshu collects advisory source truth from OSV, FIRST EPSS, CISA KEV, NVD CVE
API 2.0, and the GitLab Advisory Database (Gemnasium). Each source is
normalized into the shared `vulnerability.*` fact contract with
`source_confidence=reported` and a source-namespaced stable fact key, so a
GLAD observation of `CVE-2026-0001` coexists with OSV, GHSA, or NVD
observations of the same CVE rather than overwriting them. Reducers join
across sources at admission time and may detect cross-source disagreement on
range, severity, or fixed version.

The GLAD adapter preserves the source `package_slug`, ecosystem, package
name, raw and structured `affected_range`, human-readable `affected_versions`
and `not_impacted` text, multiple `fixed_versions` (including prerelease and
`+build` branches), CVSS v2/v3/v4 vectors, CWE IDs, URLs, and the source
advisory UUID. Range evaluation belongs to reducers.

The GLAD adapter is parser-only. Cache and freshness lifecycle for advisory
snapshots is owned by the shared source interface in
[#603](https://github.com/eshu-hq/eshu/issues/603); the GLAD parser is pure
so the cache/freshness owner can wire it later without changing the fact
payload.

No-Regression Evidence: `go test ./internal/collector/vulnerabilityintelligence -run 'TestGitLab|TestParseGitLab|TestNewGitLab' -count=1`
covers GLAD CVE/affected_package/reference envelope construction, GMS
identifier fallback, advisory-identifier validation, package_slug validation,
source-namespaced stable keys against OSV, compact multi-branch range
parsing, prerelease and `+build` fixed-version preservation, blank/empty
constraint rejection, unsupported-operator rejection, source snapshot
generation invariance, GLAD↔OSV fixed-version disagreement,
GLAD↔NVD CVSS severity disagreement, GLAD↔OSV affected-range
disagreement, and shared CVE correlation anchors for cross-source joins.

No-Observability-Change: the GLAD adapter emits the existing
`vulnerability.cve`, `vulnerability.affected_package`,
`vulnerability.reference`, and `vulnerability.source_snapshot` fact kinds. It
adds no new metric instrument, span, log key, queue, reducer lane, graph
write, or runtime worker. Operators continue to use the existing
`vulnerability.source_snapshot` `source`/`ecosystem`/`response_digest`/
`complete` fields and the readiness envelope on the supply-chain impact API
to diagnose coverage.

## Provider Alert Parity Gate

Provider-hosted alert parity is a validation gate, not a source of public test
data. For supported hosts, private validation may compare Eshu findings against
provider alerts for the same repositories and package ecosystems.

Eshu should match provider alert counts when it has equivalent owned target
evidence and advisory data. Eshu may exceed provider alert output when it can
add code-to-cloud context, image/runtime impact, or additional advisory sources.
Any mismatch must classify whether the cause is missing target collection,
missing advisory ingestion, version-range matching, unsupported ecosystem,
provider-only behavior, or an Eshu reducer bug.

Validation logs may record aggregate counts and mismatch classes. They must not
commit private repository names, package names, alert URLs, or copied provider
payloads to the public repo.

## API And MCP Contract

Security reads must be bounded, explainable, and scoped:

- require `limit`, timeout, deterministic ordering, and `truncated` signals for
  list responses;
- require at least one anchor such as repository, package, image digest,
  advisory id, service, workload, environment, or status;
- keep findings separate from readiness and source facts;
- return evidence handles and missing-evidence reasons instead of raw full
  source payloads;
- expose exact, derived, possible, known-fixed, unknown, and unsupported states
  without collapsing them into one severity bucket.

The current vulnerability impact route is documented in
[HTTP Evidence And Supply-Chain Routes](http-api/evidence-and-supply-chain.md).

## CLI Contract

The local vulnerability scan command is a thin orchestration layer, not a
second scanner product:

- resolve one repository or workspace root using the same local source rules as
  the existing scan workflow;
- collect manifest, lockfile, package, and repository evidence through normal
  fact emitters;
- fetch only bounded advisory and package metadata required by observed owned
  packages unless the user explicitly asks for broader coverage;
- support advisory source cache refresh, offline replay, explicit mirror
  fallback, retention cleanup, and update-only source refresh without treating
  cached source data as reducer-owned findings;
- run the same vulnerability impact reducer logic used by hosted Eshu;
- return the same finding, readiness, freshness, evidence-handle, and
  missing-evidence fields as API and MCP reads;
- provide machine-readable JSON and a concise terminal summary;
- cache advisory and package metadata locally with freshness markers so repeat
  developer runs are fast without silently using stale truth;
- fail closed when required evidence cannot be collected, and show whether the
  result is incomplete instead of printing a clean report.

This keeps the developer experience simple while preserving the accuracy rule:
the CLI can be convenient, but it must not produce a result that means
something different from the hosted graph.

The current `eshu vuln-scan repo [path]` implementation covers the command
registration, local root resolution, local service attach/start when no API is
configured, scan readiness proof, repository-scoped impact read, JSON envelope,
concise terminal summary, and fail-closed incomplete target behavior.
Advisory source cache lifecycle is implemented for vulnerability-intelligence
source collection and exposed through readiness metadata. Package metadata
cache freshness and fixture-backed vulnerable/ready-zero runtime proof remain
implementation gates before this is a complete standalone vulnerability scan
workflow.

## Acceptance Gates

Security intelligence work is ready only when all applicable gates pass:

- source facts are collected with provenance and freshness;
- reducer findings require owned evidence anchors;
- readiness distinguishes zero findings from missing coverage;
- API and MCP calls are scoped, bounded, and observable;
- private provider-alert comparison matches or explains mismatches without
  committing private data;
- remote Compose proves clean-volume and preserved-volume behavior;
- Kubernetes rollout proves resource settings, pprof access, logs, queue drain,
  retries, and no dead letters;
- performance evidence records target count, fact count, queue timing,
  reducer-domain timing, memory, CPU, and stop thresholds.
