# Production validation: variable lookup

Validation-Slug: prod-variable-lookup
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-7 ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-postrebase2.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_search.variable_lookup returned four Variable-labeled matches through the deployed MCP find_code tool and the deployed HTTP code-search route.

## Observed result

The fresh Compose run rebuilt the binaries, indexed the 30-repository corpus,
drained all work, and searched for the synthetic `app` binding through both
MCP `find_code` and `POST /api/v0/code/search`. Each surface returned four
matches and satisfied the `Variable` label pin in
`testdata/golden/e2e-20repo-snapshot.json`. The independent exact-search
overflow assertion also remained green. The complete gate finished with 532
passes, zero required failures, and zero advisory warnings.

## Local-authoritative profile cross-check

The same capability was rerun against a fresh real graph stack with the query
profile set to `local_authoritative`:

```bash
ESHU_QUERY_PROFILE=local_authoritative GATE_COMPOSE_PROJECT=eshu-5681-local-authoritative-20260808-4 ESHU_POSTGRES_PORT=37542 NEO4J_BOLT_PORT=37687 NEO4J_HTTP_PORT=37474 GATE_API_PORT=37080 GATE_MCP_PORT=37091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-local-authoritative-postrebase2.log 2>&1; echo $?
```

Captured output: `0`. The log records `query profile: local_authoritative`.
MCP and HTTP again returned four Variable-labeled matches; the full gate
finished in 118 seconds with 532 passes, zero required failures, and zero
advisory warnings.
