# Production validation: reaching-definition dataflow

Validation-Slug: prod-code-flow-reaching-def
Validation-Tier: deployed_services
Validation-Date: 2026-08-11
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu5970proof ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=1200 bash scripts/verify-golden-corpus-gate.sh --keep; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_flow.reaching_def returned five deployed definitions with exact parser dataflow coverage and per-binding def/use lines from POST /api/v0/code/flow/reaching-def.
B12-Assertion: code_flow.reaching_def -> mcp:dispatch_reaching_def

## Observed result

The gate ran green (`552 pass, 0 required-fail, 0 advisory-warn`, elapsed 151s)
and was invoked with `--keep`, which retains the Compose backends and the work
dir. The gate's own `eshu-api` binary was then restarted against those retained
backends and queried directly, because the gate kills its background host
binaries on exit even under `--keep`.

`POST /api/v0/code/flow/reaching-def` with
`{"repo_id":"repository:r_b15bccd4","language":"python","limit":5}` returned
HTTP 200 with:

- `coverage.state` = `exact`
- `coverage.reason` = `active parser dataflow facts matched the requested scope`
- `bounds.count` = 5
- first definition `__init__`, `language` python, `fact_label`
  `exact_parser_fact`, carrying a `def_use` entry with `binding` `config`,
  `def_line` 37 and `use_line` 38, plus an `evidence_handle` of the form
  `fact://code_dataflow_function/<digest>`

This is the gap issue #5692 left open. That issue wired `ESHU_EMIT_DATAFLOW`
into bootstrap-index and the ingester but no deployed run had ever been captured
with the flag on. The flag is now exported by the gate itself
(`scripts/verify-golden-corpus-gate.sh`, added by #5988 on 2026-08-09), so the
gate's bootstrap-index produces reaching-def facts and the deployed read returns
them. The matrix note claiming "the corpus gate still runs with it off" was
stale as of that change.

The route stays operationally gated: `ESHU_EMIT_DATAFLOW` is off by default, and
this artifact records the opted-in deployed behavior, not a default-on contract.

The same run's B-7 assertions independently exercised the sibling dataflow read
surfaces `mcp:dispatch_cfg_summary` and `mcp:dispatch_pdg_summary`, both PASS
with non-empty results.
