# prod-hosted-governance-status — production validation

Validation-Slug: prod-hosted-governance-status
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: hosted_governance.status passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `hosted_governance.status` (tool `get_hosted_governance_status`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: runtime_status`, `p95_latency_ms: 100`,
`max_truth_level: derived`.

## Claim validated

Deployed governance status exposes only safe mode, state, hash, readiness
booleans, aggregate counts, and low-cardinality reason codes — never raw
policy, source, credential, provider endpoint, prompt, or response data.

## Committed reproducible evidence

**Handler contract, redaction, and audit aggregates** —
`go/internal/query/status_governance_test.go`:
`TestStatusHandlerGovernanceLocalNoPolicyReturnsEnvelope`,
`TestStatusHandlerGovernanceEnforcingReportsSafeAggregates`,
`TestGovernanceStatusConfigDropsUnsafeStatusValues`, and
`TestGovernanceStatusReportsAuditAggregates`. Reproduce:

```bash
cd go && go test ./internal/query -run 'GovernanceStatus|StatusHandlerGovernance' -count=1
```

**Route authorization** —
`go/internal/query/auth_governance_status_test.go` and
`go/internal/mcp/dispatch_governance_status_authz_test.go` prove scoped-token
route admission for the hosted-governance-status surface on both HTTP and
MCP.

**Dedicated remote Compose proof gate** —
`scripts/verify-hosted-governance-remote-compose-proof.sh` composes the local
hosted-governance proof, API/MCP parity prerequisites, denied/out-of-scope
read canaries, and a remote Compose render-shape check; `--runtime` runs the
live two-team scoped cross-scope denial proof
(`scripts/run-two-team-governance-proof.sh`) and the live remote Compose
runtime-state proof (`scripts/verify_remote_e2e_runtime_state.sh`) against an
operator-started stack. Reproduce (list steps without running, requires no
runtime):

```bash
scripts/verify-hosted-governance-remote-compose-proof.sh --list
```

## Notes

No private data: the status surface is redacted by contract (no raw policy,
credential, or provider data), and cited tests use synthetic governance
configuration fixtures.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
