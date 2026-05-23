# Adding A Terraform Provider

Use this page when a provider is not already packaged under
`go/internal/terraformschema/schemas/`.

## Flow

1. Add `terraform_providers/<provider>/versions.tf`.
2. Generate `schemas/<provider>.json`.
3. Package `go/internal/terraformschema/schemas/<provider>-<version>.json.gz`.
4. Add service-category mappings only when the default category is too generic.
5. Run schema and relationship tests.
6. Update [Terraform Provider Support](index.md).

## Provider Config

```hcl title="terraform_providers/datadog/versions.tf"
terraform {
  required_providers {
    datadog = {
      source  = "datadog/datadog"
      version = "~> 3.0"
    }
  }
}

provider "datadog" {}
```

The provider block can stay empty when the provider exposes its schema without
credentials. If credentials are required just to read the schema, stop and
decide whether the provider belongs in the committed set.

## Generate And Package

```bash
./scripts/generate_terraform_provider_schema.sh datadog
./scripts/package_terraform_schemas.sh datadog
```

The generation script runs `terraform init` and
`terraform providers schema -json`. The packaging script reads the resolved
version from `.terraform.lock.hcl`, gzips the schema, writes the runtime asset,
and removes older packaged schemas for that provider.

The current contract is one committed schema version per provider.

## Category Mapping

Only edit `go/internal/terraformschema/categories.go` when the provider's
resource names need a better category than `infrastructure`.

Use the resource type service part after the first underscore. For example,
`datadog_security_monitoring_rule` can match `security_monitoring`.

## Verification

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

If identity-key behavior changes, add or update identity tests before changing
`identityKeyPatterns` in `schema.go`.
