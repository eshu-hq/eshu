# AGENTS.md — internal/extensions/skillgen guidance for LLM assistants

## Read first

1. `go/internal/extensions/skillgen/doc.go` — package contract.
2. `go/internal/extensions/skillgen/README.md` — purpose, ownership,
   exported surface, telemetry, gotchas.
3. `go/internal/extensions/skillgen/fragment.go` — frontmatter parsing and
   the seven canonical fragment fields.
4. `go/internal/extensions/skillgen/byte_citation.go` — citation
   normalization and the stable comment block.
5. `go/internal/extensions/skillgen/hosts.go` — the three-host registry and
   the `HostAdapter` contract.
6. `go/internal/extensions/skillgen/render.go` — `RenderAll`, `WriteExpected`,
   `CheckDrift`.
7. `go/internal/extensions/skillgen/capability_aware.go` — capability
   override loading and per-deployment rendering.
8. `docs/internal/skill-fragments-design.md` — the S1 design contract.
9. `skill-fragments/` — the source-of-truth fragments.
10. `expected/` — the roundtrip baseline committed at the repo root.

## Invariants this package enforces

- **No Eshu-internal package imports.** The skillgen is a build-time
  generator. Imports of `internal/storage`, `internal/query`,
  `internal/reducer`, `internal/capabilitycatalog`, `internal/mcp`, or
  `internal/telemetry` would create a cycle or a hidden runtime
  dependency. Use the standard library and `gopkg.in/yaml.v3` only.
- **Deterministic output.** `LoadFragments` sorts by id, `FormatCommentBlock`
  sorts and dedupes, the host adapters write byte-stable output for the
  same input. The roundtrip baseline in `expected/` is the regression
  guard; a non-deterministic change shows up as a `content_mismatch`
  drift and fails the `check` subcommand.
- **`byte_citation` is a stable identifier.** The comment block emitted at
  the top of every generated skill is the anchor S3 verifies against the
  merge tree. The format `<!-- eshu:byte-citation path#start-end -->` is
  load-bearing; do not rename the prefix, do not switch to a different
  comment style, do not move the block below the frontmatter.
- **RenderAll is host-agnostic.** Per-host frontmatter fields, the
  always-on layer file, and any per-host formatting quirks live in the
  owning adapter (`claude_code.go`, `cursor.go`, `codex.go`). Adding a
  field to the shared frontmatter would couple the hosts and is wrong.
- **Capabilities are optional and gitignored.** The
  `skill-fragments/capabilities.local.yaml` file is the per-deployment
  override. A missing file is the default (all collectors enabled); a
  malformed file is a hard error. The package never writes the file.

## Common changes and how to scope them

- **Edit a fragment body** → change `skill-fragments/<id>.md` →
  run `go run ./cmd/skillgen gen` to regenerate `expected/` → commit
  both. Why: the S3 CI gate runs `check`; the baseline must be in
  lockstep with the fragments.
- **Add a new fragment** → add `skill-fragments/<id>.md` with the S1
  frontmatter schema → add an `expected/` regeneration → add a test in
  `render_test.go` for the new fragment's body. The seven canonical
  fragment ids are stable per the S1 design; a new fragment is a
  v1-plus follow-up and ships as a new constant plus a test, not as an
  edit to the existing seven.
- **Add a new host** → add a `Host` constant in `hosts.go` → append a
  constructor to `hostRegistry` → add an adapter file
  (`<host>.go`) implementing the `HostAdapter` contract → add the host
  to `AllHosts()` in deterministic order → regenerate `expected/` →
  add a test in `hosts_test.go` and `render_test.go`.
- **Change the byte-citation comment block format** → edit
  `byteCitationPrefix` and `FormatCommentBlock` together → regenerate
  `expected/` → verify the roundtrip test still passes. The format
  `<!-- eshu:byte-citation path#start-end -->` is the S1 contract;
  changes require a S1 design doc update and a S3 sign-off.
- **Add a new frontmatter field to a host adapter** → edit only the
  owning adapter file → regenerate `expected/` → update the host's
  test in `render_test.go`. The shared pipeline does not change.

## Failure modes and how to debug

- Symptom: `check` reports `content_mismatch` → cause: a fragment
  changed, or a renderer changed, without regenerating the baseline →
  run `go run ./cmd/skillgen gen` and commit the diff.
- Symptom: `check` reports `missing` → cause: `expected/` is missing a
  file the generator writes → run `gen` and commit.
- Symptom: `LoadFragments` returns `ErrFragmentMissingByteCitation` →
  cause: a fragment file is missing the `byte_citation` field → add
  the field with the longhand form (`path#start-end`) or the shorthand
  single-line form (`path#N`).
- Symptom: `NormalizeByteCitation` returns `ErrInvalidByteCitation` →
  cause: malformed anchor → ensure the anchor is `N` (single line) or
  `start-end` (range) with positive integers and `start <= end`.
- Symptom: `LoadCapabilities` returns a parse error → cause: the
  `capabilities.local.yaml` file is not valid YAML → fix the file
  or delete it (the default is all enabled).

## Anti-patterns specific to this package

- **Importing Eshu internal packages.** The skillgen is a build-time
  tool; runtime side effects belong in the runtime packages.
- **Hardcoding host names outside the registry.** The `Host` constants
  and `hostRegistry` are the only allowed source of truth for the
  three v1 hosts.
- **Wrapping the per-host adapters in shared helpers.** The adapters
  are intentionally independent so a host-specific change is local to
  one file. If two adapters need the same helper, the helper belongs
  in `render.go` (host-agnostic) or in a new file in this package,
  never in a per-host file.
- **Emitting per-host prose inside fragment bodies.** The fragment
  contract excludes per-host prose; the adapter renders it.

## What NOT to change without an ADR

- The `byte_citation` comment block format — S3 verifies it.
- The seven canonical fragment ids — they are the S1 contract.
- The `Host` constants and the three-host limit — the S1 design caps
  the matrix at three v1 hosts; adding a fourth is a v1-plus follow-up.
- The `expected/` directory layout — the roundtrip baseline is keyed
  by host id and the per-host output path.
