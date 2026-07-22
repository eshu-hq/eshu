# AGENTS.md — internal/collector/submodule guidance

## Read First

1. `README.md` — package purpose, `.gitmodules` grammar summary, and the
   payload/resolution contract.
2. `parser.go` — `Parse`'s doc comment is the authoritative grammar reference
   for this package; read it before changing parsing behavior.
3. `candidate.go` — the single recognized `.gitmodules` location
   (`IsGitmodulesPath`); unlike CODEOWNERS there is no precedence list.
4. `resolve.go` — `ResolveRepoID`'s doc comment explains exactly why it gates
   on `NormalizeRemoteURL` producing an `https://` result before ever calling
   `CanonicalRepositoryID`. Read it before touching the resolution gate.
5. `envelope.go` / `emitter.go` — fact envelope construction.
6. `go/internal/facts/submodule.go` — Phase 1 fact-kind and schema-version
   constants this package emits into.
7. `sdk/go/factschema/submodule/v1/pin.go` — the typed payload struct;
   `sdk/go/factschema/decode_submodule.go` — the encode/decode seam.
8. `go/internal/collector/git_submodule_facts.go` — the Git collector glue
   that calls this package during content streaming (candidate discovery,
   accumulation, and the resolve-then-emit call at the end of the stream).
9. `go/internal/collector/git_submodule_pinned_sha.go` — the Git collector's
   `gitSubmoduleGitlinkSHA`, the `PinnedSHAResolver` implementation wired in
   by `git_submodule_facts.go` (issue #5420 Phase 2b).

## Invariants

- Keep this package a pure normalizer: no file discovery, no repository/
  scope/generation identifier minting, no reducer or query imports in
  production code. File discovery and stream wiring belong to the Git
  collector.
- Do not add new fact kinds or change the schema version here. This package
  emits into the existing `submodule.pin` contract
  (`facts.SubmoduleSchemaVersionV1`) that Phase 1 shipped.
- Build every payload from the typed `submodulev1.Pin` struct via
  `factschema.EncodeSubmodulePin`. Never hand-build a `map[string]any` for
  this fact kind (Contract System v1 §3.1; see the `eshu-contract-rigor`
  skill).
- `Emit` fills `Pin.PinnedSHA` only through the caller-supplied
  `FixtureContext.PinnedSHAResolver` callback; it leaves `Pin.PinnedSHA` nil
  when no resolver is set. Do not add a gitlink-tree-entry read
  (`git ls-tree` or equivalent) inside this package — that stays the Git
  collector's job (`gitSubmoduleGitlinkSHA`,
  `go/internal/collector/git_submodule_pinned_sha.go`, issue #5420 Phase 2b);
  this package only calls the injected resolver.
- `ResolveRepoID` MUST NOT guess. It returns `""` for anything ambiguous or
  unparseable — most importantly git's own relative submodule URL forms
  (`../sibling.git`, `./nested`), which are meant to be resolved against the
  PARENT repository's own remote (a later-phase concern), not canonicalized
  standalone. Do not "improve" this by attempting relative-URL resolution
  here without express scope for it; a wrong guess is worse than an absent
  edge.
- `Parse` must match git-config's documented syntax for the subset this
  package needs, not an invented approximation. If you change parsing
  behavior, re-read the grammar doc comment on `Parse` and the git-config
  docs link in `README.md` first, and update both when the behavior changes.
- A section missing `path` or `url` is dropped, never emitted as a partial
  fact — Phase 1's schema requires both as the join identity's siblings for
  the ".gitmodules"-only view (see `sdk/go/factschema/submodule/v1.Pin`'s
  doc comment).
- The stable key is `(parent_repo_id, submodule_path)` only — NOT the URL.
  Do not add the URL to the stable key; a submodule re-pointed to a new URL
  in a later generation must upsert the same fact, not fork a new one.
- Location recognition is fixed: exactly `.gitmodules` at the repository
  root. `.gitmodules` never appears at more than one location the way
  CODEOWNERS does; do not add a precedence list here.

## Common Changes

- If git-config's documented syntax changes (for example, a new escape
  sequence this collector should honor), update `Parse`'s doc comment and
  `README.md`'s grammar section in the same change as the parsing logic, and
  add a table-driven test case.
- If the payload shape changes, that is a Contract System v1 change: update
  `sdk/go/factschema/submodule/v1` and the encode/decode seam first,
  following the major/minor/patch break policy, then update this package's
  `Emit` to match. Do not change the payload shape here alone.
- Phase 2b (gitlink SHA resolution) wired `FixtureContext.PinnedSHAResolver`
  into this package but keeps the actual `git ls-tree` read in the Git
  collector (`gitSubmoduleGitlinkSHA`); do not move that read into this
  package. Later phases (reducer/projector/read surface) remain explicitly
  out of scope here; do not fold that work in without updating `doc.go`'s
  phase framing in the same change.
