# fact-kind-registry

## Purpose
Generates the core fact-kind registry from `specs/fact-kind-registry.v1.yaml`.
The command checks that the spec covers every live fact kind exposed by
`internal/facts` family helpers, then writes the Go registry and the package
Markdown catalogue from the same source.

## Ownership boundary
This command owns only build-time generation and drift verification. It does not
own fact payload structs, collector emission, reducer projection, or API/MCP
read behavior.

## Exported surface
This is a command package. Run it through `scripts/generate-fact-kind-registry.sh`
or `scripts/verify-fact-kind-registry.sh`.

## Dependencies
It imports `internal/facts` to enumerate live fact families and `gopkg.in/yaml.v3`
to parse the spec.

## Telemetry
No runtime telemetry is emitted. The command runs in local and CI generation
gates only.

## Gotchas / invariants
A non-exempt family's YAML kinds must match its live family helper exactly.
Deterministic entries must be provider-key independent, and optional semantic
entries must name a policy gate.

An `admission_exempt: true` family (issue #4752) is the exception: it has no
live `(XFactKinds, XSchemaVersion)` helper, must leave `schema_version` blank,
and its kinds classify as `unknown_kind` at runtime — it records
`payload_schema` and reserves the kind name without entering schema-version
admission. The generator fails closed if an exempt family sets a schema
version. See `docs/public/reference/fact-schema-versioning.md`.

## Related docs
- `go/internal/facts/README.md`
- `go/internal/facts/FACT_KIND_REGISTRIES.md`
- `specs/README.md`
