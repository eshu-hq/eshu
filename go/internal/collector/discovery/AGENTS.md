# AGENTS.md - internal/collector/discovery

Use `README.md` and `doc.go` for the package contract. This file preserves the
agent-only rules for discovery output shape, ignore semantics, and stats.

## Read First

1. `README.md` and `doc.go`.
2. `discovery.go` for `ResolveRepositoryFileSetsWithStats`,
   `collectSupportedFiles`, grouping, sorting, and repo-root detection.
3. `gitignore.go` for `.gitignore` and `.eshuignore` filtering.
4. `path_globs.go` for ignored and preserved path-glob behavior.
5. `go/internal/collector/discovery_advisory.go` and
   `go/internal/collector/git_snapshot_native.go` before surfacing new stats.

## Mandatory Invariants

- `SupportedFileMatcher` is required; a nil matcher returns an error.
- `RepoFileSet.RepoRoot` and every `RepoFileSet.Files` entry are absolute
  paths.
- Output order is deterministic. Keep file and repo-root sorting stable.
- Discovery is conservative: ambiguous ignore rules include files rather than
  dropping possible source truth.
- Root-anchored `.gitignore` and `.eshuignore` patterns stay rooted at the repo
  root; do not turn `/name` into a suffix match.
- Symlinks that resolve outside the scan root stay rejected.
- When no supported files are found, discovery still returns one file set for
  the scan root with an empty file list.
- This package stays a leaf. Do not import `internal/collector` or
  `internal/parser`; caller-supplied `SupportedFileMatcher` is the parser seam.

## Change Routing

- New skip reason: add the `Options`/`DiscoveryStats` field, record it in
  discovery, update aggregate helpers when needed, surface it through collector
  advisory/metrics if operator-visible, and add tests.
- `.gitignore` behavior changes require tests for ambiguous rules and a
  corpus-level validation pass before making exclusion more aggressive.
- Path-glob changes need tests for match logic, preserved overrides, and
  subtree pruning.
- New stats that should be emitted by telemetry must be wired through
  `recordDiscoveryMetrics`.

## Do Not Change Without Architecture-Owner Approval

- Absolute path output for repo roots and files.
- Conservative handling for ambiguous ignore rules.
- The leaf-package boundary and parser matcher seam.

## Required Proof

- Run `cd go && go test ./internal/collector/discovery -count=1`.
- Run collector snapshot/advisory tests when stats or advisory output changes.
- For docs-only edits, run `go run ./cmd/eshu docs verify ../go/internal/collector/discovery --fail-on contradicted,missing_evidence` from `go/`.
