# Collector registry contracts

## Purpose

`collector/registry` groups the collector-side packages that turn upstream
container and package registry metadata into source-neutral,
reported-confidence facts. It is a documentation-only namespace;
implementation belongs in its leaf packages and collection behavior stays in
the owning collector packages.

## Ownership boundary

The namespace owns no runtime declarations, provider access, fact emission,
storage calls, graph writes, or telemetry. Its `oci` child owns only OCI
Distribution-compatible registry evidence: repository and descriptor
identity normalization, tag, manifest, index, descriptor, and referrer
envelopes, plus bounded runtime scans gated by explicit target
configuration. Its `package` child owns only package-registry evidence:
ecosystem-native metadata parsing, bounded runtime target configuration,
and claim-driven metadata fetches. Neither child crawls registries,
infers ownership, or writes graph truth.

## Exported surface

None. This parent is documentation-only. See the `oci` child for its
exported surface.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract. The `oci` child's spans and counters are documented in
`go/internal/collector/registry/oci/README.md` and covered by the existing
collector OCI telemetry rows; the `package` child's counters are documented
in `go/internal/collector/registry/package/README.md` and covered by the
existing collector package-registry telemetry row. No row changes in this
nest: span and metric names are untouched.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the owning collector.
- Keep provider access, fact emission, credential handling, and telemetry
  behavior in the owning collector slice.
- The `oci` leaf is a registry evidence boundary, not an independently
  deployable service.
- The `package` leaf is a registry evidence boundary, not an independently
  deployable service.
- ECR is OCI registry evidence, not package-registry evidence. JFrog Docker
  and OCI repositories use the `oci` leaf; JFrog package feeds use the
  `package` leaf.

## Related docs

- `go/internal/collector/registry/oci/README.md`
- `go/internal/collector/registry/package/README.md`
- `go/internal/collector/README.md`
