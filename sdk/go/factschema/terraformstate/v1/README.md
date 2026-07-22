# terraform_state fact payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`terraform_state` fact family, part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` contracts module (Contract System
v1, `docs/internal/design/contract-system-v1.md`). It turns the
`terraform_state_*` fact payloads from unvalidated `map[string]any` into typed,
schema-generated, decode-validated contracts.

## Why

The terraform-state collector emits eight fact kinds; the projector's
source-local canonical extractor (`go/internal/projector/tfstate_canonical.go`)
reads five of them to materialize canonical `TerraformStateResource`,
`TerraformStateModule`, and `TerraformStateOutput` graph nodes. Before typing,
it read each payload key with a raw lookup that returned `""` for an absent
key, so a collector that dropped or renamed an identity key produced an
empty-identity node — or silently dropped the fact — with no operator signal.
Typing the payload makes an absent identity key a classified `input_invalid`
dead-letter instead of silent wrong graph truth (Contract System v1 §3.2).

## Kinds

| Struct | Fact kind | Status | Required (identity/join) |
| --- | --- | --- | --- |
| `Snapshot` | `terraform_state_snapshot` | consumed | *(none — see below)* |
| `Resource` | `terraform_state_resource` | consumed | `address` |
| `Module` | `terraform_state_module` | consumed | `module_address` |
| `Output` | `terraform_state_output` | consumed | `name` |
| `TagObservation` | `terraform_state_tag_observation` | consumed | `resource_address`, `tag_key_hash` |
| `ProviderBinding` | `terraform_state_provider_binding` | consumed (#5446) | `resource_address`, `provider_address` |
| `Candidate` | `terraform_state_candidate` | typed, not yet consumed | `candidate_source`, `backend_kind`, `repo_id`, `relative_path`, `path_hash` |
| `Warning` | `terraform_state_warning` | typed, not yet consumed | `warning_kind`, `reason`, `source` |

### The required set is the identity gate, nothing more

Each consumed kind's required set is exactly the identity/join key whose
ABSENCE breaks the graph identity in the projector today:

- `Resource.Address` anchors the resource node uid and is dropped when empty.
- `Module.ModuleAddress` is the module node uid and aggregation key.
- `Output.Name` is the output node uid key.
- `TagObservation` joins to its resource on both `ResourceAddress` and
  `TagKeyHash`; either absent breaks the join.
- `ProviderBinding` joins to its resource on `ResourceAddress`; the projector
  reads `provider`/`provider_source_address`/`provider_alias` onto the resource
  node (consumed since #5446).
- `Snapshot` has NO required field. The projector reads lineage, serial,
  backend_kind, and locator_hash best-effort and tolerates any being empty (it
  falls back to the scope id for the state path), so no snapshot field's absence
  produces a broken identity. Marking one required would flip a today-valid
  incomplete snapshot into a dead-letter — an accuracy regression the contract
  forbids.

A present-but-empty required value is a VALID decode (an empty observed value
the projector already treats as non-materializable), not a dead-letter. Only an
ABSENT key (or explicit null) dead-letters.

### Typed but not yet consumed

`Candidate` and `Warning` have no read-side decode consumer in the current
codebase (a candidate is discovery provenance, and a warning is routed on fact
kind alone without reading its payload). They are typed here so the contract, schema,
and fixture pack are ready the moment a consumer is added, matching how the GCP
family typed `gcp_image_reference` / `gcp_tag_observation` ahead of their shared
consumer. Their decode-site conversion, `input_invalid` regression test, and
No-Regression benchmark land in the change that first reads each kind — there is
no read path to convert or benchmark in this wave.

## Contract shape

- Required fields are non-pointer with no `omitempty`; the decode seam rejects a
  payload that omits one (or supplies null) with a classified
  `ClassificationInputInvalid` error naming the field.
- Optional fields are pointers/slices/maps with `omitempty`, so an absent value
  decodes to nil and stays distinct from an observed zero.
- These structs are fully typed. A polymorphic payload key no consumer reads
  (an output's raw `value`, a tag's `tag_key`/`tag_value` classification
  envelopes) is intentionally unmodeled: the open schema permits it and the
  decode seam preserves it in the envelope for a future consumer.

## Regeneration

After changing any struct's fields, from the module root
(`sdk/go/factschema`):

```bash
go generate ./...          # or: go run ./internal/schemagen/cmd
```

then copy each regenerated `schema/terraform_state_*.v1.schema.json` to
`fixturepack/schema/` and run `go test ./... -count=1`. The drift-lock tests
(`TestSchemasHaveNoDrift`, `TestFixturePackSchemasMatchCanonical`) fail until
the checked-in artifacts match the structs.
