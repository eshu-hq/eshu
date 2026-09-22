# supply

Path namespace for the facts package's `chain` family (issue #6776,
`docs/internal/naming.md` rule 3: a glued compound name splits into nested
directories rather than staying glued). This directory owns no runtime
behavior beyond naming the boundary.

| Child | Package | Owns |
| --- | --- | --- |
| `chain/` | `chain` | The supply-chain fact families: OCI registry, package registry, SBOM/attestation, vulnerability intelligence, and vulnerability suppression. See `chain/README.md` for the boundary and `chain/AGENTS.md` for the contributor invariants. |

`supplychain` is a named already-fixed violation in
`go/cmd/naming-glue-gate/prompt.go`; `supply/chain` is its destuttered form,
matching `go/internal/query/supply/chain`. The facts root's
`compat_supply_chain.go` keeps every pre-move exported spelling for callers
that still reach these names as `facts.X`.
