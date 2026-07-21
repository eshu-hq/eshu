# AGENTS.md — internal/collector/codeowners guidance

## Read First

1. `README.md` — package purpose, CODEOWNERS grammar summary, and the
   payload contract.
2. `parser.go` — `Parse`'s doc comment is the authoritative grammar reference
   for this package; read it before changing parsing behavior.
3. `precedence.go` — location precedence (`CandidatePaths`, `IsCandidatePath`,
   `ResolveWinner`).
4. `envelope.go` / `emitter.go` — fact envelope construction.
5. `go/internal/facts/codeowners.go` — Phase 1 fact-kind and schema-version
   constants this package emits into.
6. `sdk/go/factschema/codeowners/v1/ownership.go` — the typed payload struct;
   `sdk/go/factschema/decode_codeowners.go` — the encode/decode seam.
7. `go/internal/collector/git_codeowners_facts.go` — the Git collector glue
   that calls this package during content streaming (candidate discovery,
   accumulation, and the resolve-then-emit call at the end of the stream).

## Invariants

- Keep this package a pure normalizer: no file discovery, no repository/
  scope/generation identifier minting, no reducer or query imports in
  production code. File discovery and stream wiring belong to the Git
  collector.
- Do not add new fact kinds or change the schema version here. This package
  emits into the existing `codeowners.ownership` contract
  (`facts.CodeownersSchemaVersionV1`) that Phase 1 shipped.
- Build every payload from the typed `codeownersv1.Ownership` struct via
  `factschema.EncodeCodeownersOwnership`. Never hand-build a
  `map[string]any` for this fact kind (Contract System v1 §3.1; see the
  `eshu-contract-rigor` skill).
- `Parse` must match GitHub's documented CODEOWNERS syntax, not an invented
  approximation. If you change parsing behavior, re-read the grammar doc
  comment on `Parse` and the GitHub docs link in `README.md` first, and update
  both when the behavior changes.
- The GitHub sections feature (`[Section-name]`, `^[Section-name][2]`) is
  explicitly out of scope. A section header line is skipped as a non-rule; do
  not partially interpret it (for example, do not extract default owners
  declared on a section header line).
- A rule line with a pattern and zero owner tokens is dropped, never emitted
  as an owner-less fact — it carries no ownership claim in GitHub's own
  semantics.
- `OrderIndex` is the rule's 0-based position among **emitted** rule lines
  only (comments/blanks/sections/pattern-only lines do not consume an index).
  Downstream last-match-wins resolution depends on this ordinal being stable
  and gap-free; do not change it to a raw line number.
- Location precedence is fixed: `.github/CODEOWNERS` > `CODEOWNERS` >
  `docs/CODEOWNERS`. `ResolveWinner` returns the first-present candidate in
  that order; it does not merge rules across multiple present files.

## Common Changes

- If GitHub's documented syntax changes (for example, a new owner-token
  shape), update `Parse`'s doc comment and `README.md`'s grammar section in
  the same change as the parsing logic, and add a table-driven test case.
- If the payload shape changes, that is a Contract System v1 change: update
  `sdk/go/factschema/codeowners/v1` and the encode/decode seam first, following
  the major/minor/patch break policy, then update this package's `Emit` to
  match. Do not change the payload shape here alone.
- Adding a new CODEOWNERS location to the precedence list is a product
  decision that changes `README.md`'s grammar section, `CandidatePaths`,
  `IsCandidatePath`, and `ResolveWinner` together, plus the Git collector's
  discovery admission in `git_codeowners_facts.go` / `git_snapshot_native.go`.
