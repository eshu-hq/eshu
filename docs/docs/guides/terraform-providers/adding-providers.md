# Adding a New Terraform Provider

This guide shows how to add support for a new Terraform provider to Eshu.

## Prerequisites

- Terraform CLI installed (`terraform` command available)
- Access to the provider on the Terraform Registry
- Git working tree (for committing the schema)

## Step-by-Step Process

### 1. Create the Provider Configuration

Create a directory and `versions.tf` file for the new provider:

```bash
mkdir -p terraform_providers/<provider>
```

**Example:** Adding the Datadog provider

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

**Key points:**
- Provider `source` must match the Terraform Registry namespace (e.g., `datadog/datadog`, `hashicorp/aws`)
- Use a version constraint that pins the major version (e.g., `~> 3.0` means `>= 3.0, < 4.0`)
- The `provider` block with no config is required for schema generation

### 2. Generate the Raw Schema

Run the schema generation script:

```bash
./scripts/generate_terraform_provider_schema.sh <provider>
```

**Example:**

```bash
./scripts/generate_terraform_provider_schema.sh datadog
```

**What this does:**
1. Runs `terraform init` in the provider directory
2. Runs `terraform providers schema -json` to extract the schema
3. Writes the raw JSON to `schemas/<provider>.json`
4. Extracts the version from `.terraform.lock.hcl`

**Expected output:**

```
Generating schema for provider: datadog
Running terraform init...
Extracting schema...
Schema written to schemas/datadog.json
Detected version: 3.47.0
```

### 3. Package the Schema

Compress and version the schema for distribution:

```bash
./scripts/package_terraform_schemas.sh <provider>
```

**Example:**

```bash
./scripts/package_terraform_schemas.sh datadog
```

**What this does:**
1. Reads `schemas/<provider>.json`
2. Extracts the provider version
3. Compresses to `.json.gz`
4. Writes versioned file to `go/internal/terraformschema/schemas/<provider>-<version>.json.gz`
5. Removes the old unversioned schema (if it exists)

**Expected output:**

```
Packaging schema for provider: datadog
Found schema: schemas/datadog.json
Detected version: 3.47.0
Packaged: go/internal/terraformschema/schemas/datadog-3.47.0.json.gz (142.3 KB)
```

### 4. Add Service Category Mappings (Optional)

If the provider has resource types that should be classified into specific
service categories (compute, storage, data, networking, security, etc.), add
mappings to `go/internal/terraformschema/categories.go`.

**Example:** Datadog resources

```go title="go/internal/terraformschema/categories.go"
var serviceCategories = map[string]string{
    // ... existing mappings ...

    // --- Datadog (datadog_ prefix) ---
    "monitor": "monitoring",
    "dashboard": "monitoring",
    "synthetics": "monitoring",
    "logs": "monitoring",
    "integration": "monitoring",
    "downtime": "monitoring",
}
}
```

**Notes:**
- Keys are the service portion *after* stripping the provider prefix (e.g., `datadog_monitor` → `monitor`)
- Longer prefixes are tried first (e.g., `cloudwatch_event` matches before `cloudwatch`)
- Unmapped resource types default to `infrastructure`
- This step is optional — extractors work without category mappings

### 5. Verify Registration

Check that the Go loader and relationship registry accept the new packaged
schema:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

### 6. Run Tests

Verify the schema loads correctly:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

### 7. Commit the Schema

Commit the versioned schema and provider configuration:

```bash
git add go/internal/terraformschema/schemas/datadog-3.47.0.json.gz
git add terraform_providers/datadog/versions.tf
git add go/internal/terraformschema/categories.go  # if you added mappings
git commit -m "feat: add Datadog provider schema (3.47.0, 47 resource types)"
```

### 8. Update Documentation

Add the new provider to the provider list in `docs/docs/guides/terraform-providers/index.md`:

```markdown
| Datadog | 3.47.0 | 47 | `datadog/datadog` |
```

And to `CLAUDE.md`:

```markdown
| Datadog (`datadog/datadog`) | 3.47.0 | 47 |
```

## Troubleshooting

### Schema file is empty or malformed

**Symptom:** `schemas/<provider>.json` is 0 bytes or contains an error message.

**Cause:** `terraform providers schema -json` failed.

**Fix:**
1. Run `terraform init` manually in `terraform_providers/<provider>/`
2. Check for errors in the output
3. Verify the provider source and version are correct in `versions.tf`
4. Try running `terraform providers schema -json` manually to see the error

### Version detection fails (shows "unknown")

**Symptom:** Packaged schema is named `<provider>-unknown.json.gz`

**Cause:** `.terraform.lock.hcl` doesn't exist or doesn't contain the provider version.

**Fix:**
1. Ensure `terraform init` ran successfully
2. Check that `.terraform.lock.hcl` exists in `terraform_providers/<provider>/`
3. Verify the lock file contains the provider entry

### No resource types registered

**Symptom:** Provider schema loads but 0 resource types are registered.

**Cause:** Provider has no resources, only data sources (e.g., `http`, `external`).

**Fix:** This is expected for some utility providers. Data sources are not extracted as relationship evidence.

### Template provider fails to initialize

**Symptom:** `terraform init` fails for the `hashicorp/template` provider.

**Cause:** The `template` provider is deprecated and archived.

**Fix:** Skip the template provider — it has no active resources and is not needed.

## What Happens Next

Once the schema is committed:

1. **Import-time registration:** When `eshu.relationships.terraform_evidence` is imported, the schema is loaded and extractors are auto-registered
2. **Indexing:** When Eshu indexes Terraform files, resource blocks matching the new provider's types are extracted
3. **Relationship resolution:** Extracted identities are matched against repository names/aliases to create cross-repo relationships

No code changes are needed — the schema drives everything.

## Contributing

When contributing a new provider:

1. Ensure the provider is publicly available on the Terraform Registry
2. Use the latest stable version (avoid pre-release versions)
3. Include a brief description of the provider in the commit message
4. Add both the cloud provider list (if applicable) and the partner/community list
5. Run the full test suite before submitting

See [Contributing](../../contributing.md) for general contribution guidelines.
