# ADR: CI/CD Run Collector

**Date:** 2026-05-15
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- Issue: `#17`
- `2026-04-19-ci-cd-relationship-parity-across-delivery-families.md`
- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-05-10-oci-container-registry-collector.md`
- `2026-05-15-sbom-attestation-collector.md`
- `2026-05-15-vulnerability-intelligence-collector.md`

---

## Context

Eshu already parses CI/CD configuration from Git. That gives source-code truth:
workflow files, Jenkinsfiles, reusable workflow calls, action references,
shared-library hints, deployment commands, and controller-family evidence.

Issue `#17` asks for runtime CI/CD truth: what actually ran, which commit it
used, which jobs and steps executed, which artifacts were produced, whether an
environment or deploy gate was touched, and where the delivery path stalled.

The new collector must not turn CI success into deployment truth by itself. A
successful pipeline can build, test, scan, or publish without deploying
anything. Canonical deploy-path correlation belongs in reducers, where CI run
facts can be joined with Git, OCI image digests, SBOM/provenance, package
registry, cloud, Kubernetes, Terraform-state, and service/workload evidence.

This ADR is design-only. Runtime implementation should wait until the current
AWS, OCI, package-registry, SBOM, and vulnerability-intelligence collector
work has a stable deployment proof lane. Fixture-backed parser and reducer
tests can start earlier, but hosted collection from live CI providers must be
gated by operator-visible credentials, rate-limit budgets, and redaction proof.

## Source References

This ADR was checked against the current public contracts for the first source
set:

- GitHub Actions workflow syntax:
  <https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax>
- GitHub Actions workflow runs API:
  <https://docs.github.com/en/rest/actions/workflow-runs>
- GitHub Actions workflow jobs API:
  <https://docs.github.com/en/rest/actions/workflow-jobs>
- GitHub Actions artifacts API:
  <https://docs.github.com/en/rest/actions/artifacts>
- GitLab CI/CD YAML syntax:
  <https://docs.gitlab.com/ci/yaml/>
- GitLab pipelines API:
  <https://docs.gitlab.com/api/pipelines/>
- GitLab jobs API:
  <https://docs.gitlab.com/api/jobs/>
- Jenkins Remote Access API:
  <https://www.jenkins.io/doc/book/using/remote-access-api/>
- Jenkins Pipeline syntax:
  <https://www.jenkins.io/doc/book/pipeline/syntax/>
- Buildkite pipeline documentation:
  <https://buildkite.com/docs/pipelines>
- Buildkite builds API:
  <https://buildkite.com/docs/apis/rest-api/builds>
- Buildkite jobs API:
  <https://buildkite.com/docs/apis/rest-api/jobs>

## Source Contracts

The first implementation must preserve provider-native IDs and retry semantics.

| Source | Source truth | Contract notes |
| --- | --- | --- |
| GitHub Actions | Workflow definitions, workflow runs, jobs, attempts, artifacts, actors, refs, SHAs, environments, and conclusions | `run_id` plus `run_attempt` is the run generation. Jobs belong to one attempt. Artifacts are metadata only unless explicit opt-in enables payload fetch. |
| GitLab CI/CD | Pipeline configuration, pipelines, jobs, bridges, child pipelines, artifacts, environments, refs, SHAs, status, and source | Includes and dynamic rules are evaluated when a pipeline is created. Runtime pipeline facts must preserve the expanded run, not just repo-local YAML. |
| Jenkins | Job/build API records, multibranch pipeline records, Jenkinsfile-backed builds, stages where exposed, parameters, causes, artifacts, and upstream/downstream links | Jenkins is plugin-shaped. Missing stage metadata is a partial provider capability, not proof that no stages ran. |
| Buildkite | Pipeline definitions, builds, jobs, dynamic pipeline uploads, block/input steps, trigger steps, artifacts, env metadata, commit/ref/branch, and state | Dynamic pipelines can add steps at runtime, so Buildkite run facts must record uploaded/expanded jobs separately from static YAML. |

## Decision

Add a future collector family named `ci_cd_run`.

The collector owns:

- discovering configured CI provider scopes
- fetching run, build, pipeline, job, step, trigger, environment, and artifact
  metadata
- parsing provider configuration snapshots when the provider exposes the
  expanded run definition
- preserving provider-native IDs, attempts, retries, skipped/manual states, and
  partial API coverage
- redacting secrets from parameters, variables, environment metadata, artifact
  names, URLs, and error payloads
- emitting typed source facts and warning facts

The collector does not own:

- repository sync or static source parsing
- canonical graph writes
- deployable-unit or workload admission
- artifact-to-image, SBOM, package, vulnerability, cloud, or Kubernetes joins
- log ingestion by default
- policy enforcement such as "only deploy from successful CI"

Reducers own cross-source correlation and canonical answer shaping.

## Scope And Generation Model

The acceptance unit is a provider run/build/pipeline, not a repository.

Suggested scope IDs:

```text
github-actions://<host>/<owner>/<repo>/<workflow-id-or-path>
gitlab-ci://<host>/<project-id-or-path>
jenkins://<controller-id>/<job-full-name>
buildkite://<org-slug>/<pipeline-slug>
```

Suggested generation IDs:

- GitHub Actions: `<run_id>:<run_attempt>`.
- GitLab CI/CD: `<pipeline_id>`, with retried jobs preserved as child facts.
- Jenkins: `<controller_id>:<job_full_name>:<build_number>`.
- Buildkite: `<build_uuid>`.

If a provider exposes both a numeric ID and immutable UUID, facts should carry
both. The generation key should use the provider value that is stable across
pagination and detail endpoints.

Collection windows must be incremental. Normal operation should fetch new or
changed runs by provider cursor, updated timestamp, run ID, or webhook trigger.
Full historical scans are an operator-requested backfill path, not the default
freshness model.

## Fact Families

Initial fact kinds should use `collector_kind=ci_cd_run`.

| Fact kind | Purpose |
| --- | --- |
| `ci.pipeline_definition` | One provider definition or expanded runtime definition with workflow path, jobs/stages/steps, includes, reusable workflows, dynamic uploads, trigger rules, services, containers, called actions/plugins/scripts, and source refs. |
| `ci.run` | One run/build/pipeline with provider ID, URL, event/source, actor, branch/ref, commit SHA, status, result, start/end timestamps, attempt/retry metadata, and cancellation reason where available. |
| `ci.job` | One job/stage with provider ID, name, stage/group, runner/agent labels, queue time, status, result, start/end timestamps, matrix/parallel metadata, allow-failure/manual/block state, and attempt metadata. |
| `ci.step` | One ordered step or command with name, action/plugin/script reference, status, result, timing, shell/container hints, artifact/log references, and bounded deployment hints. |
| `ci.artifact` | One artifact/report/log pointer with provider ID, name/type, size, digest when exposed, expiration, source job, and download URL redacted or tokenless. |
| `ci.trigger_edge` | One upstream/downstream relation: reusable workflow call, GitLab child pipeline or bridge, Jenkins build trigger, Buildkite trigger step, or provider webhook cause. |
| `ci.environment_observation` | One provider environment/deployment observation with environment name, URL when authoritative, protected/manual gate, reviewer/approval status where exposed, and provider deployment status. |
| `ci.warning` | Rate limit, auth denial, partial provider capability, missing expanded config, log skipped, artifact payload skipped, malformed API response, redaction event, or unsupported provider feature. |

`source_confidence` should use:

- `observed` for static workflow/config files read from Git by the Git
  collector.
- `reported` for provider API run/job/artifact facts.
- `derived` only for normalized helper rows computed entirely from stored CI
  facts.

## Identity And Correlation Rules

Provider-native IDs are mandatory and must not be replaced by display names.

Rules:

1. Run-to-repo correlation requires provider repo locator plus commit SHA or an
   explicit source checkout reference.
2. Job and step facts attach to one run generation and one attempt. Retries do
   not overwrite earlier attempts.
3. Artifact facts attach to the producing job. They become image, package,
   SBOM, test-report, or release evidence only when the artifact type or
   content metadata provides an explicit anchor.
4. Environment observations are evidence that CI touched a named environment.
   They are not workload deployment proof until reducer-owned anchors match.
5. Shell text can produce candidate hints, but cannot create canonical
   deployment edges by itself.
6. Webhook or event-triggered refresh must be idempotent under duplicate
   delivery.

## Reducer Correlation Contract

Reducers should admit canonical relationships only when the evidence path is
explicit:

```text
CI provider run
  -> provider repo locator + commit SHA
  -> Git repository generation
  -> artifact digest, environment observation, or trigger edge
  -> OCI/SBOM/package/cloud/Kubernetes/Terraform evidence
  -> canonical deployable-unit, workload, release, or vulnerability impact answer
```

Candidate outcomes:

| Outcome | Meaning |
| --- | --- |
| `exact` | Provider repo, commit, artifact digest, or environment anchor matched one canonical target. |
| `derived` | A stable provider fact and Eshu-owned rule produced a bounded join, such as run-to-static workflow definition by workflow path and SHA. |
| `ambiguous` | More than one target matched, or provider metadata lacked enough identity to choose. |
| `unresolved` | The run is valid evidence but cannot be attached to current graph truth. |
| `rejected` | A shell-only, stale, mismatched, or unsafe signal was suppressed. |

CI run facts should help answer "what happened after this commit?" They should
not hide uncertainty when a provider says a pipeline succeeded but no artifact,
environment, deployment, or workload anchor exists.

## Freshness And Backfill

Normal freshness should use provider-native incremental mechanisms:

- provider webhooks where configured
- updated-since or created-since windows
- provider pagination cursors
- run/build IDs observed from release, artifact, or deployment systems
- scheduled reconciliation with small lookback overlap

Every provider should support a bounded backfill mode with explicit limits:
maximum runs, maximum pages, maximum age, maximum artifacts per run, and request
budget. Budget exhaustion emits partial-generation warnings and must keep
readiness from claiming complete coverage for that scope.

## Query And MCP Contract

Future read surfaces should be bounded and run-first:

- list recent runs for a repo, workflow, commit, or environment
- explain why a commit has or has not reached an environment
- show which run produced an image digest or release artifact
- show upstream/downstream pipeline fan-out
- show failed, blocked, skipped, or manual gates for a deploy path
- show CI evidence that supports a vulnerability-impact or provenance answer

Responses must include provider, scope ID, generation ID, source freshness,
truth/confidence label, deterministic ordering, `limit`, and `truncated`.
Normal use must not require raw Cypher.

## Observability Requirements

The hosted runtime must expose:

- provider API request counts by provider, operation, and bounded result
- rate-limit/throttle counts by provider and operation
- page and run counts scanned by provider
- run/job/step/artifact facts emitted by provider and fact kind
- parse and normalization failures by provider and error class
- partial generation counts by provider and reason
- redaction counts by provider and field family
- source freshness lag by provider and scope
- collector claim duration, processing duration, and retry/dead-letter counts
- reducer admission outcomes for run, artifact, environment, and trigger
  correlations

Spans should cover scope discovery, run listing, run detail fetch, job fetch,
artifact metadata fetch, definition parse, fact batch emission, and reducer
correlation.

Metric labels must not include repository names, job names, workflow names,
branch names, commit SHAs, artifact names, environment names, URLs, runner
labels, actor names, parameters, or credential references. Those values belong
in facts, spans, or structured logs with redaction.

## Security And Privacy

CI systems routinely expose secrets by accident through parameters, logs,
artifact names, environment variables, webhook payloads, failure messages, and
download URLs.

Rules:

- Full logs are opt-in and redacted before storage.
- Artifact payload download is opt-in; default collection stores metadata only.
- Token-bearing URLs are stripped or replaced with tokenless provider handles.
- Environment variables and parameters are redacted unless allowlisted.
- Provider credentials must be read-only and scoped to metadata collection.
- Status output must not include secrets, raw URLs with tokens, internal runner
  hostnames, private artifact paths, or full job scripts.

## Implementation Gate

The first implementation should be split into small PRs:

1. Fact contracts, fixtures, and parser/normalizer tests.
2. GitHub Actions fixture-backed collector with no hosted runtime.
3. GitLab fixture-backed collector and child-pipeline coverage.
4. Jenkins fixture-backed collector with partial-capability warnings.
5. Buildkite fixture-backed collector with dynamic pipeline coverage.
6. Reducer correlation tests for exact, derived, ambiguous, unresolved, and
   rejected outcomes.
7. Hosted runtime with health, readiness, metrics, status, credentials,
   redaction, request budgets, and ServiceMonitor proof.

Implementation must not start by adding graph writes or query shortcuts. The
facts and reducer contracts must be proven first.

### Reducer Read-Model Slice (#389)

The first reducer slice adds `DomainCICDRunCorrelation` without a hosted CI/CD
collector runtime and without graph writes. The reducer consumes fixture-backed
`ci.run`, `ci.artifact`, `ci.environment_observation`, `ci.trigger_edge`, and
`ci.step` facts, joins artifact digests to active
`reducer_container_image_identity` facts, and writes
`reducer_ci_cd_run_correlation` facts for exact, derived, ambiguous,
unresolved, and rejected outcomes. Exact canonical writes require one explicit
artifact digest to match one container-image identity row. Environment-only
evidence, CI success, and shell-only deployment hints do not become deployment
truth.

Performance Impact: the reducer loads only CI fact kinds for the intent scope,
extracts artifact digests, then loads active container-image identity rows for
that digest allowlist. The API/MCP read model requires a bounded anchor
(`scope_id`, `repository_id`, `commit_sha`, `provider_run_id`, `run_id`,
`artifact_digest`, or `environment`), requires `limit`, orders by `fact_id`,
and reads `limit + 1` rows for pagination. Scope reads use the existing
scope/generation/fact-kind index; repository, run, commit, artifact-digest, and
environment reads each have partial reducer-fact indexes. Expected cardinality
is one run generation plus its jobs/artifacts/environments and the matching
digest rows, not the full fact table. Stop threshold: any focused package test
that shows an unbounded fact scan or a read path without an index-backed anchor
blocks merge.

No-Regression Evidence: focused reducer, query, MCP, telemetry, storage, API,
and reducer command tests cover exact artifact-image admission, derived
environment evidence, ambiguous digest matches, unresolved missing anchors,
rejected shell-only hints, default domain wiring, bounded API/MCP filters,
OpenAPI/capability matrix coverage, and schema index registration:
`go test ./internal/reducer -run 'TestBuildCICDRunCorrelationDecisions|TestCICDRunCorrelationHandler|TestPostgresCICDRunCorrelationWriter|TestImplementedDefaultDomainDefinitions|TestNewDefaultRegistry' -count=1`;
`go test ./internal/query -run 'TestOpenAPISpecIncludesCICDRunCorrelations|TestCICDListRunCorrelations|TestCICDRunCorrelationQuery|TestCapabilityMatrixMatchesYAMLContract' -count=1`;
`go test ./internal/mcp -run 'TestMCPToolContractMatrixCoversReadOnlyTools|TestResolveRouteMapsCICDRunCorrelationsToBoundedQuery|TestReadOnlyTools|TestHandleHTTPMessage_ToolsList|TestReadOnlyToolsDoNotUseTopLevelComposition' -count=1`;
`go test ./internal/storage/postgres -run 'TestBootstrapDefinitionsIncludeCICDRunCorrelationFactIndexes' -count=1`;
`go test ./internal/telemetry -run 'TestSpanNames|TestMetricDimensionKeys' -count=1`;
`go test ./cmd/reducer ./cmd/api -count=1`.

Observability Evidence: `eshu_dp_ci_cd_run_correlations_total` emits bounded
`domain` and `outcome` dimensions for exact, derived, ambiguous, unresolved,
and rejected reducer decisions. `query.ci_cd_run_correlations` spans wrap the
API route, and the existing instrumented Postgres query path reports
`eshu_dp_postgres_query_duration_seconds` for active reducer fact reads.

## Acceptance Criteria

- Fixtures cover GitHub Actions, GitLab CI/CD, Jenkins, and Buildkite run/job
  extraction.
- Duplicate collection is idempotent.
- Retries and attempts remain separate.
- Partial API failures produce partial-generation warnings and block complete
  coverage publication for that scope.
- Reducer tests cover exact, derived, ambiguous, unresolved, and rejected
  deployment correlation.
- Artifact-to-image correlation requires an explicit digest or registry anchor.
- Environment observations do not become workload truth without corroborating
  deployment evidence.
- Full log ingestion remains disabled by default and redaction-tested.
- Hosted runtime proof includes request-budget, rate-limit, health, readiness,
  metrics, admin/status, and ServiceMonitor evidence before production use.

## Rejected Alternatives

### Treat CI Success As Deployment Truth

Rejected. CI success is useful evidence, but it does not prove an artifact was
deployed or a workload changed. Reducers need corroborating evidence.

### Ingest Full Logs By Default

Rejected. Logs are high-volume and secret-prone. Default collection should use
metadata and explicit artifact/log handles.

### Reuse The Git Collector Runtime

Rejected. Static workflow files and provider runtime facts have different
scope, freshness, credential, pagination, and failure models.

### Use Repository As The Acceptance Unit

Rejected. One repository can have many workflows, child pipelines, matrix jobs,
multibranch builds, and external triggers. The provider run/build/pipeline is
the bounded unit that retries and freshness can reason about correctly.
