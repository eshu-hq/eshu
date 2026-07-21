# Codeowners Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload struct for the
`codeowners` fact family. A future reducer or query handler never reads
`Envelope.Payload["some_key"]` for this kind directly; it decodes through the
parent `factschema` package's kind-keyed seam
(`factschema.DecodeCodeownersOwnership`) and receives this struct, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/codeowners/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

One `codeowners` fact kind decodes through this package today:

| Fact kind | Struct | Decode function | Read by |
| --- | --- | --- | --- |
| `codeowners.ownership` | `Ownership` | `factschema.DecodeCodeownersOwnership` | `go/internal/reducer` `codeowners_ownership` domain (projects `DECLARES_CODEOWNER` edges); served via `go/internal/query` + `go/internal/mcp` |

`Ownership` models one CODEOWNERS pattern-to-owners mapping: one line of a
repository's CODEOWNERS file (`repo_id`, `source_path`, `pattern`, `owners`,
`order_index`).

## Phase 1 of issue #5419

This package is Phase 1 of the branch-aware CODEOWNERS ingestion epic
(#5415): the contract only (fact-kind constant, payload struct, JSON schema,
registry entry, fixture pack). No collector emits this fact kind yet, and no
reducer or query handler decodes it yet — those are later phases of the same
issue. `specs/fact-kind-registry.v1.yaml`'s `codeowners` family sets
`read_surface: none` for the same reason: there is no live route to point at
yet.

## Ownership boundary

This package owns the Go type definition for `codeowners.ownership`'s
payload. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_codeowners.go`). It does not own graph/correlation
projection or the collector emitter that will build this payload; those live
outside this module and outside this phase.

## Exported surface

`Ownership`. See its godoc comment for the full field list.

## Dependencies

Standalone: this package has no imports beyond the standard library implied
by its struct tags. It carries no dependency on `go/internal/...` — see the
module `AGENTS.md` for the rule.

## Required vs. optional fields

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit JSON
  null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct.
- **Optional**: a pointer field carrying `omitempty`. An absent optional
  field decodes to nil, not a defaulted zero value.

| Struct | Required fields | Why |
| --- | --- | --- |
| `Ownership` | `RepoID`, `SourcePath`, `Pattern`, `Owners`, `OrderIndex` | A CODEOWNERS line the collector can emit always has all five: a fact-worthy line resolves to a repo, a source file, a pattern, at least one owner token, and a known position in the file. `Owners` is a required (non-`omitempty`) slice — see `intentionalRequiredCollections` in `../../decode_gcp_test.go`. `CollectorInstanceID` stays optional: it identifies the collector run, not the ownership claim. |

## Last-match-wins resolution

CODEOWNERS resolves ownership last-match-wins: for a given path, the LAST
pattern in the file that matches wins. A consumer resolving effective
ownership for a path MUST sort candidate `Ownership` facts by `OrderIndex`
and take the highest-index match, never the first.

## No Attributes pass-through

`Ownership` does not carry a polymorphic `Attributes map[string]any`
pass-through: it is a flat, fully-typed, closed schema.

## Changing this struct

Any field change here is a payload-schema change.

- **Additive optional field** (new pointer/`omitempty` field): a minor schema
  bump. Add the field, regenerate, and commit the schema in the same change.
- **Remove, rename, or narrow a field**: a major schema bump. It needs a
  conversion shim in the parent package's decode seam (`decode.go`,
  `decodeLatestMajor`) — see the module `README.md` — never a silent edit
  here.

Regenerate after any struct change:

```bash
cd sdk/go/factschema
go generate ./...
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` fails the build on drift. The
decode seam derives its required-field set reflectively from the struct's
tags (`../../fields.go`), so there is no separate map to update;
`TestDerivedKeySetsMatchGeneratedSchemas` fails if that reflective set ever
diverges from the generated schema, and `TestPayloadStructShapeConvention`
rejects a field shape that would make "required" ambiguous. Any fixture pack
copy under `../../fixturepack/schema/` must be refreshed in the same change
(`TestFixturePackSchemasMatchCanonical` locks the two).

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- Issue #5419 (branch-aware CODEOWNERS ingestion) and epic #5415.
