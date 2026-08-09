# prod-security-alert-reconciliations — production validation

Validation-Slug: prod-security-alert-reconciliations
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.security_alert_reconciliations.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: supply_chain.security_alert_reconciliations.list -> mcp:list_security_alert_reconciliations

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `supply_chain.security_alert_reconciliations.list` (tool
`list_security_alert_reconciliations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: repository_provider_package_or_advisory_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded provider alert reconciliation lookup anchored by repository, provider,
package, CVE, or GHSA; provider state and reconciliation status filter
anchored pages only.

## Committed reproducible evidence

**Bounded lookup, provider/Eshu state separation, and coverage gaps** —
`go/internal/query/supply_chain_security_alerts_test.go`:
`TestSupplyChainListSecurityAlertReconciliationsRequiresScopeAndLimit`,
`TestPostgresSecurityAlertReconciliationRejectsFilterOnlyStateOrStatus`,
`TestSupplyChainListSecurityAlertReconciliationsSeparatesProviderAndEshuState`,
`TestSupplyChainListSecurityAlertReconciliationsSurfacesIncompleteProviderCoverage`,
`TestPostgresSecurityAlertReconciliationQueryShape`, and
`TestSecurityAlertProviderRepositoryScopesQueryIsExactAndBounded`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainListSecurityAlertReconciliations -count=1
cd go && go test ./internal/query -run 'TestPostgresSecurityAlertReconciliation|TestSecurityAlertProviderRepositoryScopesQueryIsExactAndBounded' -count=1
```

**Triage detail decoding** —
`go/internal/query/security_alert_reconciliation_triage_test.go`:
`TestDecodeSecurityAlertReconciliationRowPreservesTriageDetails` and
`TestSupplyChainListSecurityAlertReconciliationsSurfacesTriageDetails`.
Reproduce:

```bash
cd go && go test ./internal/query -run 'TestDecodeSecurityAlertReconciliationRowPreservesTriageDetails|TestSupplyChainListSecurityAlertReconciliationsSurfacesTriageDetails' -count=1
```

**Deployed-services target-story readback** —
`scripts/verify_remote_e2e_target_story.sh` (via
`scripts/lib/remote_e2e_security_alerts.sh`) asserts
`security_alert_reconciliations` counts and provider/repository anchor matches
against a live deployed stack. `scripts/test-verify-remote-e2e-target-story.sh`
is the script's own local proof: it drives the count-matching and triage-field
logic against the fixture at
`scripts/lib/test-verify-remote-e2e-target-story-security-alert-count.json`
without live credentials. Reproduce the local proof:

```bash
scripts/test-verify-remote-e2e-target-story.sh
```

## Notes

No private data: cited evidence covers repository/provider/package/CVE anchors
only, never raw provider alert bodies or credentials.

Related: #5552 (burn-down).
