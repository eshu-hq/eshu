# Agent instructions: internal/reducer/code/owners

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Projects `Repository-[:DECLARES_CODEOWNER]->CodeownerTeam` edges from
directly-emitted `codeowners.ownership` facts (issue #5419 Phase 3). Moved out
of the reducer root as its own package (issue #6061). See the README's
Purpose and Ownership boundary sections for what this package owns.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/owners/README.md`
- `docs/internal/design/reducer-target-tree.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the codeowners stanza of
  `compat_projection.go` and `defaults_domain_catalog.go`'s wiring), never
  the reverse.
- **A CODEOWNERS-candidate delta forces a whole-repository retract, never a
  path-scoped one.** Winner-resolution for CODEOWNERS is whole-repo, so
  scoping the retract to only the touched path can leave a former winner's
  stale edges behind (issue #5419 P1).
- **`candidatePaths` mirrors, but never imports,**
  `internal/collector/codeowners.CandidatePaths()` — the collector/reducer
  package ownership boundary forbids the import. Update both in lockstep;
  `TestCodeownersOwnershipCandidatePathsMatchCollector` in
  `scope_test.go` is the CI gate that catches drift.

## Common changes

Adding a new codeowners edge field: extend the row map
`ExtractOwnershipEdgeRowsWithQuarantine` builds in `handler.go`, then thread
it through `buildIntentRows`' payload if the field belongs on the durable
edge write too.

## Failure modes to avoid

- Importing `internal/collector/codeowners` directly instead of duplicating
  `CandidatePaths()` — this would cross the collector/reducer ownership
  boundary.
- Collapsing a repeated (repo, path, pattern, owner) key onto its FIRST
  `order_index` instead of its highest — this breaks the last-match-wins
  precedence resolver.
- Exporting a new unexported helper "just in case" a future caller needs it.
  `Handler`, `ExtractOwnershipEdgeRowsWithQuarantine`,
  `LoadMaterializationFacts`, and `MaterializationFactKinds` are exported
  because the reducer root's compat forwarders and bench-test corpus guard
  need them; nothing else in this package has an external caller today.

## Do not change without ADR review

- The evidence-source string `"reducer/codeowners"` (`evidenceSource` in
  `handler.go`) — durable rows and downstream consumers key on this literal.
