# Updating Terraform Provider Versions

Use this page when a packaged provider schema under
`go/internal/terraformschema/schemas/` needs to move to a newer provider
version.

Provider updates are data changes, but they affect runtime extraction. A new
schema can add resource types, remove resource types, or change which string
attribute Eshu treats as the resource identity.

## Update Flow

1. Edit `terraform_providers/<provider>/versions.tf`.
2. Regenerate the raw schema.
3. Package the versioned `.json.gz` schema.
4. Review resource-count and category/identity changes.
5. Run the focused Go tests.
6. Update `docs/public/guides/terraform-providers/index.md` with the new
   version and count.

Example version change:

```hcl title="terraform_providers/aws/versions.tf"
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.110"
    }
  }
}

provider "aws" {}
```

Run the scripts from the repository root:

```bash
./scripts/generate_terraform_provider_schema.sh aws
./scripts/package_terraform_schemas.sh aws
```

The packaging script removes older packaged schemas for the same provider and
writes `go/internal/terraformschema/schemas/<provider>-<version>.json.gz`.
The current packaging contract is one committed schema version per provider.

## Compare Resource Types

When reviewing an update, compare the previous and new resource sets before
assuming the change is harmless:

```bash
gunzip -c go/internal/terraformschema/schemas/aws-5.100.0.json.gz > /tmp/aws-old.json
gunzip -c go/internal/terraformschema/schemas/aws-5.110.0.json.gz > /tmp/aws-new.json

jq '.provider_schemas[].resource_schemas | keys | length' /tmp/aws-old.json
jq '.provider_schemas[].resource_schemas | keys | length' /tmp/aws-new.json

comm -13 \
  <(jq -r '.provider_schemas[].resource_schemas | keys[]' /tmp/aws-old.json | sort) \
  <(jq -r '.provider_schemas[].resource_schemas | keys[]' /tmp/aws-new.json | sort)
```

Use the actual old and new filenames from
`go/internal/terraformschema/schemas/`. The old file disappears after packaging,
so restore it from the previous commit or compare before packaging when you
need a local diff.

## What To Review

| Change | Why it matters |
| --- | --- |
| Added resource types | New schema-driven evidence can appear after reindexing. |
| Removed resource types | Existing Terraform files using removed types will no longer get schema-driven evidence from the packaged schema. |
| Renamed resource types | Old indexed evidence remains until affected repositories are reindexed. |
| Attribute shape changes | `InferIdentityKeys` only uses string attributes from `identityKeyPatterns` or sorted `*_name` / `*_identifier` fallbacks. |
| Category drift | New services may default to `infrastructure` until `categories.go` is updated. |

Data sources are not registered as relationship evidence. A provider with zero
resource schemas can still be a valid Terraform provider, but it does not add
schema-driven relationship coverage.

## Verification

Run the focused provider-schema gate:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

If parser resource classification is part of the change, also run the focused
HCL parser tests:

```bash
cd go
go test ./internal/parser -run 'HCL|Terraform' -count=1
```

If a new service prefix was added, include or update classifier coverage in
`go/internal/terraformschema/classify_test.go`.

## Rollback

Prefer a normal git revert for a bad provider update. If you need to rebuild
the old schema instead, restore the previous `versions.tf`, regenerate, package,
and rerun the same focused tests. Do not keep two packaged versions for one
provider unless the runtime loader contract is intentionally changed.
