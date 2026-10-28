<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# eshu-mcp destination content plan

Refs #8873, part of #6707. Status: preparation only. Nothing in this plan
has been pushed to the destination repository.

## Destination

`eshu-hq/eshu-mcp`: branch `main`. It holds no cutover
authority until its reviewed cutover lands. Accountable
release/deployment owner: `@linuxdynasty` (owner-confirmed per the issue).
Deployment target: the owner-designated QA EKS environment.

## What moves (source: the ownership map)

`go/cmd/mcp-server/CUTOVER_OWNERSHIP.md` is the machine-checked census: 168
registry tools plus the `find_tool`/`call_tool` router surface, every tool in
exactly one family row, every backing `/api/v0` route served by the dispatch
tree, every source file present in the tree.

- Protocol handling, tool registration, dispatch and adaptation (all `EXTRACT`
  rows: every tool family, the `router` find/call surface, the stdio/SSE/
  streamable transports, the transport auth guard and credential composition)
  move to `eshu-mcp` as consumers of versioned read contracts. No core
  implementation handle travels with them: `wiring_router.go` imports
  `go/internal/query` and `go/internal/storage/postgres` handles that the
  destination replaces with contract clients.
- Shared response and result shapes (the `SHARED` row: the dispatch envelope,
  `result/shape`, the tool and route contract types, `EnvelopeMIMEType`) move
  to one explicit public contract with a single publisher, never to whichever
  adapter imports them first. Adapters stay separate repositories.
- Core-owned read implementation, reducer/graph truth, stores, the in-tree
  server composition root and the Postgres-backed runtime surface (the
  `RETAIN` rows) stay in `eshu`. MCP-tool truth agreement: the destination
  reproduces the registry byte-for-byte and serves every tool from the same
  backing contract with the same scope, limits, budget, envelope and
  data/truth/error semantics.

## What stays in the core

Read-model implementation, canonical graph projection, durable stores,
producer admission authority, redaction policy, and the current in-tree
`mcp-server` composition until production acceptance. The old implementation
is removed only after that acceptance.

## Cutover readiness (all NOT_CHECKED)

Representative plus hostile parity inputs, concurrent retry/duplicate/
ordering/cancellation/revoked-authority proof, bounded-resource proof with
operator signals, independent build/release/deploy from supported public
contracts, credential/artifact/version-pair record, and a rehearsed rollback
with no dual producer authority and no lost facts. Live MCP calls against
deployed artifacts: NOT_CHECKED.
