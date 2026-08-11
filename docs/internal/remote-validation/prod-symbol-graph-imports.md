# Production validation: File-[:IMPORTS]->Module symbol graph

Validation-Slug: prod-symbol-graph-imports
Validation-Tier: deployed_services
Validation-Date: 2026-08-11
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu5970proof ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=1200 bash scripts/verify-golden-corpus-gate.sh --keep; echo $?
Validation-Exit-Code: 0
Capability-Assertion: symbol_graph.imports returned 25 deployed IMPORTS relationships served from the graph backend, with source file, target module, and line number per row.
B12-Assertion: symbol_graph.imports -> mcp:investigate_import_dependencies

## Observed result

The gate ran green (`552 pass, 0 required-fail, 0 advisory-warn`, elapsed 151s)
with `--keep`, and its own `eshu-api` binary was restarted against the retained
backends for the readback.

`POST /api/v0/code/imports/investigate` with
`{"query_type":"module_dependencies","repo_id":"repository:r_b15bccd4","limit":25}`
returned HTTP 200 with:

- `count` = 25
- `coverage.relationship_types` = `["IMPORTS"]`
- `coverage.query_shape` = `repo_file_imports`
- every row `source_backend` = `graph`
- distinct target modules including `abc`, `asyncio`, `collections`,
  `contextlib`, `dataclasses`, `fastapi`, `functools`, `os`, `os.path`, and the
  relative `./basic`
- first row: `source_file` `routes/fastapi_app.py`, `target_module` `fastapi`,
  `line_number` 7, `relationship_type` `IMPORTS`

`query_type: imports_by_file` returned the same shape over the same edges.

This closes the gap defect #5691 recorded: no parser emitted the IMPORTS fact
the graph writer needed, so the deployed graph carried zero IMPORTS edges for
every language. PR #5911 added the producer
(`go/internal/projector/canonical_import_extract.go`, `extractImportsFromFiles`
populating `CanonicalMaterialization.Imports`), and the deployed read now serves
those edges from the graph rather than returning empty.

Worth recording for whoever tunes this next: wiring the producer moved real work
into the canonical `structural_edges` phase-group write, which is where the
IMPORTS edge statements are built. On the 896-repository profile that phase went
from 2 to 601 phase-group chunks and now dominates that write. See #5122 for the
attribution and the write-budget consequence; it does not affect the correctness
recorded here.
