# prod-admission-decisions — production validation

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
