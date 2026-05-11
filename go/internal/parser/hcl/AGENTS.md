# AGENTS.md - internal/parser/hcl guidance

## Read first

1. `README.md` - package boundary, payload buckets, and invariants
2. `doc.go` - godoc contract for the HCL package
3. `parser.go` - `Parse`, Terraform block extraction, and Terragrunt row extraction
4. `helpers.go` - Terragrunt helper-path regex extraction
5. `expression_helpers.go` - bounded Terragrunt expression resolution
6. `values.go` - HCL expression and object value formatting helpers
7. `terraform_resource_attributes.go` - cty-value evaluation for drift attribute extraction
8. `lexical_helpers.go` - comment, delimiter, and repository-relative path helpers
9. `../hcl_terraform_test.go` - Terraform behavior coverage through the parent engine
10. `../hcl_terragrunt_test.go` - Terragrunt payload and helper-path coverage
11. `../hcl_terragrunt_join_additional_test.go` - additional Terragrunt expression coverage

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import `internal/parser`.
- `Parse` preserves the legacy Terraform and Terragrunt payload buckets exactly.
- `terragrunt.hcl` uses the Terragrunt path; other HCL files use the Terraform
  block path.
- Bucket output must stay deterministic. Keep map iteration behind sorted key
  helpers before appending rows.
- Terragrunt helper extraction is bounded static evidence. Do not add broad
  expression evaluation without fixture proof.

## Common changes and how to scope them

- Add Terraform block metadata by writing or updating a focused test in
  `../hcl_terraform_test.go` first.
- Add Terragrunt helper-expression support by writing or updating a focused
  test in `../hcl_terragrunt_test.go` or
  `../hcl_terragrunt_join_additional_test.go` first.
- Keep registry dispatch and parent engine method signatures in `../engine.go`
  and `../hcl_language.go`.
- Keep shared parser helpers in `internal/parser/shared`; do not copy them into
  this package.

## Failure modes and how to debug

- Missing Terraform resources usually means the block label arity check did not
  match the HCL form.
- Missing provider source/version metadata usually means the
  `required_providers` object was not parsed into the provider metadata map.
- Missing Terragrunt config asset paths usually means the expression shape is
  outside the bounded resolver in `expression_helpers.go`.
- Query regressions usually mean the payload bucket names or row fields drifted
  from the legacy parent-parser shape.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse unexported helpers.
- Evaluating arbitrary Terraform or Terragrunt expressions as if parser
  evidence were runtime truth.
- Adding repository-specific Terragrunt conventions without fixture evidence.
- Returning partial payloads on HCL parse errors.
- Reading raw source bytes to extract attribute values for drift comparison.
  Heredocs and escaped-quote strings produce wrong values when read as bytes;
  use `literalAttributeValue` which calls `hclsyntax.Expression.Value(nil)`
  instead (`terraform_resource_attributes.go`).

## What NOT to change without an ADR

- Do not reassign HCL file extensions or exact filename routing. That belongs
  to the parent parser registry and changes fact idempotency for existing repos.
- Do not move Terraform relationship interpretation into this package. It owns
  parser evidence only.
