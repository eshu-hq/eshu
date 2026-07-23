# AGENTS.md — internal/ghactionsref guidance for LLM assistants

## Read first

1. `go/internal/ghactionsref/README.md` -- purpose, exported surface, and
   invariants
2. `go/internal/ghactionsref/ghactionsref.go` -- `Parse` and `Pinned`
3. `go/internal/ghactionsref/doc.go` -- package contract statement
4. `docs/public/reference/relationship-mapping-evidence.md` -- the
   `ref_value`/`ref_pinned` contract this package backs

## Invariants this package enforces

- **`Pinned` classifies exactly one property** -- full-length commit SHA
  (40-hex or 64-hex), nothing else. Do not add a `ref_kind: branch|tag`
  classification; a tag and a branch are statically indistinguishable from a
  ref string alone, and a tag is mutable regardless of which one it is.
  Classifying it would fabricate a property the string does not prove
  (issue #5372).
- **Short SHAs are not pinned** -- any hex string shorter than 40 characters
  returns `false` from `Pinned`, never `true`. A short SHA is not guaranteed
  unique.
- **`Parse` never fabricates a ref** -- a `uses:` value with no `@` segment
  (local `./` workflows, Docker actions) returns an empty `refValue`. Callers
  must treat that as "omit the field," never "default to a value."
- **Zero internal imports** -- this package imports nothing under
  `go/internal/*`. It exists specifically so the reducer/graph-projection path,
  the query/read-model path, and the content-shaping path
  (`go/internal/content/shape`) -- none of which otherwise depend on each
  other -- can all depend on it without an import cycle. Adding an internal
  import here defeats that purpose; if a change seems to need one, stop and
  reconsider which package should own the new logic instead.

## Common changes and how to scope them

- **Changing the `@` split rule** -- both `go/internal/relationships` (via
  `parseGitHubRefParts` delegating to `Parse`) and `go/internal/query` (via
  its `uses:` split helpers routing through `Parse`) depend on this exact
  behavior. Run the both-paths-agree parity tests in
  `go/internal/relationships/github_actions_ref_pin_parity_test.go` and
  `go/internal/query/github_actions_ref_pin_parity_test.go` after any change --
  both must still assert the same fixed `(slug, ref_value, pinned)` set.
- **Changing `ReusableWorkflowRepo`, `ActionRepo`, or
  `LocalReusableWorkflowPath`** -- these back the edge-target slug in
  `go/internal/relationships/github_actions_evidence.go` (which reuses each
  function's exact pre-#5526 behavior verbatim) and
  `go/internal/query/content_relationships_github_actions.go` /
  `go/internal/query/repository_workflow_artifacts.go` (which layer their own
  quote-stripping and, for `ActionRepo`, ref-cleaning on top). Run the
  differential tests in
  `go/internal/relationships/github_actions_slug_detectors_test.go` and
  `go/internal/query/github_actions_slug_detectors_test.go` after any change
  -- they assert the exact pre-#5526 output for a representative input corpus
  and will fail on any silent shape change.
- **Changing `IsWorkflowPath`** -- this backs the content-entity identity
  gate in `go/internal/content/shape/materialize.go`
  (`isDirectGitHubActionsWorkflowPath`) and the content-relationship
  classifier's workflow-path branch in
  `go/internal/query/content_relationships_github_actions.go`
  (`isGitHubActionsArtifactPath`). Both packages have their own table-driven
  path-gate tests (`TestMaterializeGitHubActionsWorkflowPathGate` in
  `go/internal/content/shape/materialize_github_actions_workflow_test.go` and
  `TestGitHubActionsSourceRelationshipsRejectsInexactWorkflowPaths` in
  `go/internal/query/content_relationships_github_actions_path_gate_test.go`)
  plus this package's own `TestIsWorkflowPath`; run all three after any
  change so the two call sites cannot silently diverge again.
- **Widening the pinned classification** (for example, accepting a shorter hex
  length) -- this is a security-relevant behavior change with a direct
  downstream effect on `ref_pinned` in graph nodes and HTTP/MCP responses that
  already-shipped callers may treat as a safety signal. Do not change the
  40/64 threshold without updating
  `docs/public/reference/relationship-mapping-evidence.md` and re-verifying
  GitHub's current hardening guidance language in the same change.

## Failure modes and how to debug

- Symptom: the reducer/graph path and the query/read-model path report
  different `ref_pinned` values for the same ref -- one of the two callers
  stopped routing through `Parse`/`Pinned` and reimplemented the split
  locally. Grep both `go/internal/relationships/first_party_refs.go` and
  `go/internal/query/content_relationships_github_actions.go` for any
  `strings.Index(..., "@")` that does not call into this package.
- Symptom: a local `./` reusable workflow shows `ref_pinned: true` -- some
  caller defaulted a missing ref instead of omitting the field. `Parse`
  returns an empty `refValue` for that shape; the omission must happen at the
  call site (see the citation-field omission pattern in
  `go/internal/reducer/cross_repo_evidence_artifacts.go`).
- Symptom: `go/internal/content/shape` materializes (or fails to purge) a
  content entity for a path `go/internal/query` would reject as a GitHub
  Actions artifact, or vice versa -- one of the two callers stopped routing
  through `IsWorkflowPath` and reimplemented the path gate locally. Grep both
  `go/internal/content/shape/materialize.go` and
  `go/internal/query/content_relationships_github_actions.go` for any
  `strings.Split(..., "/")` path-segment check that does not call into this
  package.

## Anti-patterns specific to this package

- **Adding a `go/internal/*` import** -- breaks the leaf-package guarantee
  this package exists to provide.
- **Adding a `ref_kind` field or branch/tag classification** -- rejected by
  design; re-read the "Decision" section of issue #5372 before proposing it
  again.
- **I/O or package-level state** -- this is a pure string-parsing package. No
  database connections, HTTP calls, or global mutable state belong here.

## What NOT to change without an ADR

- The 40/64 hex-length threshold in `Pinned` -- this is the entire safety
  claim the `ref_pinned` signal makes across every consumer (graph node
  property, Postgres read-model row, HTTP API response, MCP tool response).
