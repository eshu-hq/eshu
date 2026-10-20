# AWS Collector Catalog

## Purpose

The catalog package is the leaf that the #6696 AWS consolidation moves the
flat `constants_<service>.go` families into, grouped by service category
(`storage.go`, then one file per follow-up category). It exists because the
constants cannot live beside any service: the awscloud root uses the `Service*`
constants itself while every service package imports the root, so either
placement would be an import cycle. A leaf that imports neither side breaks
it.

## Ownership boundary

Owns the AWS service-kind, resource-type, and relationship string constants
only. It does not own scanners, runtime binding, redaction, envelopes, or
graph truth — those stay in `awscloud`, `awscloud/service/<service>`, and the
reducer. It must never import `awscloud`, any `awscloud/service` package, or
the AWS SDK: any of those reintroduces the cycle this package exists to
avoid.

## Exported surface

- `ServiceS3`, `ServiceEFS`, `ServiceFSx`, `ServiceStorageGateway`,
  `ServiceBackup` — storage-category service-kind strings
- `ResourceTypeS3Bucket`, `ResourceTypeEFS*`, `ResourceTypeFSx*`,
  `ResourceTypeStorageGateway*`, `ResourceTypeBackup*` — storage-category
  fact-schema resource-type tokens
- `RelationshipS3*`, `RelationshipEFS*`, `RelationshipFSx*`,
  `RelationshipStorageGateway*`, `RelationshipBackup*` — storage-category
  reducer relationship types

See `doc.go` for the full contract. Each follow-up category lands as one more
`<category>.go` file with the same shape; the remaining ~131 `constants_*`
files stay in the awscloud root until their category lands.

## Dependencies

None. The package imports only the standard library (tests only). That is
the point: anything that imports the awscloud root or a service package
cannot be imported here.

## Telemetry

None. String constants register no metrics, spans, or logs. The scanners
that consume these constants keep their existing counters; see
`docs/public/observability/telemetry-coverage.md`.

## Gotchas / invariants

- A consolidation move must never change a value. `storage_test.go` pins
  every constant byte-identical to its pre-consolidation value; a red test
  there is a behavior change, not a move.

## Consolidation proof

No-Regression Evidence: the storage-category consolidation (Refs #6696)
moves 55 string constants from five `constants_<service>.go` root files into
`catalog/storage.go` verbatim (only the package clause changes); the 149
referencing `.go` files change only the qualifier (146 `awscloud.` to
`catalog.` plus the leaf import; 3 same-package test files qualify previously
bare identifiers). Baseline: the pre-move values read from the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `storage_test.go` (55/55 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the full
affected suite (89 packages plus the awscloud root, awsruntime, and
`go/cmd/collector-aws-cloud`) is green before and after; `go build ./...` and
`go vet` are clean. Backend/version: none — a pure compile-time constant
relocation with no query, graph write, queue, lease, worker, batching, or
Compose/Helm change, so there are no row counts to regress; the input shape is
the fixed 55-symbol set. No-Observability-Change: the leaf declares no
instrument and runs no stage; scanner emission stays covered by
`eshu_dp_aws_resources_emitted_total` and `eshu_dp_aws_scan_duration_seconds`
(AWS cloud row in `docs/public/observability/telemetry-coverage.md`). Safe
because the Go compiler resolves every moved reference, the value-pinning test
fails on any drift, and the relguard/redaction disk-walk derivations were
extended to the catalog directory with their own failing-first tests.
- When a category carrying `Service*` constants lands, extend the
  `go/cmd/collector-aws-cloud` redaction-derivation test's constants walk to
  parse this directory too — it resolves every `runtimebind` `ServiceKind`
  through the parsed table and fails loudly on an unknown constant.
- Keep each category file under the 500-line Go cap; split an oversized
  category before it approaches the limit.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/reference/multi-cloud-collector-contract.md`
- Issue #6696 (consolidation checklist) and epic #6692 (target tree)
