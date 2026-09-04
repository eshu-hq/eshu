# Supply-chain core package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/supplychaincore/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- This package must remain a leaf below `internal/reducer`; never import the
  parent reducer package or any sibling family package.
- It declares data types and their string-enum constants only. No functions
  with bodies, no queue, storage, graph, telemetry, or runtime dependencies.
- Preserve every field, method-free type shape, and constant string value byte
  for byte; the parent reducer, the query decoders, and the storage loaders all
  depend on the exact wire values.

## Common changes

Add a type here only when it must be shared across the impact-finding and
suppression halves of the family (or is in the field-type closure of one that
is). Add the type and its constants together, and add the matching alias in the
reducer root in the same change so existing callers keep their spelling.

## Failure modes

- Importing the parent reducer package from here re-forms the #6061 cycle this
  leaf exists to break.
- Changing a constant string literal silently changes persisted wire truth and
  the query/MCP response shape without a compiler error.
- Adding a field typed as a reducer-root type makes `SupplyChainImpactFinding`
  un-hoistable; hoist that type too or leave the field out.

## Anti-patterns

- Do not move suppression evaluation, provenance selection, priority scoring,
  reachability enrichment, remediation, or writer logic into this leaf; only
  the value types belong here.
- Do not drop the reducer-root aliases when moving a type here.

## Architecture decisions

- #6061 establishes this leaf as the shared value tier for the reducer
  supply-chain family, so the impact-finding and suppression halves can split
  into sibling packages without importing each other.
- The parent reducer remains the composition root and compatibility surface.

## Verification

Run the reducer build and the reducer and parent tests, package-doc
verification, and the directory-size gate:

```
cd go && env -u GOROOT go build ./internal/reducer/...
cd go && env -u GOROOT go test ./internal/reducer -count=1
bash scripts/verify-package-docs.sh
bash scripts/verify-dirgate.sh --digest internal/reducer
```
