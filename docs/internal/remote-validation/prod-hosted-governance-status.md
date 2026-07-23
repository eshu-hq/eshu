# prod-hosted-governance-status — production validation

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
