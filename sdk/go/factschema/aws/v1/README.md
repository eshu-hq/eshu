# AWS Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the `aws`
fact family, the sample family the `factschema` scaffold demonstrates end to
end. A reducer handler never reads `Envelope.Payload["some_key"]` for these
kinds directly; it decodes through the parent `factschema` package's
kind-keyed seam (for example `factschema.DecodeAWSResource`) and receives one
of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)
- Sample fact kind: `aws.resource` (`Resource`)

## Required vs. optional fields

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. `Resource`'s
  `AccountID`, `ResourceID`, `Region`, and `ResourceType` are required — the
  decode seam rejects a payload that omits any of them, or supplies an explicit
  JSON null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct.
- **Optional**: a pointer field or one carrying `omitempty`. `Resource`'s
  `Name` (`*string`) and `Tags` (`*map[string]string`) are optional; an absent
  optional field decodes to nil, not a defaulted zero value.

`Tags` is a pointer to a map so "observed, no tags" stays distinct from "not
observed": a nil pointer is omitted from the payload (not observed), a non-nil
pointer to an empty map marshals as `"tags":{}` and round-trips to a non-nil
empty map (observed, empty), and a populated map round-trips as observed with
tags. A plain map with `omitempty` would collapse the first two states, because
an empty map would be omitted and decode back as nil.

The generated JSON Schema at
`../../schema/aws_resource.v1.schema.json` mirrors this: its `"required"`
array lists exactly the four required fields (the pointer on `Tags` is
transparent to the value schema, so `tags` stays optional).

## Changing a struct

Any field change here is a payload-schema change. Regenerate and commit the
schema in the same change:

```bash
cd sdk/go/factschema
go generate ./...
```

`schema_gen_test.go` fails the build on drift, and
`TestRequiredFieldsMatchStructShape` fails if `decode.go`'s `requiredFields`
map no longer matches the struct. Removing, renaming, or narrowing a field is
a major schema bump and needs a conversion shim in the parent package's
decode seam — see the module `README.md`.
