# Updating Terraform Provider Versions

This guide shows how to update an existing Terraform provider to a newer version.

## Why Update Providers

- **New resource types:** Newer provider versions often add support for new cloud services
- **Bug fixes:** Schema fixes and improved attribute definitions
- **Security updates:** Provider binaries may include security patches
- **API compatibility:** Match the provider version your Terraform code actually uses

## Update Process

### 1. Edit the Version Constraint

Update the version constraint in `terraform_providers/<provider>/versions.tf`:

**Before:**

```hcl title="terraform_providers/aws/versions.tf"
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.100"
    }
  }
}

provider "aws" {}
```

**After:**

```hcl title="terraform_providers/aws/versions.tf"
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.110"  # Updated
    }
  }
}

provider "aws" {}
```

### 2. Regenerate the Schema

Run the schema generation script:

```bash
./scripts/generate_terraform_provider_schema.sh aws
```

**What happens:**
1. Old `.terraform/` directory is removed
2. `terraform init` downloads the new provider version
3. `terraform providers schema -json` extracts the new schema
4. Version is detected from `.terraform.lock.hcl`

**Expected output:**

```
Generating schema for provider: aws
Running terraform init...
Extracting schema...
Schema written to schemas/aws.json
Detected version: 5.110.0
```

### 3. Package the New Schema

Run the packaging script:

```bash
./scripts/package_terraform_schemas.sh aws
```

**What happens:**
1. Old versioned schema is automatically removed (e.g., `aws-5.100.0.json.gz`)
2. New versioned schema is created (e.g., `aws-5.110.0.json.gz`)
3. Schema is compressed with gzip

**Expected output:**

```
Packaging schema for provider: aws
Found schema: schemas/aws.json
Detected version: 5.110.0
Removed old schema: aws-5.100.0.json.gz
Packaged: go/internal/terraformschema/schemas/aws-5.110.0.json.gz (1.2 MB)
```

### 4. Verify the New Resource Types

Check what changed by rerunning the Go schema and relationship suites:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

### 5. Run Tests

Ensure the new schema loads correctly:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

### 6. Commit the Changes

```bash
git add terraform_providers/aws/versions.tf
git add go/internal/terraformschema/schemas/aws-5.110.0.json.gz
git commit -m "chore: update AWS provider schema to 5.110.0"
```

The old schema file (`aws-5.100.0.json.gz`) is already removed by the packaging script, so it will show as deleted in `git status`.

### 7. Update Documentation

Update the provider version in:

- `docs/docs/guides/terraform-providers/index.md`
- `CLAUDE.md`

**Example:**

```markdown
| AWS | 5.110.0 | 1,542 | `hashicorp/aws` |
```

## Version Comparison

To see what resource types were added or removed between versions, you can compare the schemas:

```bash
# Extract old schema (if you still have it)
gunzip -c go/internal/terraformschema/schemas/aws-5.100.0.json.gz > /tmp/aws-old.json

# Extract new schema
gunzip -c go/internal/terraformschema/schemas/aws-5.110.0.json.gz > /tmp/aws-new.json

# Compare resource counts
echo "Old version:"
cat /tmp/aws-old.json | jq '.provider_schemas[].resource_schemas | keys | length'

echo "New version:"
cat /tmp/aws-new.json | jq '.provider_schemas[].resource_schemas | keys | length'

# Find new resource types
comm -13 \
  <(cat /tmp/aws-old.json | jq -r '.provider_schemas[].resource_schemas | keys[]' | sort) \
  <(cat /tmp/aws-new.json | jq -r '.provider_schemas[].resource_schemas | keys[]' | sort)
```

## Handling Breaking Changes

### Removed Resource Types

If a provider version removes resource types that were previously supported:

1. **Check if they're used:** Search your indexed repositories for references
2. **Document the removal:** Add a note to the commit message
3. **Consider keeping both versions:** You can support multiple provider versions by keeping both schema files (just don't remove the old one in step 3)

### Renamed Resource Types

If a resource type is renamed (e.g., `aws_db_instance` → `aws_rds_instance`):

1. The old type will be automatically removed from registration
2. The new type will be automatically added
3. Historical references to the old type in indexed repos remain in Neo4j
4. Re-index affected repositories to pick up the new type

### Changed Attribute Names

If the identity attribute changes (e.g., `name` → `instance_name`):

1. The extractor will automatically use the new attribute (identity key inference)
2. Re-index affected repositories to update extracted identities
3. Old evidence facts remain in Postgres with the old attribute name

## Frequency Recommendations

- **Major cloud providers (AWS, Azure, GCP):** Update quarterly or when new services you use are added
- **Utility providers (random, tls, time):** Update annually or when bugs are fixed
- **Partner providers (Datadog, PagerDuty):** Update when your team upgrades the provider in production Terraform

## Rollback Process

If a new provider version causes issues:

1. Revert the version constraint in `versions.tf` to the previous version
2. Re-run the generation and packaging scripts
3. Commit the old schema back

The old schema file is preserved in git history, so you can also restore it directly:

```bash
git checkout HEAD~1 -- go/internal/terraformschema/schemas/aws-5.100.0.json.gz
git checkout HEAD~1 -- terraform_providers/aws/versions.tf
```

## Automation

Consider automating provider updates with a CI workflow:

```yaml title=".github/workflows/update-providers.yml"
name: Update Terraform Providers
on:
  schedule:
    - cron: '0 0 1 * *'  # Monthly on the 1st
  workflow_dispatch:

jobs:
  update-providers:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Terraform
        uses: hashicorp/setup-terraform@v3

      - name: Update AWS provider
        run: |
          ./scripts/generate_terraform_provider_schema.sh aws
          ./scripts/package_terraform_schemas.sh aws

      - name: Create Pull Request
        uses: peter-evans/create-pull-request@v5
        with:
          title: "chore: update AWS provider schema"
          body: "Automated provider schema update"
          branch: chore/update-aws-provider
```

## See Also

- [Adding a New Provider](adding-providers.md) — add support for a new provider
- [Service Categories](service-categories.md) — customize resource classifications
- [Configuration Reference](../../reference/configuration.md) — Eshu environment variables
