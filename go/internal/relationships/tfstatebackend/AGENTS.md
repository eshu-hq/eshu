# internal/relationships/tfstatebackend Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md`, `doc.go`, and `resolver.go`.
2. `go/internal/scope/tfstate.go` for state-snapshot scope identity.
3. `go/internal/storage/postgres/tfstate_backend_canonical.go` for the
   production query adapter.
4. Drift reducer docs/tests before changing caller-visible error behavior.

## Local Rules

- The resolver is read-only. It must not mutate canonical rows, queue state, or
  facts.
- `ResolveConfigCommitForBackend` returns at most one `CommitAnchor`.
- A nil `TerraformBackendQuery` is valid no-owner mode and must return
  `ErrNoConfigRepoOwnsBackend`.
- Multi-owner conflicts return `ErrAmbiguousBackendOwner`; do not pick a repo
  winner.
- Missing owners return `ErrNoConfigRepoOwnsBackend`; callers must not classify
  config-vs-state drift from absence alone.
- Latest selection is deterministic: highest `CommitObservedAt`, with
  lexicographic ascending `CommitID` as the tie-break.
- Do not call the resolver in an unbounded hot loop without caller-side caching
  or batching.

## Change Gates

- Error changes require resolver tests and drift-handler classification tests.
- Selection-rule changes require architecture-owner approval and package docs.
- Return-shape changes must keep each resolve call bounded to the matching
  `(backend_kind, locator_hash)` ownership key.

## Do Not Change Without Owner Review

- Single-owner policy.
- Deterministic latest selection.
- Typed error meanings.
