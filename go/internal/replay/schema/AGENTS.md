# schema — agent scope

## Owned surface

- `go/internal/replay/schema/` — the cassette-format JSON Schema builder
  (`schema.go`), the offline cassette validator (`validate.go`), and the two
  committed schema copies.

## Key invariants

- **The schema is generated, never hand-edited.** `CassetteFormatV1()` is the
  single source of truth. Both committed `cassette-format.v1.schema.json` files
  (here and the `sdk/go/collector/schema/` mirror) are regenerated with
  `go test ./internal/replay/schema -run TestCassetteSchemaMatchesGolden -update`
  and committed. Editing the JSON by hand will fail the matches-golden gate.
- **The schema MUST track `format.go`.** The properties the builder declares per
  object must equal the JSON keys the cassette structs serialize
  (`TestSchemaPropertiesMatchCassetteStructs`). When a field is added to
  `cassette.File`/`Scope`/`Fact`, add it to the matching `*Schema()` builder in
  the same change, then regenerate.
- **`required` must equal the loader's enforcement, not a superset.** A field is
  `required` in the schema only if `cassette.File.validate()` rejects its
  absence. `collector` is informational and not required; keep it that way
  unless the loader changes too.
- **`additionalProperties:false` is load-bearing.** It is what turns a
  field-name typo into a validation failure. Do not relax it; the unknown-field
  check in `validate.go` derives its allowed keys from the same structs.
- **Validation MUST stay offline and fast.** No Docker, no graph, no network.
  `ValidateCassetteBytes` is pure CPU over bytes.

## Skill routing

- `golang-engineering` for any Go change here.
- `eshu-golden-corpus-rigor` because the cassette format is what the B-7
  golden-corpus gate replays; a format change ripples to every cassette.
- `generator-script-discipline` when touching `scripts/verify-cassette-author.sh`
  or the regeneration flow.

## Do not

- Hand-edit either committed `cassette-format.v1.schema.json`.
- Let the schema and the cassette structs diverge.
- Add a third-party JSON Schema dependency; the contract is built and enforced
  with the standard library, matching the SDK result-schema precedent.
