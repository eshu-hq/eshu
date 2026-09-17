# Code MCP namespace

Groups MCP code-family route-selection packages. The leaves decide whether
the parent owns a tool and map decoded arguments to a dependency-neutral
internal request without executing it.

- `flow` selects the four code-flow tools (`dispatch_taint_path`,
  `dispatch_reaching_def`, `dispatch_cfg_summary`, `dispatch_pdg_summary`).
- `intel` selects the eight code-intelligence tools (`find_code`,
  `find_symbol`, `inspect_code_inventory`, `inspect_call_graph_metrics`,
  `trace_route_callers`, `investigate_code_topic`,
  `execute_language_query`, `find_function_call_chain`).
- `quality` selects the complexity/quality tools
  (`calculate_cyclomatic_complexity`, `find_most_complex_functions`,
  `inspect_code_quality`).
- `owners` selects the CODEOWNERS ownership tool
  (`list_codeowners_ownership`).
- `dead` selects the three dead-code tools (`find_dead_code`,
  `investigate_dead_code`, `find_cross_repo_dead_code`).

## Ownership boundary

Leaf packages own family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapters, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. `internal/query/codequery`
owns the bounded reads behind each `POST /api/v0/code/...` path, with
dead-code analysis behind its own `deadcode` leaf. Matching domain names do
not make MCP selection and query execution one owner or a shared package;
this namespace must not import query execution and query execution must not
import this namespace.

### Move record (#6627)

Rename-only `codeflow` to `code/flow`, `codeintel` to `code/intel`,
`codequality` to `code/quality`, `codeowners` to `code/owners`, and
`deadcode` to `code/dead` moves. Every exported Go symbol is identical;
only import paths change.

No-Observability-Change: no stage added and no metric, span, or log name
changed; this namespace declares no function, holds no state, and performs
no I/O of its own.

## Related docs

- [MCP architecture](../README.md)
- [Query code family](../../../query/codequery/doc.go)
