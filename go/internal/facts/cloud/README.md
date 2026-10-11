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

Today: every caller uses the `cloud.<Name>` spelling directly —
`go/internal/facts/schema_version.go`'s `schemaVersionFamilies` table,
`go/cmd/capability-inventory/surfaces.go`,
`go/cmd/fact-kind-registry/main.go`, and all collector, reducer,
projector, storage, query, and mcp callers. The transitional
`facts.<Name>` compat surface (`compat_cloud.go`,
`compat_cloud_posture.go`) was retired in #6950 once the last caller
moved: the posture forwarders, the gcp stanza, the small-AWS entries
(`AWSFactKinds` plus the DNSRecord, ImageReference,
ResourcePolicyPermission, SecurityGroupRule, and TagObservation kinds),
the `AWSResource` / `AWSRelationship` entries, and finally the
IAMPermission / Warning kinds plus `AWSSchemaVersion` (batch 5d, which
deleted `compat_cloud.go` itself). No `facts.<Name>` spelling remains
for any name this package owns.

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

No-Regression Evidence (#6950 batch 4c, cloud azure stanza): this change
moves the eighteen `cloud.*` azure compat entries' Go importers (sixteen
fact-kind and schema-version constants plus the `AzureFactKinds` /
`AzureSchemaVersion` accessors) off the transitional `facts.Azure*` compat
spellings and deletes the emptied stanza from `compat_cloud.go`. No
fact-kind string, payload shape, registry entry, or executable statement
changes: across 77 files (76 code and guidance files plus the ledger
rows; this note extends the README hunk), every production hunk
requalifies an identifier or import path only; the mcp route marker
for the azure inventory domain names the
`cloud.` spelling the migration introduces (per the batch-3 `docs.` marker
precedent); every other hunk is this note, a ledger row, a stale-comment or
guidance reword, or the stanza's own deletion; and the build resolves with
no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-cloud-batch4c-before, ledger:6950-cloud-batch4c-after). The command is `go test -count=1`
over the 10 affected package targets (per-side counts in the
cited rows) on baseline `aa3f6c9e5a` vs measurement commit `49a260851b`
(this Evidence note and the two ledger rows are the only later changes):
690 packages ok plus the same single pre-existing host-only
`TestFetchChurnZombiesDrainedByReaper` failure (#7865, fails identically on
the clean base) on both sides, with the ok-package set byte-identical
after timing strip. `go test -list` inventory is identical on both sides
(15658 names). Backend/version: go1.26.9 linux/amd64, in-memory; no
backend touched. Input shape: n/a (no runtime input). Terminal queue/row
counts: none — no queue, lease, Cypher, or SQL path is touched. Contract
gates green on the branch: `verify-fact-kind-registry.sh` (generated
artifacts byte-identical), `verify-factschema-diff.sh` (no breaking
changes), `verify-payload-usage-manifest.sh`, and `verify-contracttest.sh`,
plus `precommit-go.sh surface` (no MCP tool-surface drift). The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 4c, cloud azure stanza): this package
carries no instrumentation (see Telemetry above) and the move adds, removes,
or renames no metric, span, structured log, or status field in any touched
package. The collector, reducer, projector, storage, and mcp telemetry that
reads and writes facts of these kinds is untouched; operator signals are
identical before and after.

No-Regression Evidence (#6950 batch 5a, cloud gcp stanza): this change
moves the sixteen `cloud.*` gcp compat entries' Go importers (fourteen
fact-kind and schema-version constants plus the `GCPFactKinds` /
`GCPSchemaVersion` accessors) off the transitional `facts.GCP*` compat
spellings and deletes the emptied stanza from `compat_cloud.go`. No
fact-kind string, payload shape, registry entry, or executable statement
changes: across 144 files (138 Go caller files plus `schema_version.go`,
the stanza deletion, and four comment/guidance rewords; this note and the
ledger rows ride in the follow-up docs-only commit), every production hunk
requalifies an identifier or import path only; the mcp route marker
for the gcp inventory domain names the
`cloud.` spelling the migration introduces (per the batch-3 `docs.` marker
precedent); every other hunk is this note, a ledger row, a stale-comment or
guidance reword, or the stanza's own deletion; and the build resolves with
no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-cloud-batch5a-before, ledger:6950-cloud-batch5a-after). The command is `go test -count=1 ./...`
over the full tree on baseline `60f526c46` vs measurement commit `12c85858a`
(this Evidence note and the two ledger rows are the only later changes):
1085 packages ok plus the same single pre-existing host-only
`TestFetchChurnZombiesDrainedByReaper` failure (#7865, fails identically on
the clean base) on both sides, with the ok-package set byte-identical
after timing strip. `go test -list` inventory is identical on both sides
(31959 names: 31606 Test + 353 Bench). Backend/version: go1.26.9
linux/amd64, in-memory; no backend touched. Input shape: n/a (no
runtime input). Terminal queue/row counts: none — no queue, lease, Cypher,
or SQL path is touched. Contract gates green on the branch:
`verify-fact-kind-registry.sh` (generated artifacts byte-identical),
`verify-factschema-diff.sh` (no breaking changes),
`verify-payload-usage-manifest.sh`, and `verify-contracttest.sh`,
plus `precommit-go.sh surface` (no MCP tool-surface drift). The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 5a, cloud gcp stanza): this package
carries no instrumentation (see Telemetry above) and the move adds, removes,
or renames no metric, span, structured log, or status field in any touched
package. The collector, reducer, projector, storage, and mcp telemetry that
reads and writes facts of these kinds is untouched; operator signals are
identical before and after.

No-Regression Evidence (#6950 batch 5b, cloud small-AWS entries): this change
moves the ten small-AWS `cloud.*` compat entries' Go importers (nine
constants — five fact-kind constants plus four schema-version constants —
plus the `AWSFactKinds` accessor) off the transitional `facts.AWS*` compat
spellings and deletes the emptied entries from `compat_cloud.go`. No
fact-kind string, payload shape, registry entry, or executable statement
changes: across 49 files (46 Go caller files plus `schema_version.go`,
`schema_version_test.go`, and the ten-entry deletion; this note, the two
ledger rows, and two stale-guidance rewords ride in the follow-up
docs-only commit), every production hunk requalifies an identifier or
import path only, and the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6950-cloud-batch5b-before, ledger:6950-cloud-batch5b-after). The command is `go test -count=1 ./...`
over the full tree on baseline `ac9644c96` vs measurement commit `eeb5f9488`
(this Evidence note, the two ledger rows, and the two guidance rewords are
the only later changes): 1086 packages ok plus the same single pre-existing
host-only `TestFetchChurnZombiesDrainedByReaper` failure (#7865, fails
identically on the clean base) on both sides, with the ok-package set
byte-identical after timing strip. `go test -list` inventory is identical
on both sides (32039 names: 31686 Test + 353 Bench). Backend/version:
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

No-Observability-Change (#6950 batch 5b, cloud small-AWS entries): this package
carries no instrumentation (see Telemetry above) and the move adds, removes,
or renames no metric, span, structured log, or status field in any touched
package. The collector, reducer, projector, storage, and mcp telemetry that
reads and writes facts of these kinds is untouched; operator signals are
identical before and after.

No-Regression Evidence (#6950 batch 5c): this change moves the two big
AWS collector kinds' Go importers
(`AWSResourceFactKind`, `AWSResourceSchemaVersion`,
`AWSRelationshipFactKind`, `AWSRelationshipSchemaVersion`) off the
transitional `facts.AWS*` compat spellings and deletes the four emptied
entries from `compat_cloud.go`. No fact-kind string, payload shape,
registry entry, or executable statement changes: across 290 files (289 Go
caller files plus the four-entry deletion; this note, the two ledger rows,
and the stale-guidance rewords ride in the follow-up docs-only commit),
every production hunk requalifies an identifier or import path only, and
the build resolves with no dangling reference. Measurement: identical
before/after outcomes (ledger:6950-cloud-batch5c-before,
ledger:6950-cloud-batch5c-after). The command is `go test -count=1 ./...`
over the full tree on baseline `64f6600d9` vs measurement commit
`df95f6ee7` (this Evidence note, the two ledger rows, and the
stale-guidance rewords are the only later changes): 1087 packages ok plus
the same single pre-existing host-only `TestFetchChurnZombiesDrainedByReaper`
failure (#7865, fails identically on the clean base and on the untouched
main checkout) on both sides, with the ok-package set byte-identical after
timing strip. `go test -list` inventory is identical on both sides (32100
names: 31746 Test + 354 Bench). The runtime `FactKindRegistry()` dump is
byte-identical before/after (182 entries). Backend/version: go1.26.2
linux/amd64, in-memory; no backend touched. Input shape: n/a (no runtime
input). Terminal queue/row counts: none — no queue, lease, Cypher, or SQL
path is touched. Contract gates green on the branch:
`verify-fact-kind-registry.sh` (generated artifacts current),
`verify-factschema-diff.sh` (no breaking changes),
`verify-payload-usage-manifest.sh`, `verify-contracttest.sh` (contract
data current), and `verify-openapi.sh` (261 HandleFunc routes, 261 OpenAPI
path entries). The change is safe because it cannot alter runtime
behavior: the compiler resolves the same constants through their new
paths, and the compat deletion is compile-enforced total — any missed
caller would fail the build.

No-Observability-Change (#6950 batch 5c): this package carries no
instrumentation (see Telemetry above)
and the move adds, removes, or renames no metric, span, structured log, or
status field in any touched package. The collector, reducer, projector,
storage, and mcp telemetry that reads and writes facts of these kinds is
untouched; operator signals are identical before and after.

No-Regression Evidence (#6950 batch 5d): this change moves the last
`cloud.*` compat entries' Go importers (the IAMPermission and Warning
fact-kind and schema-version constants plus the `AWSSchemaVersion`
accessor) off the transitional `facts.AWS*` spellings and deletes
`compat_cloud.go` itself. No fact-kind string, payload shape, registry
entry, or executable statement changes: across 88 files (83 caller
files plus the file deletion and 4 package-doc rewords; this note, the
two ledger rows, and the guidance rewords ride in the follow-up
docs-only commit), every production hunk requalifies an identifier or
import path only, and the build resolves with no dangling reference.
Measurement: see ledger:6950-cloud-batch5d-before and
ledger:6950-cloud-batch5d-after for identical before/after outcomes. The
command is `go test -count=1 ./...` over the full tree on baseline
`f3d560021` vs measurement commit `1d4ba3658` (this Evidence note, the
two ledger rows, and the guidance rewords are the only later changes):
1087 packages ok plus the same single pre-existing host-only
`TestFetchChurnZombiesDrainedByReaper` failure (#7865, fails identically
on the clean base) on both sides, with the ok-package set byte-identical
after timing strip. `go test -list` inventory is identical on both sides
(32101 names: 31747 Test + 354 Bench). The runtime `FactKindRegistry()`
dump is byte-identical before/after (182 entries). Backend/version:
go1.26.2 linux/amd64, in-memory; no backend touched. Input shape: n/a
(no runtime input). Terminal queue/row counts: none — no queue, lease,
Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-fact-kind-registry.sh` (generated artifacts current),
`verify-factschema-diff.sh` (no breaking changes),
`verify-payload-usage-manifest.sh`, `verify-contracttest.sh` (contract
data current), `verify-package-docs.sh`, and `verify-openapi.sh` (261
HandleFunc routes, 261 OpenAPI path entries). The change is safe because
it cannot alter runtime behavior: the compiler resolves the same
constants through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6950 batch 5d): this package carries no
instrumentation (see Telemetry above) and the move adds, removes, or
renames no metric, span, structured log, or status field in any touched
package. The collector, reducer, projector, storage, and mcp telemetry
that reads and writes facts of these kinds is untouched; operator
signals are identical before and after.

## Related docs

- `docs/public/reference/fact-schema-versioning.md`
- `docs/public/reference/fact-envelope-reference.md`
