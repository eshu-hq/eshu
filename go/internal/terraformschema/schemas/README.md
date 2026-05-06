# Packaged Terraform Provider Schemas

Gzipped Terraform provider schemas loaded by `internal/terraformschema` at
runtime to classify resources and infer identity keys. Files are committed to
git so the runtime is self-contained.

## Naming

`<provider>-<version>.json.gz` — for example `aws-5.100.0.json.gz`,
`google-6.50.0.json.gz`. Exactly one version per provider lives here; the
packaging step removes older versions when it rewrites a file.

## How it is loaded

`terraformschema.DefaultSchemaDir()` resolves to this directory by default
(or honors `ESHU_TERRAFORM_SCHEMA_DIR`). `LoadProviderSchema` accepts both
`.json` and `.json.gz`; the runtime always uses the gzipped form.

## How it is regenerated

1. `scripts/generate_terraform_provider_schema.sh [provider]` runs
   `terraform providers schema -json` against `terraform_providers/<provider>/`
   and writes raw JSON to top-level `schemas/` (gitignored).
2. `scripts/package_terraform_schemas.sh [provider]` reads the resolved
   version from the matching `.terraform.lock.hcl`, gzips the raw JSON, and
   writes `<provider>-<version>.json.gz` here, removing prior versions.

After regenerating, run
`cd go && go test ./internal/terraformschema ./internal/relationships -count=1`.
