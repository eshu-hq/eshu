# internal/terraformschema

## Purpose

`internal/terraformschema` loads packaged Terraform provider schemas and maps
provider resource types to Eshu service/category labels and identity keys.

## Ownership boundary

This package owns schema loading and classification helpers. It does not parse
Terraform configuration, read Terraform state, choose cloud credentials, or
write facts. Parser and Terraform-state collector packages consume its
normalized schema data.

## Exported surface

See `doc.go` for the contract. Exported surfaces are `AttributeSchema`,
`ProviderSchemaInfo`, `LoadProviderSchema`, `InferIdentityKeys`,
`ClassifyResourceCategory`, `ClassifyResourceService`, `DefaultSchemaDir`, and
`EmbeddedSchemasFS`.

## Dependencies

The package uses generated or packaged provider-schema JSON plus embedded
schema files. `ESHU_TERRAFORM_SCHEMA_DIR` overrides the source-tree schema
directory for focused tests and local schema experiments.

## Telemetry

The package emits no metrics or spans directly. Callers such as the
Terraform-state collector own schema resolver gauges and warning counters.

## Gotchas / invariants

- Keep disk schema loading and embedded schema loading equivalent for packaged
  runtime binaries.
- Metadata-nested attributes must be merged before identity-key inference.
- Classification tables are curated contracts; do not infer service truth from
  resource names without tests.
- Schema directory overrides are for local and test use. Do not make runtime
  behavior depend on a source-tree path in containers.

## Focused tests

```bash
cd go
go test ./internal/terraformschema -run 'Test.*Schema|Test.*Identity|Test.*Classif|Test.*DefaultSchemaDir|Test.*Embedded' -count=1
go test ./internal/terraformschema -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/reference/local-testing.md`
- `go/internal/collector/terraformstate/README.md`
