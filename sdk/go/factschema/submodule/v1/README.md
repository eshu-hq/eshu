# Submodule Graph Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload struct for the
`submodule` fact family's one fact kind, `submodule.pin`. A consumer never
reads `Envelope.Payload["some_key"]` for this kind directly; it decodes
through the parent `factschema` package's kind-keyed seam
(`factschema.DecodeSubmodulePin`) and receives a `Pin` struct, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/submodule/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `submodule.pin` | `Pin` | `factschema.DecodeSubmodulePin` |

`Pin` is one git submodule reference declared in a parent repository: a
`.gitmodules` entry, a bare gitlink tree entry with no `.gitmodules`
declaration, or both. It is the join between a parent repository and the
submodule it embeds at one path.

This package (issue #5420) defines the fact-kind constant, this typed
payload struct, its generated JSON Schema, and the registry entry. The git
collector emits `submodule.pin` and the reducer decodes and projects it into
a `Repository-[:PINS_SUBMODULE]->Repository` graph edge. That edge needs no
dedicated read surface (it is queryable through the generic graph tools), so
the registry entry sets `read_surface: none` rather than naming a route with
no consumer behind it.

## Ownership boundary

This package owns the Go type definition for this one fact kind. It does not
own decode dispatch, schema-version routing, or required-field validation —
that lives in the parent `factschema` package (`decode.go`,
`decode_submodule.go`). It does not own graph projection, submodule-edge
materialization, or the collector emitter that builds this payload; those
live in `go/internal/collector/submodule` and
`go/internal/reducer/submodule_pin_materialization.go`, outside this
module.

## Exported surface

`Pin`. See its godoc comment for the full field list and the required/
optional split below.

## Dependencies

Standalone: this package has no imports beyond the standard library implied
by its struct tags. It carries no dependency on `go/internal/...` — see the
module `AGENTS.md` for the rule.

## Required vs. optional fields

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit
  JSON null for one, with a classified `input_invalid` error naming the
  field, never a zero-value struct.
- **Optional**: a pointer field carrying `omitempty`. An absent optional
  field decodes to nil, not a defaulted zero value.

| Struct | Required fields | Why |
| --- | --- | --- |
| `Pin` | `ParentRepoID`, `SubmodulePath` | The join identity a submodule-edge consumer keys off: which parent repository, at which path. |
| `Pin` optional fields | `CollectorInstanceID`, `SubmoduleURL`, `ResolvedRepoID`, `PinnedSHA` | A non-dangling observation can legitimately be missing any one of these: `.gitmodules` with no gitlink has a URL but no `PinnedSHA`; a bare gitlink with no `.gitmodules` entry has a `PinnedSHA` but no `SubmoduleURL`; `ResolvedRepoID` is nil whenever the URL is unresolved, ambiguous, or dangling. |

The constraint that a real emitted fact must carry at least one of
`SubmoduleURL` or `PinnedSHA` (never neither) is a collector-side emission
rule (`go/internal/collector/submodule`), not a schema constraint enforced
here.

## Changing this struct

Any field change here is a payload-schema change.

- **Additive optional field** (new pointer/`omitempty` field): a minor
  schema bump. Add the field, regenerate, and commit the schema in the same
  change.
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
rejects a field shape that would make "required" ambiguous. The fixture pack
copy under `../../fixturepack/schema/` must be refreshed in the same change
(`TestFixturePackSchemasMatchCanonical` locks the two).

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- The git collector emits `submodule.pin`
  (`go/internal/collector/submodule`, wired into
  `go/internal/collector/git_submodule_facts.go`) and the reducer decodes
  and projects it (`decodeSubmodulePin`,
  `go/internal/reducer/submodule_pin_materialization.go`) into
  `Repository-[:PINS_SUBMODULE]->Repository` graph edges (issue #5420).
- `read_surface: none` in `specs/fact-kind-registry.v1.yaml` is intentional,
  not a placeholder: this fact feeds a graph edge queryable through the
  generic graph tools, not a dedicated route.
- The reducer decodes only the latest struct for this kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
