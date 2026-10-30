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
- `ManifestConsumptionKeys` turns one manifest dependency row into registry
  join keys. Every package manager but sbt goes through `ConsumptionKeys`
  unchanged; an sbt row joins the Maven artifact it publishes: `%` by its exact
  name, `%%` only by `sbt_binary_artifact` (`name_2.13`, `name_3`), and `%%%`, a
  custom `cross`, or a row with no known Scala binary version not at all.
  `NormalizeEcosystem("sbt")` stays empty so no caller can reach the unsuffixed
  name through an alias.
- `ManifestPackageManagersForEcosystem` lists the `package_manager` values a
  registry ecosystem's manifests can carry (`maven` -> `gradle`, `maven`,
  `sbt`; `gomod` -> `go`, `gomod`). It is a read prefilter, never an identity rule.
- `ManifestEntityNameCandidates` returns the names a reducer asks a store for
  when it reads manifest dependency rows by their stored `entity_name`. It is
  `ConsumptionNameCandidates` plus, for Maven, the case-preserved
  `group:artifact` coordinate (and its lowercase form) derived from a
  `group/artifact` or `group:artifact` name, because Maven and Gradle manifests
  store that coordinate while an advisory can name the package as a bare
  artifact, a slash Package URL path, or a package ID. It never feeds
  `ConsumptionKeys`, so persisted join keys do not change (issue #9286).
- `LowerASCII` is the only case fold package identity uses: it maps A-Z to
  a-z and keeps every other byte, so a non-ASCII letter such as U+0130 or the
  Kelvin sign U+212A never folds onto an ASCII letter (#9584). It equals
  `strings.ToLower` on every ASCII input. `ContainsNonASCII` reports a name
  that holds any non-ASCII byte; the ingester counts those rows.
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
- Case folding is ASCII-only in every ecosystem, for names, namespaces,
  registry hosts, package managers, and ecosystem aliases alike. Unicode case
  mapping would give a lookalike name (`Serİlog`) the key of the real package
  (`Serilog`), and no registry Eshu reads defines a Unicode fold: PyPI names
  ([name normalization](https://packaging.python.org/en/latest/specifications/name-normalization/)),
  Go module paths (`golang.org/x/mod/module.CheckPath`), and crates.io names
  ([manifest reference](https://doc.rust-lang.org/cargo/reference/manifest.html))
  are ASCII-only, npm rejects names that are not URL-safe
  (`validate-npm-package-name`), and NuGet IDs may not hold characters that are
  not valid in a URL ([nuspec reference](https://learn.microsoft.com/en-us/nuget/reference/nuspec)).
  A non-ASCII name therefore keeps its non-ASCII letters in its keys and joins
  only an identically spelled advisory or registry record.
- NuGet residual: the nuget.org gallery validates new package IDs with the
  ASCII-only pattern `^[A-Za-z0-9_]+([.-][A-Za-z0-9_]+)*$`
  (`NuGet/NuGetGallery`, `src/NuGetGallery.Core/Packaging/PackageIdValidator.cs`)
  and keeps `\w` only for reading legacy IDs, so only legacy IDs can hold
  non-ASCII letters. nuget.org compares those with .NET `ToLowerInvariant`,
  so two legacy IDs that differ only in non-ASCII case are one package there
  and two keys here (a false negative). Confirming nuget.org's behavior is
  tracked in #9594.
- Registry hosts fold ASCII letters only, the same as names. ASCII and
  punycode hosts are unchanged. A host written in Unicode keeps its
  non-ASCII letters, so two Unicode spellings of one internationalized host
  give two package IDs (fail-closed, no false join). The proper fold for such
  hosts is IDNA mapping, which is out of scope here.

## Evidence

No-Regression Evidence: `go test ./internal/packageidentity ./internal/collector/registry/package ./internal/collector/vulnerabilityintelligence ./internal/reducer ./internal/projector ./internal/storage/cypher ./internal/query -count=1`
proves the shared identity contract normalizes npm, PyPI, Go, Maven, Composer,
RubyGems, Cargo, Pub, Swift, Hex, NuGet, OS, and generic package coordinates, carries
PURL/BOMRef/manager/source fields through package-registry facts, keeps OSV
and GLAD affected-package facts on the same canonical IDs, and preserves
bounded package-registry query behavior.

No-Observability-Change: the identity package is pure normalization and emits
no runtime telemetry. Existing collector fact counters, reducer queue metrics,
canonical phase spans, graph query spans, and API `count/limit/truncated` truth
metadata expose the runtime stages that consume these identity fields.
