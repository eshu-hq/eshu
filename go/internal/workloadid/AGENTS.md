# AGENTS.md — internal/workloadid

## Read first

1. `README.md` — why this package exists and the invariants it keeps.
2. `doc.go` — the godoc contract.
3. `docs/internal/design/5385-workload-identity-key.md` — the approved re-key,
   its measured blast radius, and the retract/rebuild proof that constrains the
   migration.

## Invariants

- **This package is a leaf and must stay one.** It has no in-module imports.
  Adding one defeats the reason it was split out of `internal/reducer`, where a
  74-package closure made identity construction unavailable to lower layers.
- **Exactly one constructor per identifier.** The package exists so the compiler
  can enumerate construction sites; a second constructor, or a caller building
  the string itself, silently restores the blind spot a hand-maintained list
  already had three times.
- **Constructors take `repoID` and currently ignore it.** That is deliberate and
  is not dead weight — it is what makes the re-key an edit here rather than at
  every call site. Do not remove the parameter.

## Changing the identifier format

Do not change it here alone. The format is a public contract: it appears in HTTP
path and body parameters, MCP selectors, Console URLs, persisted search
handles, and provider-asserted fields in the published SDK fact schema that
out-of-tree collectors emit. Section 6a of the design enumerates the tiers and
which of them a read-side alias can and cannot cover.

The migration also has a measured ordering constraint: retract with the **old**
ids before the new ones exist. Both retract paths anchor on the id that changes,
so retracting afterwards finds nothing and orphans the old nodes with every edge
attached. The proof is in section 5a.

## Tests

`workloadid_test.go` pins the **current** format on purpose. A re-key must update
those assertions deliberately; they exist so the format cannot move as an
unnoticed side effect.
