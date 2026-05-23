# Updating Terraform Provider Versions

Use this page when a packaged schema under
`go/internal/terraformschema/schemas/` needs to move to a newer provider
version.

Provider updates are data changes, but they affect runtime extraction. A new
schema can add resources, remove resources, or change which string attribute
Eshu treats as the resource identity.

## Flow

1. Edit `terraform_providers/<provider>/versions.tf`.
2. Regenerate the raw schema.
3. Package the versioned `.json.gz` schema.
4. Review resource-count, category, and identity changes.
5. Run focused Go tests.
6. Update [Terraform Provider Support](index.md).

```bash
./scripts/generate_terraform_provider_schema.sh aws
./scripts/package_terraform_schemas.sh aws
```

The packaging script removes older packaged schemas for the same provider. Keep
one committed schema version per provider unless the loader contract changes.

## Review Checklist

| Change | Why it matters |
| --- | --- |
| Added resource types | New schema-driven evidence can appear after reindexing. |
| Removed resource types | Existing Terraform files may stop producing schema-driven evidence for those types. |
| Renamed resource types | Old indexed evidence remains until affected repositories are reindexed. |
| Attribute shape changes | `InferIdentityKeys` only uses known string attributes and sorted `*_name` or `*_identifier` fallbacks. |
| Category drift | New services may default to `infrastructure` until `categories.go` is updated. |

Data sources are not registered as relationship evidence.

## Verification

```bash
cd go
go test ./internal/terraformschema ./internal/relationships -count=1
```

If parser resource classification is part of the change, also run:

```bash
cd go
go test ./internal/parser -run 'HCL|Terraform' -count=1
```

Rollback should be a normal git revert unless the runtime loader contract has
changed.
