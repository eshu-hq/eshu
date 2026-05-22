# Adding A Terraform Provider

Use this page when a provider is not already packaged under
`go/internal/terraformschema/schemas/`.

Provider support is schema-driven. The normal change is:

1. add `terraform_providers/<provider>/versions.tf`
2. generate `schemas/<provider>.json`
3. package `go/internal/terraformschema/schemas/<provider>-<version>.json.gz`
4. add service-category mappings only when the default category would be too
   generic
5. run the schema and relationship tests

## Provider Config

Create `terraform_providers/<provider>/versions.tf` with the Terraform Registry
source and a stable version constraint:

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

The provider block can stay empty when the provider can initialize without
credentials. If a provider requires configuration just to expose its schema,
stop and decide whether it belongs in the committed provider set.

## Generate And Package

The scripts run from the repository root:

```bash
./scripts/generate_terraform_provider_schema.sh datadog
./scripts/package_terraform_schemas.sh datadog
```

`generate_terraform_provider_schema.sh` runs `terraform init` in
`terraform_providers/<provider>/`, then writes raw JSON to
`schemas/<provider>.json`. The script uses `jq` to print the resource count, so
both Terraform and `jq` must be installed.

`package_terraform_schemas.sh` reads the resolved version from
`terraform_providers/<provider>/.terraform.lock.hcl`, gzips the raw schema, and
writes `go/internal/terraformschema/schemas/<provider>-<version>.json.gz`.
It removes older packaged schemas for the same provider. Keep exactly one
packaged version per provider unless the loader and docs are changed together.

## Category Mapping

Only edit `go/internal/terraformschema/categories.go` when the provider's
resource types need a better category than `infrastructure`.

```go title="go/internal/terraformschema/categories.go"
var serviceCategories = map[string]string{
	// Existing mappings...
	"monitor":             "monitoring",
	"dashboard":           "monitoring",
	"synthetics":          "monitoring",
	"security_monitoring": "security",
}
```

Use the resource type service part after the first underscore:
`datadog_security_monitoring_rule` can match `security_monitoring`. The
classifier tries the longest prefix first, then shorter prefixes, then falls
back to the first service token. Unknown services become category
`infrastructure`.

## Runtime Path

The packaged schema is loaded by `go/internal/terraformschema`. The
relationship extractor bootstrap in `go/internal/relationships` scans schema
files with `*.json*`, infers identity keys from string attributes, classifies
the resource category, and registers Terraform evidence extractors.

No provider-specific extractor code is needed for ordinary resource types.
Add code only when the schema-driven path cannot express the evidence safely.

## Verification

Run the focused gate after adding the provider or changing categories:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

If category mappings changed, make sure the classify tests cover the new
prefixes. If identity-key behavior changed, add or update identity tests before
changing `identityKeyPatterns` in `schema.go`.

Update `docs/public/guides/terraform-providers/index.md` with the packaged
provider version and resource count after the schema is regenerated.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| `schemas/<provider>.json` is empty or invalid | Run `terraform init` and `terraform providers schema -json` manually in `terraform_providers/<provider>/`. |
| Packaged file ends in `unknown.json.gz` | Confirm `.terraform.lock.hcl` exists and contains a resolved provider version. |
| No resource types register | The provider may expose only data sources. Relationship evidence is registered from resource schemas. |
| A known resource stays `infrastructure` | Add the longest stable service prefix to `serviceCategories` and test it. |
