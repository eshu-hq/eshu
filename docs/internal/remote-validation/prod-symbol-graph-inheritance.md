# Production validation: class inheritance symbol graph

Validation-Slug: prod-symbol-graph-inheritance
Validation-Tier: deployed_services
Validation-Date: 2026-08-11
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu5970proof ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=1200 bash scripts/verify-golden-corpus-gate.sh --keep; echo $?
Validation-Exit-Code: 0
Capability-Assertion: symbol_graph.inheritance returned declared INHERITS edges in both directions from POST /api/v0/code/relationships, naming the resolved parent and child classes rather than literal text.
B12-Assertion: symbol_graph.inheritance -> mcp:analyze_code_relationships

## Observed result

The gate ran green (`552 pass, 0 required-fail, 0 advisory-warn`, elapsed 151s)
with `--keep`, and its own `eshu-api` binary was restarted against the retained
backends for the readback.

`POST /api/v0/code/relationships` against the `Dog` class
(`content-entity:e_8adc1231903a`, `inheritance.py`) returned HTTP 200 for both
directions:

| direction | edge | resolution_method | confidence |
| --- | --- | --- | ---: |
| `outgoing` | Dog -> Animal (`content-entity:e_76514cf21b96`) | `declared` | 0.95 |
| `incoming` | ServiceDog (`content-entity:e_e9e009bc5f12`) -> Dog | `declared` | 0.95 |

Each edge carried resolved `source_name`/`target_name`, `source_type`/
`target_type` of `Class`, file paths, and start/end lines — not the literal
`type(rel)` text that defect #5694 described.

This is the route the matrix said "returns empty for every edge" because older
NornicDB builds returned literal text for `type(rel)`/`coalesce` after an
`OPTIONAL MATCH`. #5694 is closed. #5916 recorded that historical backend
failure, and #6262 now requires the corrected v1.2.3 behavior while retaining
the query split. The deployed route returns real edges.

The corpus fixture used is `python_comprehensive/inheritance.py`, which declares
a deliberate hierarchy (`Animal`, `Dog(Animal)`, `Cat(Animal)`,
`ServiceDog(Dog, LogMixin, SerializeMixin)`, `GuideDog(ServiceDog)`), so both a
single-parent and a multiple-inheritance shape are present in the graph the
readback queried.
