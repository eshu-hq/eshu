# Contract

## Purpose

`contract` is the dependency-neutral metadata model for typed,
colocated fact-family declarations (issue #6629). A `Family` value,
written beside a family's fact definitions and wrapped in
`contract.Declare`, is the build-time source of truth for that
family's identity, fact-definition directory, canonical core owner
repository, kinds, producers, read surface, and schema references. A
`Binding` value, written beside a reducer implementation and wrapped
in `contract.Bind`, declares the exact kind subset that implementation
consumes.

The fact-kind-registry generator (`go/cmd/fact-kind-registry`)
statically discovers these declarations and emits the compact
`specs/fact-kind-registry.v1.yaml`, the
`specs/fact-kind-registry-contracts.d/` shards, the generated Go
registry, and the Markdown catalog. Edit declarations, never generated
outputs; regenerate with `bash scripts/generate-fact-kind-registry.sh`.

## Ownership boundary

Owns the declaration vocabulary only: `Family`, `Kind`, `Producer`,
`Binding`, `Disposition`, and `AuthorityMode`. It owns no fact kinds,
no payload shapes, no admission or authorization behavior, and no
runtime state. Fact families own their kinds; reducer packages own
their bindings; the generator owns discovery, validation, and
rendering.

## Exported surface

- `Family`, `Kind`, `Producer` — one family's declaration: identity,
  directories, owners, legacy registry metadata, kinds with per-kind
  overrides and no-reducer dispositions, and producers with
  emitted-kind subsets.
- `Binding` — one reducer implementation's verified consumption of an
  exact kind subset of one family.
- `Declare`, `Bind` — identity markers the static generator discovers.
- `Disposition` (`read_only`, `projector_only`, `collector_only`,
  `unconsumed`) — how an unbound kind is consumed.
- `AuthorityMode` (`core_owned`, `core_issued_grant`) — a producer's
  existing authority to emit core-owned kinds. Recording a mode here
  creates no grant.

## Dependencies

None inside the module, by design. Fact families (including nested
families that cannot import the facts root) and reducer packages both
declare against this package, so it must stay a leaf: no imports from
`go/internal/facts`, its nested families, or any reducer, collector,
or query package.

## Telemetry

None. This is a leaf metadata package with no I/O and no runtime path;
the generator that consumes it reports validation failures as
file:line diagnostics on stderr. No observability change is expected
from edits confined to this package.

## Operational context

Declarations are compile-time values. The generator's discovery scan
parses source with `go/parser` only and never executes package
initialization, so a declaration cannot run code during generation.
The restricted literal grammar (string/bool literals, slice literals,
nested struct literals from this package) is documented in `doc.go`;
the contributor workflow lives in
`docs/public/reference/fact-schema-versioning.md`.
