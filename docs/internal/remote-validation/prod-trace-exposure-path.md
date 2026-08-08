# Production validation: trace exposure path

Validation-Slug: prod-trace-exposure-path
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-5 ESHU_POSTGRES_PORT=31542 NEO4J_BOLT_PORT=31687 NEO4J_HTTP_PORT=31474 GATE_API_PORT=31080 GATE_MCP_PORT=31091 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-final.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_to_cloud.trace_exposure_path resolved the deployed MCP source as an HTTP handler and returned an explicit bounded unresolved result when no materialized cloud-sink bridge existed.

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
