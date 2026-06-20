# AGENTS.md - internal/searchembedruntime guidance for LLM assistants

## Read first

1. `go/internal/searchembedruntime/README.md` - package purpose and boundary.
2. `go/internal/searchembedruntime/config.go` - environment selection logic.
3. `go/internal/searchembedprovider/README.md` - hosted embedder adapter.
4. `go/internal/semanticprofile/README.md` - provider profile contract.

## Invariants this package enforces

- API, MCP, and reducer must share the same selected vector identity.
- Explicit local hash mode wins over provider auto-selection. `auto_hash` is
  the Compose fallback mode: it yields to one governed `search_documents`
  profile and otherwise falls back to local hash mode.
- Provider auto-selection is allowed only for a single governed
  `search_documents` profile that is admitted by semantic policy and egress,
  unless a selector is configured.
- Selection must not call provider endpoints.

## Common changes and how to scope them

- **New environment variable** - add a red test here, update env registry and
  public environment docs.
- **Provider selection change** - prove single-profile, multi-profile, selected,
  and disabled cases.
- **Identity change** - update reducer/API/MCP wiring tests so vector build and
  read identities remain in lockstep.

## Failure modes and how to debug

- Invalid local embedder values fail during runtime startup.
- Multiple provider profiles fail until a selector is set.
- A selected profile that is missing policy/egress, dimensions, endpoint, model,
  or supported credentials fails before any datastore or provider call.

## Anti-patterns specific to this package

- Silently choosing the first provider when multiple are configured.
- Reading credential values into logs, status, docs, or errors.
- Letting API/MCP and reducer compute different provider identities.
