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

Today: the facts root's `compat_cloud.go` and `compat_cloud_posture.go`
import `cloud` and forward every constant and accessor as `facts.<Name>`
(e.g. `facts.AWSFactKinds` calls `cloud.AWSFactKinds`), and
`go/internal/facts/schema_version.go`'s `schemaVersionFamilies` table
references those forwarders. Every external caller that uses
`facts.AWSFactKinds` / `facts.AzureFactKinds` / etc. —
`go/cmd/capability-inventory/surfaces.go`, `go/cmd/fact-kind-registry/main.go`,
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

## Related docs

- `docs/public/reference/fact-schema-versioning.md`
- `docs/public/reference/fact-envelope-reference.md`
