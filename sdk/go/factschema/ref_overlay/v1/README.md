# Ref Overlay Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`ref_overlay` fact family's two fact kinds,
`ref_overlay.branch_observation` and `config_key_divergence`. A consumer
never reads `Envelope.Payload["some_key"]` for these kinds directly; it
decodes through the parent `factschema` package's kind-keyed seams
(`factschema.DecodeRefOverlayBranchObservation`,
`factschema.DecodeConfigKeyDivergence`) and receives a validated struct.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/ref_overlay/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `ref_overlay.branch_observation` | `BranchObservation` | `factschema.DecodeRefOverlayBranchObservation` |
| `config_key_divergence` | `ConfigKeyDivergence` | `factschema.DecodeConfigKeyDivergence` |

`BranchObservation` is one deployment-referenced non-default ref: the branch,
tag, or commit a deployment actually names, plus its resolution state. It is
the fact side of the `RefSnapshot` join the graph gains in a sibling Phase 1
leaf of epic #5393.

`ConfigKeyDivergence` is one config key's divergence state for one
environment on one tracked ref: whether the key's effective value there
differs from the default branch. It carries keys plus a per-env divergence
boolean only — no values, no hashes — so a divergence answer can never leak
a secret.

This package (epic #5393, Phase 1) defines the fact-kind constants, the typed
payload structs, their generated JSON Schemas, and the registry entry. The
ref-extraction emitter and the ref-scoped read surfaces land in sibling
Phase 1 leaves; until they do, the registry entry sets
`read_surface: none` rather than naming a route with no consumer behind it.

## Ownership boundary

This package owns the Go type definitions for these two fact kinds. It does
not own decode dispatch, schema-version routing, or required-field validation
— that lives in the parent `factschema` package (`decode.go`,
`decode_ref_overlay.go`). It does not own the ref-extraction emitter, the
config comparison, reducer/query consumption, or the divergence read surface;
those live in `go/internal/...`, outside this module.

## Exported surface

`BranchObservation`, `ConfigKeyDivergence`, the `RefKind*`,
`Resolvability*`, and `ValueKind*` closed value sets, and
`StripURLUserinfo`. See the godoc comments for the full field lists and the
required/optional split below.

## Dependencies

Standalone: this package imports only the standard library (`net/url`, for
the userinfo normalization). It carries no dependency on `go/internal/...` —
see the module `AGENTS.md` for the rule.

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
| `BranchObservation` | `RepoID`, `Ref`, `RefKind`, `Resolvability` | The overlay identity (`repo_id` + `ref`), the git object class selecting the observation path, and the resolution state. An observation missing any of these is not attributable to any overlay. |
| `BranchObservation` optional fields | `CollectorInstanceID`, `HeadSHA` | Run metadata, and the resolved commit — nil whenever the ref is unresolved or the fetch failed (an unreachable ref reads unknown, never the default branch's SHA). |
| `ConfigKeyDivergence` | `RepoID`, `Ref`, `ConfigKey`, `Env`, `Diverged` | The comparison identity (which repo, ref, key, environment) and its verdict. A row missing any of these is not attributable to any comparison. |
| `ConfigKeyDivergence` optional fields | `CollectorInstanceID`, `ValueKind` | Run metadata, and the diverged value's shape class recorded without the value itself. |

The emitter-side rules — suppress sensitive key names before emission (the
collector's sensitive-key rule), strip userinfo from URL-shaped values before
comparing or classifying (`StripURLUserinfo`) — are emission constraints
enforced where facts are built, not schema constraints enforced here.

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
copies under `../../fixturepack/schema/` must be refreshed in the same change
(`TestFixturePackSchemasMatchCanonical` locks the two).

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- `config_key_divergence` keeps the exact bare wire string epic #5393 names —
  it does not take the `ref_overlay.` family prefix. The schema filename is
  the wire kind plus `.v1.schema.json`, dots included, needing no transform.
- `ConfigKeyDivergence` carries keys and booleans only.
  `TestConfigKeyDivergenceCarriesNoValuesOrHashes` pins the exact field set:
  adding a value-, hash-, or secret-shaped field fails there first.
- `read_surface: none` in `specs/fact-kind-registry.v1.yaml` is intentional,
  not a placeholder: the divergence surface and the ref-scoped content reads
  land in sibling Phase 1 leaves. Do not replace it with a placeholder route.
- The reducer decodes only the latest struct for each kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
