# prod-admission-decisions — production validation

Validation-Slug: prod-admission-decisions
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: admission_decisions.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `admission_decisions.list` (tools `list_admission_decisions`,
`export_deployable_unit_packet`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: scope_generation`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded, scoped-token-safe readback of reducer-owned correlation admission decisions —
domain, scope_id, generation_id, optional state, and optional anchor filters — returning
source handles and recommended next calls, with evidence capped per decision.

## Committed reproducible evidence

**Bounded readback, filters, and evidence capping** —
`go/internal/query/admission_decisions_test.go`:
`TestAdmissionDecisionHandlerReturnsBoundedStatesAndNextCalls` (bounded Postgres readback with
source handles and next-call recommendations), `TestAdmissionDecisionHandlerFiltersStateAndReturnsEmpty`,
`TestAdmissionDecisionHandlerRejectsUnboundedOrInvalidFilters`, and
`go/internal/query/admission_decisions_bounds_test.go`:
`TestAdmissionDecisionHandlerCapsIncludedEvidencePerDecision`. Reproduce:

```bash
cd go && go test ./internal/query -run TestAdmissionDecisionHandler -count=1
```

**Scoped-token safety** — `TestAdmissionDecisionScopedEmptyGrantReturnsEmptyWithoutStoreRead` and
`TestAdmissionDecisionScopedOutOfGrantReturnsEmptyWithoutStoreRead` (same file) prove empty or
out-of-grant scoped tokens short-circuit before any store read, and
`TestAuthMiddlewareWithScopedTokensAllowsAdmissionDecisionRoute` proves the route is on the
scoped-token allowlist. `admission_decisions_bounds_test.go`:
`TestAdmissionDecisionUnsupportedProfileReturnsContractErrorBeforeStoreRead` proves the
lightweight profile fails closed. Reproduce:

```bash
cd go && go test ./internal/query -run TestAdmissionDecision -count=1
```

**Contract declaration** — `go/internal/query/openapi_admission_decisions_test.go`:
`TestOpenAPISpecIncludesAdmissionDecisions`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPISpecIncludesAdmissionDecisions -count=1
```

## Notes

No private data: the tests above exercise fixture generation/scope IDs and fake stores only,
never real deployment identifiers, hostnames, or credentials.

Related: #5552 (burn-down).
