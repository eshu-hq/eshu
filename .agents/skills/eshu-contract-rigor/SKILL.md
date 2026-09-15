---
name: eshu-contract-rigor
description: Use when a fact kind, payload shape, sdk/go/factschema, sdk/go/collector, or specs/fact-kind-registry.v1.yaml changes. Covers major/minor/patch classification and the factschema-diff and payload-usage-manifest gates.
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
  ignores a new field until a handler opts in. A new member of a closed
  string enum is minor only where every reader of that enum keeps a member
  it does not know rather than failing the read; today that holds for the
  governance-audit stored-row reader (actor class, scope class, decision,
  event type), where treating membership as read-side validation turned
  every rolling upgrade that added a member into a 500 on the audit-list
  page (#6574). For a fact-payload enum whose consumers still reject an
  unfamiliar member, adding one is a major change until those readers are
  made tolerant.
- **Patch** — docs only.

These gates are live on `main` today (`specs/ci-gates.v1.yaml`, all `tier:
pre-pr`/blocking, so part of what `required-gates-complete` waits on):

- **`factschema-diff`** (issue #4569) — `bash scripts/verify-factschema-diff.sh`
  diffs generated JSON Schemas under `sdk/go/factschema/schema/` against the
  merge-base with `origin/main` (no `factschema-*` release tag exists yet, so
  the baseline is the branch point, not a tag); a removed/renamed/narrowed
  field, a widened or newly-added required set, or a deleted schema file
  without a major bump fails the build. See
  `go/cmd/factschema-diff/README.md`.
- **Conformance payload validation** — `sdk/go/collector/conformance` (see
  `payload_validate.go`, `payload_schema_test.go`) validates fixture payloads
  against the checked-in JSON Schemas, not only kind, version, and confidence
  (design doc section 3.5). This is landed, not pending: a fixture whose
  payload is missing a required field fails closed with the offending field
  named. `scorecard-example-conformance` runs this for the out-of-tree
  example collector (`examples/collector-extensions/scorecard`,
  `fixturepack_pin_test.go` — see that package's README "Pinning story") so
  external-collector conformance is a real running gate, not a manual step.
- **`payload-usage-manifest`** (issue #4573) —
  `bash scripts/verify-payload-usage-manifest.sh` derives, from the typed
  `factschema.Decode*` seams across reducer/projector/query/loader/
  relationships/replay, which declared payload fields typed-decode handlers
  actually read, and fails when a handler reads a field no checked-in JSON
  Schema declares. This is the reverse direction from `factschema-diff` (a
  consumer starting to require a field no schema promises). See
  `go/internal/payloadusage/README.md` and
  `go/internal/reducer/payload_usage_manifest_test.go`
  (`TestPayloadUsageManifest`).
- **`fact-kind-registry`** — `bash scripts/verify-fact-kind-registry.sh`
  validates `specs/fact-kind-registry.v1.yaml` (currently `version: "1.1.0"`)
  and regenerates `go/internal/facts/fact_kind_registry.generated.go` and
  `FACT_KIND_REGISTRIES.md`. The registry already carries the additive
  `payload_schema:`, `deprecated_in:`, and `removed_in:` fields from the
  v1.1.0 bump (a handful of AWS kinds set `payload_schema:` today); see the
  registry file's own header comment and
  `docs/public/reference/fact-schema-versioning.md`.
- **`contract-source-of-truth`** — `bash scripts/verify-contracttest.sh`
  regenerates and diffs the contract test fixtures under
  `go/internal/collector/contracttest/` against
  `specs/fact-kind-registry.v1.yaml` / `specs/collector_fact_contract.v1.yaml`.

## Fixture packs and Odù

An Odù is a fixture-pack entry (`#4572`). Fixture packs are released in
lockstep with the contracts module so an external collector can pin a
fixture-pack version and prove in its own CI that it emits exactly the shapes
the target reducer release consumes (design doc section 3.5). Keep fixture
packs in lockstep with the contracts release they describe — a fixture pack
that outlives the schema version it was cut from is stale evidence, not a
fixture. `examples/collector-extensions/scorecard` is the reference
implementation of this pinning story, proved by the
`scorecard-example-conformance` gate above.

When a fixture pack change also touches a cassette or the B-12 snapshot
(`testdata/golden/e2e-20repo-snapshot.json`), load `eshu-golden-corpus-rigor`
in addition to this skill — that skill owns the golden-corpus gate contract.

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
