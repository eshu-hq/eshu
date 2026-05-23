# internal/terraformschema Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `schema.go` before changing loading, identity keys, or classification.
3. `categories.go` before changing service/category mappings.
4. `paths.go` before changing `ESHU_TERRAFORM_SCHEMA_DIR` behavior.
5. `schemas/README.md` before adding or regenerating packaged schemas.
6. `go/internal/relationships/terraform_schema.go` before changing
   schema-driven extractor behavior.

## Local Rules

- Keep this package a leaf dependency. Do not import Eshu-internal packages.
- Keep it standard-library-only.
- `LoadProviderSchema` returns `(nil, nil)` for absent or unparseable schema
  files. Callers must handle nil schemas; absence is not fatal to evidence
  extraction.
- Provider keys must stay sorted before resource types are built.
- Gzip detection is suffix-based: `.gz` uses gzip, anything else is plain JSON.
- Disk schema loading and embedded schema loading must stay equivalent for
  packaged runtime binaries.
- Merge metadata-nested attributes before identity-key inference.
- Identity-key pattern order matters; first match wins.
- Classification tables are curated truth. Do not infer service/category truth
  from resource names without tests.
- `ESHU_TERRAFORM_SCHEMA_DIR` is the supported override for focused tests and
  local schema experiments; container runtime must not depend on source-tree
  paths.

## Change Gates

- New provider schemas must follow `schemas/README.md`, use generated
  `terraform providers schema -json` output, and pass
  `go test ./internal/terraformschema ./internal/relationships -count=1`.
- New service/category mappings require classification tests and longest-prefix
  review.
- New identity patterns require identity tests and ordering review.
- Nested identity block changes require tests proving top-level attributes are
  not shadowed.

## Do Not Change Without Owner Review

- `identityKeyPatterns` order or semantics.
- Stored category/service labels used by graph queries or API responses.
- `ESHU_TERRAFORM_SCHEMA_DIR` semantics.
- Suffix-based gzip contract.
