# Service Category Classification

Terraform resource categories are broad labels used by parser output and
schema-driven relationship evidence.

Common labels include `compute`, `storage`, `data`, `networking`, `messaging`,
`security`, `cicd`, `monitoring`, `governance`, and `infrastructure`.

## Where Categories Are Used

- `go/internal/parser/hcl` writes `provider`, `resource_service`, and
  `resource_category` on Terraform resource rows.
- `go/internal/relationships` writes category details into Terraform
  relationship evidence.
- `go/internal/terraformschema/categories.go` owns the mapping table.

## Matching Rules

For `aws_cloudwatch_event_rule`:

1. Strip the provider prefix: `cloudwatch_event_rule`.
2. Try the longest service prefix first.
3. Return the mapped category when a prefix exists.
4. Otherwise return service `cloudwatch` and category `infrastructure`.

Longest-prefix matching lets `aws_cloudwatch_event_rule` map to `messaging`
while other `aws_cloudwatch_*` resources can map to `monitoring`.

## Adding Mappings

Edit `serviceCategories` in `go/internal/terraformschema/categories.go`.

Choose the longest stable service prefix that avoids false matches. Do not add
provider-specific category names when an existing broad label works across
providers.

Before changing the table:

- check the provider naming convention in the generated schema
- prefer existing category names
- use the same category for equivalent resources across providers
- add tests for prefixes that could collide with shorter prefixes
- avoid renaming existing category strings without a migration plan

## Verification

```bash
cd go
go test ./internal/terraformschema -count=1
```

If relationship evidence behavior changes, also run:

```bash
cd go
go test ./internal/relationships -count=1
```
