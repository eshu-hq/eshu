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

## Evidence

No-Regression Evidence (#6950 batch 4d, supply-chain vulnerability_suppression stanza): this change
moves the twelve `chain.*` vulnerability_suppression compat entries' Go importers (ten fact-kind,
justification, source, and schema-version constants plus the `<Family>FactKinds`/
`<Family>SchemaVersion` accessors) off the transitional `facts.VulnerabilitySuppression*`
compat spellings and deletes the emptied stanza from `compat_supply_chain.go`. No fact-kind
string, payload shape, registry entry, or executable statement changes: across 32 Go files,
every production hunk requalifies an identifier or import path only (plus the two MCP matcher
widenings, which resolve through factsConstValues so unknown spellings match nothing), every
other hunk is a package-doc rewording, a ledger row, or the compat stanza's own deletion, and
the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-supply-batch4d-before, ledger:6950-supply-batch4d-after). The command is `go test -count=1`
over the 8 affected package targets (per-side counts in the
cited rows) on baseline `b238366b5e` vs measurement commit `1527b3e5cd`
(this Evidence note and the two ledger rows are the only later changes):
288 packages ok with zero failures on both sides, with the ok-package set
byte-identical after timing strip. `go test -list` inventory is identical
on both sides (31503 Test names plus 352 benchmarks). Backend/version:
go1.26.9 linux/amd64, in-memory; no backend touched. Input shape: n/a (no
runtime input). Terminal queue/row counts: none — no queue, lease, Cypher,
or SQL path is touched. Contract gates green on the branch:
`verify-fact-kind-registry.sh` (generated artifacts byte-identical),
`verify-factschema-diff.sh` (no breaking changes),
`verify-payload-usage-manifest.sh`, and `verify-contracttest.sh`,
plus `precommit-go.sh surface` (no MCP tool-surface drift). The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 4d, supply-chain vulnerability_suppression stanza): this package
carries no instrumentation (see Telemetry above) and the move adds, removes,
or renames no metric, span, structured log, or status field in any touched
package. The collector, reducer, projector, storage, ifa, and mcp telemetry that
reads and writes facts of these kinds is untouched; operator signals are
identical before and after.

No-Regression Evidence (#6950 batch 4e, supply-chain sbom_attestation stanza): this change
moves the eleven `chain.*` sbom_attestation compat entries' Go importers (nine fact-kind
and schema-version constants plus the `<Family>FactKinds`/
`<Family>SchemaVersion` accessors) off the transitional `facts.SBOM*` and
`facts.Attestation*` compat spellings and deletes the emptied stanza from
`compat_supply_chain.go`. No fact-kind string, payload shape, registry entry, or executable
statement changes: across 55 Go files, every production hunk requalifies an identifier or
import path only, every other hunk is a package-doc link rewording, a ledger row, or the
compat stanza's own deletion, and the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-supply-batch4e-before, ledger:6950-supply-batch4e-after). The command is `go test -count=1`
over the 7 affected package targets (per-side counts in the
cited rows) on baseline `652af9751a` vs measurement commit `d286fc683b`
(this Evidence note and the two ledger rows are the only later changes):
607 packages ok with the same single pre-existing #7865 failure on both sides, with the
ok-package set byte-identical after timing strip. `go test -list` inventory is identical
on both sides (9657 Test names plus 143 benchmarks). Backend/version:
go1.26.9 linux/amd64, in-memory; no backend touched. Input shape: n/a (no
runtime input). Terminal queue/row counts: none — no queue, lease, Cypher,
or SQL path is touched. Contract gates green on the branch:
`verify-fact-kind-registry.sh` (generated artifacts byte-identical),
`verify-factschema-diff.sh` (no breaking changes),
`verify-payload-usage-manifest.sh`, and `verify-contracttest.sh`,
plus `precommit-go.sh surface` (no MCP tool-surface drift). The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 4e, supply-chain sbom_attestation stanza): this package
carries no instrumentation (see Telemetry above) and the move adds, removes,
or renames no metric, span, structured log, or status field in any touched
package. The collector, reducer, projector, and cmd telemetry that
reads and writes facts of these kinds is untouched; operator signals are
identical before and after.

No-Regression Evidence (#6950 batch 4f, supply-chain oci_registry stanza): this change
moves the sixteen `chain.*` oci_registry compat entries' Go importers (fourteen fact-kind
and schema-version constants plus the `<Family>FactKinds`/
`<Family>SchemaVersion` accessors) off the transitional `facts.OCIImage*` and
`facts.OCIRegistry*` compat spellings and deletes the emptied stanza from
`compat_supply_chain.go`. No fact-kind string, payload shape, registry entry, or executable
statement changes: across 55 Go files, every production hunk requalifies an identifier or
import path only, every other hunk is a ledger row or the compat stanza's own deletion,
and the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-supply-batch4f-before, ledger:6950-supply-batch4f-after). The command is `go test -count=1`
over the 8 affected package targets (per-side counts in the
cited rows) on baseline `3cc7c006cd` vs measurement commit `3b25552e03`
(this Evidence note and the two ledger rows are the only later changes):
613 packages ok with the same single pre-existing #7865 failure on both sides, with the
ok-package set byte-identical after timing strip. `go test -list` inventory is identical
on both sides (10236 Test names plus 145 benchmarks). Backend/version:
go1.26.9 linux/amd64, in-memory; no backend touched. Input shape: n/a (no
runtime input). Terminal queue/row counts: none — no queue, lease, Cypher,
or SQL path is touched. Contract gates green on the branch:
`verify-fact-kind-registry.sh` (generated artifacts byte-identical),
`verify-factschema-diff.sh` (no breaking changes),
`verify-payload-usage-manifest.sh`, and `verify-contracttest.sh`,
plus `precommit-go.sh surface` (no MCP tool-surface drift). The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 4f, supply-chain oci_registry stanza): this package
carries no instrumentation (see Telemetry above) and the move adds, removes,
or renames no metric, span, structured log, or status field in any touched
package. The collector, reducer, projector, ifa, and cmd telemetry that
reads and writes facts of these kinds is untouched; operator signals are
identical before and after.

## Related docs

- `docs/public/reference/fact-schema-versioning.md`
- `docs/internal/design/contract-system-v1.md`
- `docs/internal/naming.md`
