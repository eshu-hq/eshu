# Cloud

## Purpose

`cloud` declares the fact-kind and schema-version constants for evidence
reported by cloud-provider collectors and posture derivations: AWS, Azure,
GCP, Kubernetes live-cluster observations, Terraform state, and the derived
EC2 instance, RDS instance, S3 bucket, and S3 external-principal-grant
posture facts.

## Ownership boundary

Owns the fact-kind vocabulary and schema-version contract for these nine
families. Does not own collection (`internal/collector/*`), reducer
projection or graph writes (`internal/reducer/*`, `internal/projector/*`),
or Postgres storage (`internal/storage/postgres`) — those packages consume
the kind and schema-version constants declared here.

## Exported surface

- `AWSFactKinds`/`AWSSchemaVersion` — resource, relationship, tag, DNS,
  image-reference, security-group-rule, derived IAM permission, derived
  resource-policy permission, and warning evidence
- `AzureFactKinds`/`AzureSchemaVersion` — resource, relationship, tag,
  identity, resource-change, DNS, image-reference, and collection-warning
  evidence
- `GCPFactKinds`/`GCPSchemaVersion` — Cloud Asset Inventory resource,
  collection-warning, relationship, tag, IAM policy observation, DNS
  record, and image-reference evidence
- `KubernetesLiveFactKinds`/`KubernetesLiveSchemaVersion` — pod-template,
  relationship, warning, and namespace evidence observed live in-cluster
- `TerraformStateFactKinds`/`TerraformStateSchemaVersion` — state
  candidate, snapshot, resource, output, module, provider-binding, tag
  observation, and warning evidence
- `EC2InstancePostureFactKinds`/`EC2InstancePostureSchemaVersion`,
  `RDSPostureFactKinds`/`RDSPostureSchemaVersion`,
  `S3BucketPostureFactKinds`/`S3BucketPostureSchemaVersion`,
  `S3ExternalPrincipalGrantFactKinds`/`S3ExternalPrincipalGrantSchemaVersion`
  — single-kind derived posture families

See `doc.go` for the full godoc contract and each file's fact-kind
constants.

## Dependencies

Go standard library only (`slices`). No internal package imports.

## Depended on by

Today: the facts root's `compat_cloud.go` imports `cloud` and forwards
every remaining constant and accessor as `facts.<Name>` (e.g.
`facts.AWSFactKinds` calls `cloud.AWSFactKinds`), and
`go/internal/facts/schema_version.go`'s `schemaVersionFamilies` table
references those forwarders alongside the posture accessors, which it
reaches directly. The posture forwarders (`compat_cloud_posture.go`) were
retired in #6950 once the last caller moved to the `cloud.<Name>` spelling.
Every external caller that uses `facts.AWSFactKinds` /
`cloud.AzureFactKinds` / etc. — `go/cmd/capability-inventory/surfaces.go`,
`go/cmd/fact-kind-registry/main.go`,
`go/cmd/eshu/component_schema_versions_test.go`,
`go/internal/storage/postgres/facts_test.go`, and
`go/internal/query/fact_schema_version_test.go` — builds unchanged through
that compat surface today. A caller moves to `cloud.<Name>` directly only
when the importer-migration follow-up on #6776 retires the corresponding
compat entry; this package's contract does not change when that happens.

## Telemetry

None. This package emits no metrics, spans, or logs; it declares constants
and pure lookups only.

## Gotchas / invariants

- No exported identifier stutters the package name (`cloud.AWSFactKinds`,
  not `cloud.CloudAWSFactKinds`); none needed renaming in this move.
- `<Family>FactKinds()` returns a fresh copy (`slices.Clone`) on every
  call; never rely on it returning the package-level backing slice.
- Every kind in this package is schema-version admitted: adding a kind
  requires a matching schema-version constant, a
  `specs/fact-kind-registry.v1.yaml` entry, and a `schemaVersionFamilies`
  row in the facts root once it is wired to reference this package.

## Evidence

No-Regression Evidence (#6950 batch 2, cloud-posture family): this change
moves the sixteen `cloud.*` posture compat entries' Go importers (eight
fact-kind and schema-version constants plus the eight
`<Family>FactKinds`/`<Family>SchemaVersion` accessors) off the transitional
`facts.<Posture>` compat spellings and deletes the emptied
`compat_cloud_posture.go`. No fact-kind string, payload shape, registry
entry, or executable statement changes: across 75 files, every production
hunk requalifies an identifier or import path only, every other hunk is a
package-doc rewording, a ledger row, or the compat file's own deletion, and
the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-cloud-posture-batch2-before, ledger:6950-cloud-posture-batch2-after). The command is `go test -count=1`
over the 18 affected recursive package targets (per-side counts in the
cited rows) on baseline `a0830a6826` vs measurement commit `a3773f91ad`
(this Evidence section, the two ledger rows, and content-identical rebases
tracking main are the only later changes): 436 packages ok, 0 fail on
both sides, with the ok-package set byte-identical after timing strip.
`go test -list` inventory is identical on both sides. Backend/version:
go1.26.6 linux/amd64, in-memory; no backend touched. Input shape: n/a (no
runtime input). Terminal queue/row counts: none — no queue, lease, Cypher,
or SQL path is touched. Contract gates green on the branch:
`verify-fact-kind-registry.sh` (generated artifacts byte-identical),
`verify-factschema-diff.sh` (all four posture schemas, no breaking
changes), `verify-payload-usage-manifest.sh`, and
`verify-contracttest.sh`. The change is safe because it cannot alter
runtime behavior: the compiler resolves the same constants through their
new paths, and the compat deletion is compile-enforced total — any missed
caller would fail the build.

No-Observability-Change (#6950 batch 2, cloud-posture family): this package
carries no instrumentation (see Telemetry above) and the move adds,
removes, or renames no metric, span, structured log, or status field in any
touched package. The collector, reducer, and projector telemetry that reads
and writes facts of these kinds is untouched; operator signals are identical
before and after.

No-Regression Evidence (#6950 batch 4a, cloud kubernetes_live stanza): this
change moves the ten `cloud.*` kubernetes_live compat entries' Go importers
(eight fact-kind and schema-version constants plus the
`KubernetesLiveFactKinds`/`KubernetesLiveSchemaVersion` accessors) off the
transitional `facts.Kubernetes*` compat spellings and deletes the emptied
stanza from `compat_cloud.go`. No fact-kind string, payload shape, registry
entry, or executable statement changes: across 37 files, every production
hunk requalifies an identifier or import path only, every other hunk is this
note, a ledger row, or the stanza's own deletion, and the build resolves
with no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-cloud-batch4a-before, ledger:6950-cloud-batch4a-after). The command is `go test -count=1`
over the 9 affected package targets (per-side counts in the
cited rows) on baseline `ee0d7d5e5b` vs measurement commit `305c506c8d`
(this Evidence note and the two ledger rows are the only later changes):
14 packages ok, 0 fail on both sides, with the ok-package set byte-identical
after timing strip. `go test -list` inventory is identical on both sides
(1431 names). Backend/version: go1.26.9 linux/amd64, in-memory; no backend
touched. Input shape: n/a (no runtime input). Terminal queue/row counts:
none — no queue, lease, Cypher, or SQL path is touched. Contract gates green
on the branch: `verify-fact-kind-registry.sh` (generated artifacts
byte-identical), `verify-factschema-diff.sh` (no breaking changes),
`verify-payload-usage-manifest.sh`, and `verify-contracttest.sh`. The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 4a, cloud kubernetes_live stanza): this
package carries no instrumentation (see Telemetry above) and the move adds,
removes, or renames no metric, span, structured log, or status field in any
touched package. The collector, reducer, and projector telemetry that reads
and writes facts of these kinds is untouched; operator signals are identical
before and after.

No-Regression Evidence (#6950 batch 4b, cloud terraform_state stanza): this
change moves the eighteen `cloud.*` terraform_state compat entries' Go
importers (sixteen fact-kind and schema-version constants plus the
`TerraformStateFactKinds`/`TerraformStateSchemaVersion` accessors) off the
transitional `facts.TerraformState*` compat spellings and deletes the
emptied stanza from `compat_cloud.go`. No fact-kind string, payload shape,
registry entry, or executable statement changes: across 55 files (52 Go
files plus this note, the ledger rows, and one sdk AGENTS.md guidance
line), every production hunk requalifies an identifier or import path only;
the mcp kind-consumer matchers accept the `cloud.` spelling the migration
introduces (per the contract batch 3 established); every other hunk is this
note, a ledger row, a stale-comment reword, or the stanza's own deletion;
and the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-cloud-batch4b-before, ledger:6950-cloud-batch4b-after). The command is `go test -count=1`
over the 8 affected package targets (per-side counts in the
cited rows) on baseline `473a6a757f` vs measurement commit `3e16807853`
(this Evidence note and the two ledger rows are the only later changes):
640 packages ok plus the same single pre-existing host-only
`TestFetchChurnZombiesDrainedByReaper` failure (#7865, fails identically on
the clean base) on both sides, with the ok-package set byte-identical
after timing strip. `go test -list` inventory is identical on both sides
(13479 names). The 16 storage/postgres tests in the two touched test files
pass on both sides. Backend/version: go1.26.9 linux/amd64, in-memory; no
backend touched. Input shape: n/a (no runtime input). Terminal queue/row
counts: none — no queue, lease, Cypher, or SQL path is touched. Contract
gates green on the branch: `verify-fact-kind-registry.sh` (generated
artifacts byte-identical), `verify-factschema-diff.sh` (no breaking
changes), `verify-payload-usage-manifest.sh`, and `verify-contracttest.sh`,
plus `precommit-go.sh surface` (no MCP tool-surface drift). The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 4b, cloud terraform_state stanza): this
package carries no instrumentation (see Telemetry above) and the move adds,
removes, or renames no metric, span, structured log, or status field in any
touched package. The collector, reducer, projector, and query telemetry that
reads and writes facts of these kinds is untouched; operator signals are
identical before and after.

## Related docs

- `docs/public/reference/fact-schema-versioning.md`
- `docs/public/reference/fact-envelope-reference.md`
