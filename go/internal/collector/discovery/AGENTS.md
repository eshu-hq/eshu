# AGENTS.md — internal/collector/discovery guidance for LLM assistants

## Read first

1. `go/internal/collector/discovery/README.md` — purpose, exported surface,
   path invariants, and operational notes
2. `go/internal/collector/discovery/discovery.go` — `ResolveRepositoryFileSetsWithStats`,
   `collectSupportedFiles`, `groupFilesByRepository`, and `nearestRepositoryRoot`
3. `go/internal/collector/discovery/gitignore.go` — `.gitignore` and
   `.eshuignore` filtering implementations
4. `go/internal/collector/discovery/path_globs.go` — `PathGlobRule` matching
   and `IgnoredPathGlobs` / `PreservedPathGlobs` logic
5. `go/internal/collector/discovery/doc.go` — the package contract

## Invariants this package enforces

- **Absolute output paths** — `RepoRoot` and all `Files` in every `RepoFileSet`
  are absolute paths after `filepath.Abs` + `filepath.EvalSymlinks` on the scan
  root. Files come from `filepath.WalkDir` which already produces absolute paths
  when given an absolute scan root. This invariant is stated in the `RepoFileSet`
  doc comment at `discovery.go:109-112`.

- **Sorted output** — `Files` is sorted with `sort.Strings` before gitignore and
  eshuignore filtering. Downstream stages rely on stable ordering across snapshot
  runs for the same repository state. Gitignore filtering preserves sort order
  because it only removes entries; do not re-sort after filtering.

- **Conservative gitignore semantics** — ambiguous `.gitignore` rules include
  the file rather than exclude it. This is intentional; downstream parsers
  reject what they cannot handle. Do not change this to exclusive-by-default
  without verifying fixture intent across the full corpus.

- **SupportedFileMatcher required** — a nil `SupportedFileMatcher` returns an
  error immediately. The matcher is the seam for the parser registry; callers
  must always supply one.

- **External symlinks skipped** — `isExternalSymlink` rejects symlinks that
  resolve outside the scan root. This prevents traversal of system paths through
  symlinked directories inside a repo. Do not remove this check.

## Common changes and how to scope them

- **Add a new skip reason to Options** → add the field to `Options`, handle it
  in `collectSupportedFiles`, add the corresponding counter to `DiscoveryStats`,
  and populate it. Add a test in `discovery_test.go`. Update
  `collector.LoadDiscoveryOptionsFromEnv` if the new option is env-driven.

- **Change `.gitignore` parsing behavior** → edit `gitignore.go`; add a test
  case that exercises the ambiguous rule path. Do not change conservative
  include semantics without a corpus-level validation pass.

- **Add a new path glob matching feature** → edit `path_globs.go`; add test
  cases covering the new match logic, a `PreservedPathGlobs` that overrides it,
  and a subtree prune case.

- **Add a stat or counter to DiscoveryStats** → add the field to `DiscoveryStats`
  in `discovery.go`, increment it in `collectSupportedFiles`, and update
  `TotalDirsSkipped()` or `TotalFilesSkipped()` if the counter should be
  included in the aggregate. Update the discovery advisory report type in
  `internal/collector/discovery_advisory.go` if the new stat should surface in
  the advisory output.

## Failure modes and how to debug

- Symptom: `RepoFileSet.Files` is empty for a repo that has source files →
  likely cause: all files matched an ignored dir, extension, or path glob rule →
  run `eshu index --discovery-report` on the repo; check the skip breakdown
  in the advisory output for which rules fired.

- Symptom: discovery returns no `RepoFileSet` for a checked-out repo →
  likely cause: no `.git` marker found in the directory tree → verify the repo
  has a `.git` directory at its root; `nearestRepositoryRoot` returns empty
  string for trees without a `.git` marker, grouping all files under the scan
  root instead.

- Symptom: gitignore-excluded files appearing in `RepoFileSet.Files` →
  likely cause: `HonorGitignore` is not set in `Options`, or the rule is
  ambiguous → check that `HonorGitignore=true` is in the `Options` passed to
  discovery; review the `.gitignore` rule for ambiguity.

- Symptom: `eshu_dp_discovery_files_skipped_total` counter not incrementing for
  a new skip reason → likely cause: the new reason is not recorded in the
  collector snapshotter's `recordDiscoveryMetrics` function
  → check `git_snapshot_native.go` in `internal/collector` and add the new stat
  mapping there.

## Anti-patterns specific to this package

- **Returning relative paths in RepoFileSet.Files** — callers depend on
  absolute paths for `parser.Engine.ParsePath` and for `streamFacts` body
  re-reads. Relative paths break both callers silently.

- **Sorting after gitignore filtering** — the sort is applied before filtering
  (`discovery.go:157-163`). Adding a sort after filtering changes the output
  contract unnecessarily and wastes time.

- **Importing internal/collector or internal/parser** — this package is a leaf
  below `internal/collector`. It must not import its parent or sibling packages.
  The `SupportedFileMatcher` function parameter is deliberately the only
  coupling to the parser registry.

- **Eager `.gitignore` exclusion for ambiguous rules** — the conservative
  include semantic is a deliberate design choice. Overly aggressive exclusion
  causes fixture-level mismatch and undercounts indexed files.

## What NOT to change without an ADR

- `RepoFileSet.RepoRoot` and `Files` being absolute paths — changing either to
  relative paths breaks the collector snapshotter, `streamFacts`,
  the parser engine parse dispatch, and every downstream consumer that
  dereferences these paths.
- Conservative gitignore semantics — changing ambiguous rule behavior from
  include to exclude requires a corpus-level validation pass to ensure no
  previously-indexed files disappear.
