# Supply-chain MCP namespace

Groups the two MCP supply-chain route-selection siblings, kept as separate
packages because evidence selection and impact findings are distinct
responsibilities:

- `evidence` selects the supply-chain-evidence tools (vulnerability-scanner
  read contract, advisory evidence, SBOM/attestation attachments).
- `impact` selects the supply-chain-impact tools (findings, count,
  inventory, explanation).

## Ownership boundary

Leaf packages own family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapters, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry.

### Move record (#6627)

Rename-only `supplychainevidence` to `supply/chain/evidence` and
`supplychainimpact` to `supply/chain/impact` moves. Every exported Go
symbol is identical; only import paths change.

No-Observability-Change: no stage added and no metric, span, or log name
changed; this namespace declares no function, holds no state, and performs
no I/O of its own.

## Related docs

- [MCP architecture](../../README.md)
