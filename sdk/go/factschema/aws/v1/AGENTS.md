# AWS Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `aws` fact family. It must
remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`.
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- Required payload fields are non-pointer, no-`omitempty` struct fields;
  optional fields are pointers or carry `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive from this shape — keep all three in
  agreement (`TestRequiredFieldsMatchStructShape` and
  `TestAWSResourceSchemaHasNoDrift` enforce it).
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam, not a silent edit here.
- This scaffold defines one fact kind (`aws.resource`). Adding a second kind
  or migrating a real fact family is follow-on epic work, not a casual edit.
