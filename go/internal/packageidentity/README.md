# Package Identity

## Purpose

`internal/packageidentity` owns canonical package identity normalization for
package-registry, vulnerability, SBOM, and reducer consumers. It converts raw
source records into a stable package identity with normalized ecosystem, package
name, namespace, package ID, Package URL, BOM reference, package manager, and
source-debug fields.

## Ownership Boundary

This package is pure normalization. It does not fetch registry metadata, parse
advisory documents, commit facts, write graph state, or decide whether a
repository owns or consumes a package. Collectors and parsers preserve source
truth; reducers admit user-facing truth.

## Exported Surface

- `Ecosystem` names the canonical package ecosystem family.
- `RawIdentity` carries source-observed identity fields.
- `Identity` carries normalized package identity fields used for joins and
  facts.
- `Normalize` applies ecosystem-specific normalization and returns a stable
  package ID plus PURL/BOMRef fields.
- `NormalizeEcosystem` maps package-manager aliases such as `python`,
  `typescript`, `golang`, `packagist`, `ruby`, `crates.io`, Pub aliases such as
  `pub.dev` and `dart`, and distro package managers, plus SwiftPM aliases such
  as `swiftpm` and `spm` and Hex aliases such as `hexpm`, onto Eshu's canonical
  ecosystems.
- `NormalizeRegistry` canonicalizes registry host/path values without hiding
  the source registry.
- `DefaultRegistry` returns the canonical registry host for an ecosystem so
  every caller that derives a `PackageID` from a bare purl lands on the same
  identity.
- `PackageIDFromPURL` parses a purl, fills the default registry when the purl
  carries none, and returns the canonical versionless `PackageID`. It returns
  an empty string (and nil error) for blank or non-purl input so SBOM and other
  purl-only sources can correlate on the same key without failing on odd input.

## Invariants

- `PackageID` is versionless so one package can have multiple version facts.
- `PURL` includes `Version` when the caller supplies one.
- `BOMRef` preserves a source-provided reference or falls back to the generated
  PURL/package ID.
- Raw fields remain available for debugging and read-surface explanations.
- Unknown ecosystem aliases fail closed.

## Evidence

No-Regression Evidence: `go test ./internal/packageidentity ./internal/collector/packageregistry ./internal/collector/vulnerabilityintelligence ./internal/reducer ./internal/projector ./internal/storage/cypher ./internal/query -count=1`
proves the shared identity contract normalizes npm, PyPI, Go, Maven, Composer,
RubyGems, Cargo, Pub, Swift, Hex, NuGet, OS, and generic package coordinates, carries
PURL/BOMRef/manager/source fields through package-registry facts, keeps OSV
and GLAD affected-package facts on the same canonical IDs, and preserves
bounded package-registry query behavior.

No-Observability-Change: the identity package is pure normalization and emits
no runtime telemetry. Existing collector fact counters, reducer queue metrics,
canonical phase spans, graph query spans, and API `count/limit/truncated` truth
metadata expose the runtime stages that consume these identity fields.
