# prod-cloud-runtime-drift-readback — production validation

Validation-Slug: prod-cloud-runtime-drift-readback
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: cloud_runtime_drift.readback.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `cloud_runtime_drift.readback.list` (tools `list_cloud_runtime_drift_findings`,
`export_cloud_runtime_drift_packet`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: account_project_or_subscription`, `p95_latency_ms: 5000`,
`max_truth_level: derived`.

## Claim validated

Bounded paginated readback of reducer-owned `reducer_multi_cloud_runtime_drift_finding` rows
with provider, normalized identity, `finding_kind`, `management_status`, provider-neutral source
state, and refusal-safety posture (unsafe findings return `source_state=rejected` with a refused
action rather than being silently omitted).

## Committed reproducible evidence

**Provider-neutral findings listing and scope enforcement** — `go/internal/query/cloud_runtime_drift_test.go`:
`TestHandleCloudRuntimeDriftFindingsReturnsProviderNeutralFindings`,
`TestHandleCloudRuntimeDriftFindingsScopedOutOfGrantNeverCallsStore`,
`TestHandleCloudRuntimeDriftFindingsScopedInGrantReturnsRealRowData`,
`TestHandleCloudRuntimeDriftFindingsRequiresScope`,
`TestHandleCloudRuntimeDriftFindingsRejectsUnknownProvider`, and
`TestHandleCloudRuntimeDriftFindingsUnsupportedProfile`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCloudRuntimeDriftFindings -count=1
```

**Contract declaration** — `go/internal/query/openapi_cloud_runtime_drift_test.go`:
`TestOpenAPISpecIncludesCloudRuntimeDriftFindings`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPISpecIncludesCloudRuntimeDriftFindings -count=1
```

## Notes

This capability is the provider-neutral peer of the AWS-specific
`aws_runtime_drift.findings.list` capability (see `prod-aws-runtime-drift-read-model.md`), which
also cites `scripts/verify_aws_runtime_drift_compose.sh` for the deployed Compose driver over the
shared reducer drift-finding read model.

No private data: fixtures use synthetic cloud resource UIDs and provider-neutral identity
fields; no real cloud account identifiers.

Related: #5552 (burn-down).
