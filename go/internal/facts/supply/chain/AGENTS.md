# chain — agent instructions

This package holds the supply-chain fact families' declarations. They moved
out of the facts root in issue #6776.

## Invariants

- **Never import `go/internal/facts` from here.** The facts root imports
  this package to build `schemaVersionFamilies`; the reverse edge is an
  import cycle.
- **A new or changed fact kind is a Contract System v1 change.** Add it to
  `specs/fact-kind-registry.v1.yaml`, add the family to the facts root's
  `schemaVersionFamilies`, and run
  `bash scripts/verify-fact-kind-registry.sh`,
  `bash scripts/verify-factschema-diff.sh`, and
  `bash scripts/verify-payload-usage-manifest.sh`. Load the
  `eshu-contract-rigor` skill first.
- **Kind strings are durable.** A fact kind string is persisted in
  `fact_envelopes` and matched by reducer handlers and cassettes. Renaming
  one is a migration, not an edit.
- **Keep the emission order.** `<Family>FactKinds` returns the collector's
  emission order and its tests assert it positionally. Insert a new kind
  where the collector actually emits it.
- **Return copies.** Every accessor clones its backing slice so a caller
  cannot mutate the package's ordering; a new accessor does the same.
- **No package-name stutter and no glued directory.** Do not reintroduce a
  `SupplyChain*` prefix (`docs/internal/naming.md` rule 4), and do not
  collapse `supply/chain` back to `supplychain` — it is a named
  already-fixed violation in `go/cmd/naming-glue-gate/prompt.go`.
- **Removing a `facts.X` alias is a separate, caller-driven step.**
  `compat_supply_chain.go` in the facts root carries the pre-move spellings;
  delete an entry only once its last caller has moved.

## Proof expected for a change here

- `cd go && go test ./internal/facts/... -count=1`
- `cd go && go vet ./...` — the compat surface means a rename here breaks
  callers in other packages, and only a whole-module vet sees them
- For a move or rename, `go test -list '.*' ./internal/facts/...` diffed
  against the base
- The three contract gates above when a kind or schema version changes
