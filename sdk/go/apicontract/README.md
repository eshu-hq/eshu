# API Wire-Contract SDK

This module publishes the Eshu API wire shapes a thin client needs:
the `{data, truth, error}` response envelope, truth and profile contracts,
the MCP tool-definition shape, and the client-scoped environment-variable
registry with its validator. It intentionally does not import
`github.com/eshu-hq/eshu/go/internal` packages.

## Compatibility

- Go module path: `github.com/eshu-hq/eshu/sdk/go/apicontract`
- Initial SDK module semver: `v0.1.x` (proposed; tag cut by maintainer per
  `RELEASING.md` after this lane merges)
- Wire sources: `go/internal/query/querycontract` (envelope, truth, profile,
  freshness), `go/internal/mcp/contract/tool` (tool definition),
  `go/internal/envregistry` (client entries, validator)
- Golden pins: `testdata/golden/*.json`, generated from the live core types;
  the core drift tests (`apicontract_drift_test.go` in each source package)
  regenerate the comparison from the live side on every run

The module version moves only when a ported shape or entry moves. A core
change to any source shape must update the goldens and this module in the
same change; both directions fail closed without it.

## Contracts

The published surface includes:

- `ResponseEnvelope`, `TruthEnvelope`, `ErrorEnvelope`, `ErrorCode`, and the
  truth-level/basis/freshness/profile/backend vocabularies — byte-identical
  JSON to the core query contract.
- `QueryProfile` and `GraphBackend` with their pure parse/normalize helpers.
- `FreshnessCause`, `FreshnessNextCheck`, and `ValidFreshnessCause` for
  decoding freshness metadata.
- `ToolDefinition` — the advertised MCP tool shape, decoded from `tools/list`.
- `Entry`, `Finding`, `Registry`, and `Validate` — the client-scoped
  environment registry (`ClientEntries`) with the same pure validation the
  core runs, plus `New`, `Lookup`, `Covers`, and `Entries`.

## What stays out

- Envelope constructors, the capability matrix, and profile ceilings: the
  core query owner admits capabilities, not the client.
- Freshness cause plumbing (cause to next-check table, timestamp formatters):
  the server attaches causes; a client only decodes them.
- Retry markers, packet renderers, report composers, bundle capture/validate,
  and markdown reference pages: those run behind core endpoints, never in a
  client library (`config validate --reference` keeps rendering in core; the
  extracted CLI formats its own reference output at population time).
- Answer classification prompts, server/storage/reducer/graph/queue handles:
  never a client contract.

## Client table scope

`ClientEntries` covers the `ESHU_*` names a thin CLI reads or writes outside
local-owner paths (API auth, MCP setup, query profile, config home and
backend selection). Server, collector, reducer, and local-supervision
variables stay core-owned. Two names the thin CLI reads are not covered
because the core registry does not own them yet — `ESHU_SERVICE_URL`
(`go/cmd/eshu/client.go`, remote base URL) and
`ESHU_REMOTE_TIMEOUT_SECONDS` (remote timeout). Registering them is registry-
owner follow-up; this module syncs them once the registry owns them.

## Snippet scope decision

`mcp setup` snippet generation needs no published shape and no remote call:
it renders the config file from the setup request and platform alone
(`go/internal/cli/mcpsetup/snippet.go` takes no tool definitions). The
`--verify` tools-visible stage needs only the tool count, which a thin client
serves from a remote `tools/list` response decoded into `ToolDefinition`
instead of the in-process catalog. The shape is published for that decode;
no local catalog import travels with it.
