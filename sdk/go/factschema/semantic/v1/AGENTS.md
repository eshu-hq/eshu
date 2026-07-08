# Semantic Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds
schema-version-1 typed payload structs for optional semantic evidence facts.

## Required Checks

- Keep this module independent from `github.com/eshu-hq/eshu/go/internal/...`.
- After changing payload struct fields, run `go generate ./...` from
  `sdk/go/factschema`, refresh `fixturepack/schema/`, update fixture payloads,
  and run `go test ./... -count=1`.
- Required fields are non-pointer fields without `omitempty`; optional fields
  use pointers, slices, or maps with `omitempty`.

## Contract Rules

- Semantic facts are evidence, not canonical truth. Do not add fields that imply
  deterministic promotion without a reducer design.
- Keep provider fields credential-free: profile IDs, provider kind, model ID,
  and endpoint profile ID only.
- `DocumentationObservation` and `CodeHint` remain optional semantic evidence;
  missing required provenance must decode as `ClassificationInputInvalid`.
