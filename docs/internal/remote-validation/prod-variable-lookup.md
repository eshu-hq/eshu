# Production validation: variable lookup

Validation-Slug: prod-variable-lookup
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-5 ESHU_POSTGRES_PORT=31542 NEO4J_BOLT_PORT=31687 NEO4J_HTTP_PORT=31474 GATE_API_PORT=31080 GATE_MCP_PORT=31091 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-final.log 2>&1; echo $?
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
