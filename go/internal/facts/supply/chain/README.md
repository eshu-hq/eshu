# Supply-chain facts

## Purpose

`chain` owns the fact kinds and schema versions for the supply-chain
evidence families: OCI registry observations, language package registries,
SBOM documents and attestations, vulnerability intelligence, and
vulnerability suppressions.

It moved here from `go/internal/facts` in issue #6776, which nested the flat
facts root back under the 40-non-test-file dirgate cap. The path is
`supply/chain` rather than `supplychain` because a glued compound directory
name violates `docs/internal/naming.md` rule 3; it mirrors the existing
`go/internal/query/supply/chain`.

## Ownership boundary

Declarations only. This package does not scan registries, parse SBOMs,
resolve advisories, or decide whether a suppression is authoritative. The
collectors that emit these facts live under `internal/coordinator`
(`oci/registry`, `registry/package`, `sbom/attestation`,
`vulnerability`); the truth derived from them lives in `internal/reducer`
and `internal/projector`; the read surfaces live in
`internal/query/supply/chain`.

## Exported surface

- `OCIRegistryFactKinds`, `OCIRegistrySchemaVersion` and the `OCI*` kind and
  schema-version constants — registry repository, manifest, index,
  descriptor, referrer, tag-observation, and warning evidence
- `PackageRegistryFactKinds`, `PackageRegistrySchemaVersion` and the
  `PackageRegistry*` constants — package, version, artifact, dependency,
  repository-hosting, source-hint, vulnerability-hint, registry-event, and
  warning evidence
- `SBOMAttestationFactKinds`, `SBOMAttestationSchemaVersion`, the `SBOM*`
  constants, and the `Attestation*` constants — SBOM documents, components,
  dependency relationships, external references, in-toto statements, SLSA
  provenance, and signature verification
- `VulnerabilityIntelligenceFactKinds`,
  `VulnerabilityIntelligenceSchemaVersion` and the `Vulnerability*`
  constants — CVE records, references, EPSS scores, known-exploited
  markers, affected products and packages, OS packages, Go module evidence,
  Go call reachability, and source snapshots
- `VulnerabilitySuppressionFactKinds`,
  `VulnerabilitySuppressionSchemaVersion`, and the suppression source and
  justification vocabularies

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`slices` for the defensive copies). It must not
depend on `internal/facts`: the root imports this package, so the reverse
edge is an import cycle.

## Dependents

- `internal/facts` — `compat_supply_chain.go` re-exports every pre-move
  spelling, and `schema_version.go` dispatches through the accessors
- the `internal/coordinator` supply-chain collectors, `internal/reducer`,
  `internal/projector`, and `internal/query/supply/chain`, which reach these
  names as `facts.X` today

## Telemetry

None. This package emits no metrics, spans, or logs; it has no runtime
behavior to observe. Operator signals for supply-chain ingestion live with
the collector, reducer, and query packages that consume these kinds.

## Gotchas / invariants

- Two families must never claim the same fact kind. The facts root's
  `liveSchemaVersionRegistry` panics at init if they do, which surfaces in
  any test that touches the registry.
- A fact kind declared here must also appear in
  `specs/fact-kind-registry.v1.yaml`, or the facts root's
  `ValidateFactKindRegistry` fails with "missing registry entry for fact
  kind".
- `<Family>SchemaVersion` returning `false` is the contract for a kind the
  family does not own; it is not an error and must not be turned into one.
- `<Family>FactKinds` order is the collector's emission order, not
  alphabetical. Reordering it changes observable behavior for consumers that
  iterate it.

## Related docs

- `docs/public/reference/fact-schema-versioning.md`
- `docs/internal/design/contract-system-v1.md`
- `docs/internal/naming.md`
