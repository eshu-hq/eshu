---
name: eshu-contract-rigor
description: Use when adding or changing a fact kind, changing a payload shape, editing sdk/go/factschema or sdk/go/collector, editing specs/fact-kind-registry.v1.yaml, editing fixture packs, or touching anything where an Odù (fixture-pack entry) overlaps a cassette or the B-12 snapshot. Encodes the Contract System v1 rules: typed structs over hand-built maps, version shims that live in the contracts module and never in reducer handlers, and the major/minor/patch break policy for payload schemas.
---

# eshu-contract-rigor

Contract System v1 makes the fact payload, not just the envelope, a versioned
contract between collectors and the reducer. See
[Contract System v1](../../../docs/internal/design/contract-system-v1.md) and
the [Contributor Summary](../../../docs/internal/contract-system-contributor-summary.md)
for the full design; this skill is the operating checklist for changes that
touch it.

## Does my change touch the contract system?

- A fact kind is added, renamed, removed, or its meaning changes.
- A payload shape changes (field added, removed, renamed, retyped, or a stable
  key's derivation changes).
- `sdk/go/factschema` or `sdk/go/collector` is edited.
- `specs/fact-kind-registry.v1.yaml` is edited.
- A fixture pack is added or changed.
- An Odù (a fixture-pack entry, see #4572) overlaps a cassette or the B-12
  snapshot — when snapshots or cassettes are also touched, add
  `eshu-golden-corpus-rigor` alongside this skill.

If yes to any, the rules below apply before the change is done.

## The one rule

> Every repository imports the contracts. No repository imports another
> repository.

Collectors and the reducer meet only in the contracts module,
`github.com/eshu-hq/eshu/sdk/go/factschema`. Families live at
`sdk/go/factschema/<family>/v1` — today `sdk/go/factschema/aws/v1`. Neither
side imports the other directly.

## Typed structs only

- Never hand-build a `map[string]any` for a fact kind that already has a typed
  struct in `sdk/go/factschema/<family>/v1`. Build the payload from the struct.
- Never read a typed kind's payload with a raw
  `payloadString(env.Payload, "some_key")` lookup in a reducer handler. Decode
  through the contracts seam instead, e.g. `factschema.DecodeAWSResource(env)`,
  and use the returned struct's fields.
- A kind without a typed struct yet is not a violation — the migration is
  incremental, family by family (design doc section 7). Do not invent a struct
  ahead of the migration; flag the gap and route it to the family's own
  migration work instead of doing it inline on an unrelated change.

## Version shims live in the contracts module

Version handling belongs to the decode seam
(`sdk/go/factschema/decode.go`, e.g. `DecodeAWSResource`), never in a reducer
handler. A reducer handler codes against the **latest** struct only; when a
payload majors, the contracts module gains a conversion shim and the reducer
takes a dependency bump with no handler-code change. If you find a reducer
handler branching on `schema_version` or holding its own upgrade/downgrade
logic, that is a design violation — move the shim into the contracts module.

## Breaking-change definition

Classify every payload schema change against this policy (design doc section
5, contributor summary versioning cheat sheet):

- **Major** — remove a field, rename a field, narrow a field's type, or change
  the meaning of a field, including changing how a stable key is derived.
  Requires a conversion shim in the same contracts change.
- **Minor** — an additive optional field. The reducer needs no change and
  ignores it until a handler opts in.
- **Patch** — docs only.

Name the gates a payload change must clear when you touch this surface. These
are **design rules from Contract System v1**, not all live as CI today — state
each one as a design requirement unless you have confirmed it exists as a gate
on `main`:

- **Schema-diff gate** (`#4569`, contracts CI) — diffs generated JSON Schemas
  against the last tag; a removed, renamed, or narrowed field without a major
  bump must fail the build. Confirm current status before asserting it blocks
  anything.
- **Conformance payload validation** — `sdk/go/collector/conformance` validates
  fixture payloads against the checked-in JSON Schemas, not only kind, version,
  and confidence (design doc section 3.5). Extending it to payload-shape
  validation is part of this epic's scope, not necessarily landed.
- **Payload-usage manifest** (`#4573`, core CI) — generated from the typed
  decode calls; lists which payload fields each reducer domain reads, and
  diffs the reverse break (a handler starting to require a field no schema
  declares). Treat as a design rule unless confirmed present on `main`.
- **Registry regeneration** — `specs/fact-kind-registry.v1.yaml` gains
  `payload_schema:`, `deprecated_in:`, and `removed_in:` fields as an additive
  minor bump of the registry's own `version:` field (design doc section 3.1),
  not a new registry file or a registry major.

## Fixture packs and Odù

An Odù is a fixture-pack entry (`#4572`). Fixture packs are released in
lockstep with the contracts module so an external collector can pin a
fixture-pack version and prove in its own CI that it emits exactly the shapes
the target reducer release consumes (design doc section 3.5). Keep fixture
packs in lockstep with the contracts release they describe — a fixture pack
that outlives the schema version it was cut from is stale evidence, not a
fixture.

When a fixture pack change also touches a cassette or the B-12 snapshot
(`testdata/golden/e2e-20repo-snapshot.json`), load `eshu-golden-corpus-rigor`
in addition to this skill — that skill owns the golden-corpus gate contract.

## `proto/eshu/data_plane` is not a source of truth

The design (section 3.3, section 9) demotes the unwired
`proto/eshu/data_plane` tree: it is a future transport candidate that may
later be generated **from** the Go schema package, or deleted outright. Do not
treat it as authoritative for payload shape, and do not hand-edit it to keep
it "in sync" with `sdk/go/factschema`. That direction of sync does not exist
in this design.

## Missing required fields dead-letter, they never silently zero out

A missing required field on decode is a classified `input_invalid` dead letter
(`go/internal/projector/dead_letter_triage.go`, `TriageClassInputInvalid`),
never a silent empty string or zero value. This is the accuracy guarantee the
design exists to protect (design doc section 1): a collector that renames or
drops a payload key must produce a visible, classified failure instead of a
wrong graph identity that looks fine until someone traces it back.

## Out of scope / related

- Editing `go/internal/reducer` handler logic, collector internals, or
  `sdk/go/factschema` code: that is family-migration work (design doc
  section 7), not this skill's job to perform — this skill tells you the
  rules to apply while doing it.
- Cassette and B-12 snapshot mechanics: `eshu-golden-corpus-rigor`.
- Go edits generally: `golang-engineering`.
- MCP/API response shapes for fact-derived data: `eshu-mcp-call-rigor`.
