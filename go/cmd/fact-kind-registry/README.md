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
The YAML must list every fact kind in the live family helpers. Deterministic
entries must be provider-key independent, and optional semantic entries must
name a policy gate.

## Related docs
- `go/internal/facts/README.md`
- `go/internal/facts/FACT_KIND_REGISTRIES.md`
- `specs/README.md`
