# fact-kind-registry

## Purpose
Generates the core fact-kind registry from typed fact-family contract
declarations in Go source (issue #6629): `contract.Declare` family
declarations beside fact definitions and `contract.Bind` reducer
bindings beside their implementations. The command discovers them with
a `go/parser`-only scan (scanned packages are never compiled or
executed), validates the contract guards, checks the derived registry
covers every live fact kind exposed by `internal/facts` family helpers,
then writes the compact `specs/fact-kind-registry.v1.yaml`, the
`specs/fact-kind-registry-contracts.d/` family/kind/binding shards and
catalog pages, the Go registry, and the package Markdown catalogue
from the same declarations.

## Ownership boundary
This command owns only build-time generation and drift verification. It does not
own fact payload structs, collector emission, reducer projection, or API/MCP
read behavior.

## Exported surface
This is a command package. Run it through `scripts/generate-fact-kind-registry.sh`
or `scripts/verify-fact-kind-registry.sh`. The `-spec` flag names the generated
registry YAML output path (a generated artifact since #6629, not an input).

## Dependencies
It imports `internal/facts` to enumerate live fact families,
`internal/facts/contract` for the declaration model it evaluates, and
`gopkg.in/yaml.v3` to render the generated YAML outputs. Discovery and the
reducer-evidence scan use only `go/parser`, `go/ast`, and `go/token` from the
standard library — never `go/packages` or `go/types` — so package
initialization in scanned code cannot run.

## Telemetry
No runtime telemetry is emitted. The command runs in local and CI generation
gates only.

## Gotchas / invariants
A non-exempt family's declared kinds must match its live family helper exactly.
Deterministic entries must be provider-key independent, and optional semantic
entries must name a policy gate.

An `admission_exempt: true` family (issue #4752) is the exception: it has no
live `(XFactKinds, XSchemaVersion)` helper, must leave `schema_version` blank,
and its kinds classify as `unknown_kind` at runtime — it records
`payload_schema` and reserves the kind name without entering schema-version
admission. The generator fails closed if an exempt family sets a schema
version. See `docs/public/reference/fact-schema-versioning.md`.

Every kind has exactly one reducer-consumption state: one or more verified
bindings (file colocated in the reducer directory, real top-level symbol,
per-kind consumption evidence), or a `Disposition` with a non-blank
`NoReducerReason` and `NoReducerEvidence`. A disposition that contradicts
live reducer evidence fails as a stale disclosure. Generated shard paths
follow the arbiter filename policies (one exact `<family>_` or `<directory>_`
prefix strip), and the generator rejects duplicate or case-colliding
destinations, unsafe components, file/directory conflicts, directory-word
filename stutter, and any output at or over 500 lines.

## Related docs
- `go/internal/facts/README.md`
- `go/internal/facts/contract/README.md`
- `go/internal/facts/FACT_KIND_REGISTRIES.md`
- `specs/README.md`
