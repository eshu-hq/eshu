# factschema-diff

## Purpose

Diffs the generated JSON Schemas under `sdk/go/factschema/schema/` against a
baseline git ref and fails the build when a schema broke compatibility
without a corresponding major version bump. This is the "`buf breaking`
equivalent for this stack" named in
[Contract System v1 §6, enforcement gate 1](../../../docs/internal/design/contract-system-v1.md#6-enforcement-gates).

## Ownership boundary

This command owns only schema-vs-schema structural diffing. It does not own
payload struct definitions (`sdk/go/factschema/aws/v1/...`), the schema
generator (`sdk/go/factschema/internal/schemagen`), reducer decode behavior,
or the reverse-direction payload-usage manifest gate (a separate, not-yet-
built gate per issue #4569's "Out of scope").

## Baseline resolution

There is no contracts release tag yet (only product `v0.0.x` tags), so this
gate cannot diff against "the last contracts tag" the way the design doc's
steady-state description implies. `-base-ref` names any git ref; the default
is the merge-base of `HEAD` against `origin/main`. A schema file with **no
counterpart** at the baseline ref (a brand-new fact kind) is **not** a
break — it passes unconditionally. This keeps the gate correct before any
contracts tag exists and forward-compatible afterward: once a
`factschema-<version>` tag exists, pass it as `-base-ref` (for example in a
release-time re-verification) to diff against the last released contract
instead of the merge-base.

## Breaking-change rule

Mirrors Contract System v1 §5's versioning policy: "Major = remove/rename
key, narrow a type, change stable-key derivation, change meaning" and §6.1's
"removed, renamed, or narrowed field" — field, not just required field. The
factschema schemas set `additionalProperties: false`, so a payload carrying
an undeclared property is rejected; that makes removing ANY declared field a
real break. This gate checks the parts of the rule a JSON Schema diff can
express structurally:

- **`removed_required_field`** — a field required in the baseline is absent
  from the current schema's `properties`. A required-field rename surfaces
  identically (the old name is both removed from `properties` and removed
  from `required`).
- **`removed_field`** — an OPTIONAL field present in the baseline is absent
  from the current schema's `properties`, when the baseline is fail-closed
  (`additionalProperties: false`). A collector still emitting the dropped
  field now produces a schema-invalid payload (an `input_invalid` dead letter
  per §3.2), even though the field was never required. An optional-field
  rename surfaces here on the old name. When the baseline is an OPEN schema
  (`additionalProperties` not `false`), removing an optional field is not
  flagged — the open schema still accepts the field.
- **`narrowed_type`** — a field gained an `enum` constraint where none
  existed, its `enum` set shrank, or its declared `type` changed to a
  different type.
- **`widened_required`** — a field that was optional in the baseline (present
  in `properties`, absent from `required`) became required in the current
  schema. This breaks any collector that never emitted the field.
- **`added_required_field`** — a brand-new field (absent from the baseline
  `properties` entirely) was added to the current schema's `required` set.
  Existing collectors that never emitted it now fail validation.

Every violation is suppressed when the current schema's title version marker
(`"... (schema version N)"`) took a major bump relative to the baseline's
marker, or when the baseline had no parseable marker (a schema's first
appearance under the versioned-title convention).

Additive optional fields — a new property that leaves every existing
required field, type, and name unchanged — are never flagged.

## Exported surface

This is a command package, invoked via `scripts/verify-factschema-diff.sh`
(wired into `make pre-pr` through `specs/ci-gates.v1.yaml`'s
`factschema-diff` gate) or directly:

```bash
cd go && go run ./cmd/factschema-diff -repo-root .. -base-ref <ref>
```

Run `go run ./cmd/factschema-diff -help` for the full flag reference.

## Dependencies

Standard library only (`encoding/json`, `os/exec` for `git show` /
`git merge-base`, `flag`). Does not import `sdk/go/factschema` or any
`go/internal/...` package — it operates purely on the checked-in JSON Schema
artifacts and git history, not on Go struct definitions.

## Telemetry

No runtime telemetry. This command runs only in local and CI generation/gate
contexts, never in a deployed Eshu process.

## Gotchas / invariants

- The tool reads the **working tree's** current schema files and the
  **baseline ref's** committed version of the same relative path — it does
  not require the current schema to be committed, so it works against
  in-progress edits before a commit.
- `git merge-base HEAD origin/main` requires `origin/main` to be fetched
  locally (the standard state in a CI checkout and any repo clone that has
  run `git fetch`). Pass `-base-ref` explicitly to bypass merge-base
  resolution entirely (for example in a shallow or detached-HEAD context).
- Only `sdk/go/factschema/schema/*.schema.json` files are compared. Adding a
  schema outside `sdk/go/factschema/schema/` will not be picked up; use
  `-schema-dir` to point at a different directory for local experimentation.

## Related docs

- [Contract System v1](../../../docs/internal/design/contract-system-v1.md)
- `sdk/go/factschema/README.md`
- `sdk/go/factschema/AGENTS.md`
