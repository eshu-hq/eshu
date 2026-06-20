# Investigation Workflows

A guided investigation workflow is a deterministic, versioned catalog entry that
helps assistants start with a high-value workflow before selecting low-level MCP
tools. It is workflow-plan truth: it lists the input shape, required evidence,
optional evidence, expected output packet, grouped atomic tools, starter prompts,
and missing-evidence routes. It does not execute tools, read Postgres, read the
graph backend, call providers, or expose raw Cypher.

Use query playbooks when you need a fixed ordered call sequence. Use
investigation workflows when a prior answer or operator observation has already
identified missing evidence and the assistant needs the next bounded tool call.

## Catalog

The first catalog has three workflow families:

| ID | Output packet | Required evidence | Optional evidence |
| --- | --- | --- | --- |
| `guided_vulnerable_dependency` | `guided-vulnerable-dependency.v1` | advisory, package, impact | SBOM, workload, freshness |
| `guided_deployable_drift` | `guided-deployable-drift.v1` | admission, deployment config, runtime | service, freshness |
| `guided_incident_context` | `guided-incident-context.v1` | incident, service, changes | observability, runtime |

Each workflow groups existing atomic tools behind the workflow. Expert callers
can still call the atomic tools directly; assistants should list or resolve the
workflow first so they do not skip bounds, truth labels, or missing-evidence
handling.

`guided_deployable_drift` requires `deployable_unit_id`, `scope_id`, and
`generation_id` because admission decisions are reducer-scoped by domain, scope,
and generation. Runtime drift drilldowns reuse `scope_id` and can optionally
include `account_id` and `region`.

## Starter Prompts

Examples:

- "Investigate whether this CVE affects `repo_id`, and show the missing
  supply-chain hops before recommending remediation."
- "Explain why this deployable unit is accepted, drifted, unmanaged, rejected,
  or ambiguous, and name the next evidence calls."
- "Investigate this incident and return incident, service, observability,
  runtime, and recent-change context with missing evidence called out."

## API And MCP

| Surface | Operation | Result |
| --- | --- | --- |
| HTTP | `GET /api/v0/investigation-workflows` | Lists workflow IDs, versions, input shapes, evidence families, output packets, grouped tools, starter prompts, and missing-evidence routes. |
| HTTP | `POST /api/v0/investigation-workflows/resolve` | Resolves `workflow_id`, declared string inputs, and `missing_evidence[]` into bounded `recommended_next_calls`. |
| MCP | `list_investigation_workflows` | Dispatches to the HTTP catalog route and returns the canonical envelope as structured content. |
| MCP | `resolve_investigation_workflow` | Dispatches to the HTTP resolver with `workflow_id`, `inputs`, and `missing_evidence`. |

Both routes report `query.investigation_workflows` truth with `runtime_state`
basis because they describe static workflow-plan data. Scoped-token requests may
list or resolve workflows because the handler returns catalog data only; the
underlying evidence tools keep their own scoped-route policy.

## Missing Evidence

The resolver only selects next calls from caller-provided missing evidence.
Unknown evidence keys are returned in `unmatched_missing_evidence` instead of
being guessed. Common keys include:

| Missing evidence | Typical next call |
| --- | --- |
| `advisory` | `list_advisory_evidence` |
| `package` | `list_package_registry_correlations` |
| `sbom` | `list_sbom_attestation_attachments` |
| `workload` | `list_supply_chain_impact_findings` |
| `admission` | `list_admission_decisions` |
| `deployment_config` | `investigate_deployment_config` |
| `runtime` | `list_aws_runtime_drift_findings` |
| `incident` | `get_incident_context` |
| `observability` | `list_observability_coverage_correlations` |
| `changes` | `list_ci_cd_run_correlations` |
| `freshness` | `get_generation_lifecycle` |
| `permission_hidden` | `get_hosted_governance_status` |

No-Regression Evidence: focused tests cover catalog shape, deterministic
resolution, unknown missing-evidence reporting, required-input validation,
HTTP handler envelopes, scoped-token allowlist, MCP registry, and MCP route
mapping. The aggregate static-route gate is
`cd go && go test ./internal/query ./internal/mcp ./cmd/api ./cmd/mcp-server -run 'InvestigationWorkflow|AuthMiddlewareWithScopedTokensAllowsInvestigationWorkflow|EveryRegisteredToolHasDispatchRoute|ReadOnlyTools|MCPToolContractMatrix|RuntimeSurface|Wiring' -count=1`.

Dogfood Evidence: `TestInvestigationWorkflowDogfoodRoutesFewerCallsThanAtomicOnly`
uses the three starter prompt families as bounded prompt runs. For vulnerable
dependency, deployable drift, and incident context prompts, the workflow
resolver selects only the calls needed for the supplied missing-evidence keys,
keeps expected evidence text on every recommended call, and covers every missing
evidence key while using fewer calls than the grouped atomic-tool choice space.

No-Observability-Change: list and resolve read only in-process static catalog
data and return through the existing HTTP/MCP envelope path. They do not open
graph or Postgres connections, enqueue work, execute resolved calls, start
backend read spans, or add metric labels. Operators diagnose failures through
the existing HTTP status code, canonical error envelope, MCP dispatch result,
and request metrics middleware.
