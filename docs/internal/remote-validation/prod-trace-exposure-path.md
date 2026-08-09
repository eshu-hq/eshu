# Production validation: trace exposure path

Validation-Slug: prod-trace-exposure-path
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-7 ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-postrebase2.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_to_cloud.trace_exposure_path resolved the deployed MCP source as an HTTP handler and returned an explicit bounded unresolved result when no materialized cloud-sink bridge existed.
B12-Assertion: code_to_cloud.trace_exposure_path -> mcp:trace_exposure_path

## Observed result

The fresh Compose run rebuilt the binaries, projected the 30-repository corpus,
drained all work, and queried `trace_exposure_path` through MCP for the
synthetic `list_orders` handler. The response identified
`source_kind=http_handler`, reported `truth_label=derived`, enforced maximum
depth four, returned zero paths, and supplied a non-empty unresolved reason.
Those values are pinned in `testdata/golden/e2e-20repo-snapshot.json`. The
complete gate finished with 532 passes, zero required failures, and zero
advisory warnings.

This is the capability's documented honest negative state: it proves source
resolution and bounded traversal without fabricating an exposure path when the
required bridge evidence is absent.

## Local-authoritative profile cross-check

The same capability was rerun against a fresh real graph stack with the query
profile set to `local_authoritative`:

```bash
ESHU_QUERY_PROFILE=local_authoritative GATE_COMPOSE_PROJECT=eshu-5681-local-authoritative-20260808-4 ESHU_POSTGRES_PORT=37542 NEO4J_BOLT_PORT=37687 NEO4J_HTTP_PORT=37474 GATE_API_PORT=37080 GATE_MCP_PORT=37091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-local-authoritative-postrebase2.log 2>&1; echo $?
```

Captured output: `0`. The log records `query profile: local_authoritative`.
The real graph-backed route resolved `list_orders`, enforced depth four,
returned zero paths, and supplied the explicit unresolved reason. The full gate
finished in 118 seconds with 532 passes, zero required failures, and zero
advisory warnings.
