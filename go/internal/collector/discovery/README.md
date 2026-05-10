# Collector Discovery

## Purpose

`collector/discovery` resolves the parser-supported files inside a checked-out
repository into stable per-repo file sets. Each `RepoFileSet` carries an
absolute `RepoRoot` and a sorted slice of absolute file paths, so callers do
not have to re-resolve relative paths against the snapshot root. The git
collector calls discovery once per snapshot to decide which files to feed the
parser registry.

## Where this fits in the pipeline

```mermaid
flowchart LR
  A["internal/collector\nNativeRepositorySnapshotter"] --> B["internal/collector/discovery\nResolveRepositoryFileSetsWithStats"]
  B --> C["internal/parser\nRegistry.LookupByPath\n(SupportedFileMatcher)"]
  B --> D["RepoFileSet\nabsolute root + sorted files"]
  D --> E["internal/collector\nbuildParsedRepositoryFiles"]
```

## Internal flow

```mermaid
flowchart TB
  A["ResolveRepositoryFileSetsWithStats\nnormalize scan root"] --> B["collectSupportedFiles\nfilepath.WalkDir"]
  B --> C{"dir entry?"}
  C -- yes --> D["check ignoredDirs\nhiddenPath\nignored path globs"]
  C -- no --> E["check hiddenFile\nignored extension\nSupportedFileMatcher\nignored path globs\nsymlink check"]
  D -- skip --> F["SkipDir"]
  E -- accept --> G["append to files"]
  G --> H["groupFilesByRepository\nnearestRepositoryRoot (.git marker)"]
  H --> I["per-repo sort + optionalGitignore filter + optional EshuIgnore filter"]
  I --> J["[]RepoFileSet\nRepoRoot + sorted Files"]
```

## Lifecycle / workflow

`ResolveRepositoryFileSetsWithStats` normalizes the scan root (absolute path
plus `filepath.EvalSymlinks`), then calls `collectSupportedFiles` which walks
the directory tree. For each directory entry the walker:

1. Prunes ignored directories (`IgnoredDirs`, `.hidden` when `IgnoreHidden=true`,
   and `IgnoredPathGlobs` subtree rules).
2. Skips hidden files when `IgnoreHidden=true` unless the path matches a
   `PreservedHiddenPrefixes` entry.
3. Skips files whose extension matches `IgnoredExtensions`.
4. Calls `SupportedFileMatcher` — the caller-supplied predicate backed by
   `parser.Registry.LookupByPath`. Files no parser claims are silently dropped.
5. Applies `IgnoredPathGlobs` file-level rules (operator and repo-local).
6. Skips symlinks that resolve outside the scan root via `isExternalSymlink`.

After collection, `groupFilesByRepository` walks parent directories up from
each file looking for a `.git` marker (directory, regular file, or symlink).
Results are cached per-directory so repeated lookups are constant time. Files
without a `.git` ancestor are grouped under the scan root.

Per repo, files are sorted with `sort.Strings`, then optionally filtered by
`.gitignore` rules (`HonorGitignore=true`) and `.eshuignore` rules
(`HonorEshuIgnore=true`). Root-anchored gitignore patterns are matched only
against repo-root-relative paths, so `/terraform` drops a root binary or file
named `terraform` without pruning `internal/terraform` source packages. The
filtered sorted slice is placed in `RepoFileSet.Files`.

## Exported surface

- `Options` — discovery inputs: `IgnoredDirs`, `IgnoredExtensions`,
  `IgnoreHidden`, `PreservedHiddenPrefixes`, `HonorGitignore`, `HonorEshuIgnore`,
  `IgnoredPathGlobs`, `PreservedPathGlobs`
- `PathGlobRule` — `Pattern` + `Reason` string for operator-facing skip
  reporting
- `RepoFileSet` — `RepoRoot` (absolute) + `Files` (absolute, sorted)
- `DiscoveryStats` — per-run counters: `DirsSkippedByName`,
  `FilesSkippedByExtension`, `FilesSkippedByContent`, `DirsSkippedByUser`,
  `FilesSkippedByUser`, `FilesSkippedHidden`, `FilesSkippedGitignore`,
  `FilesSkippedEshuIgnore`; aggregated via `TotalDirsSkipped()` and
  `TotalFilesSkipped()`
- `SupportedFileMatcher` — `func(path string) bool`; the parser registry
  supplies this so discovery skips files no parser claims
- `ResolveRepositoryFileSets(root, SupportedFileMatcher, Options) ([]RepoFileSet, error)`
  — entry point for callers that do not need statistics
- `ResolveRepositoryFileSetsWithStats(root, SupportedFileMatcher, Options) (DiscoveryStats, []RepoFileSet, error)`
  — entry point used by the collector snapshotter; returned stats are
  surfaced via the discovery advisory report and `eshu index --discovery-report`

## Dependencies

- Standard library `io/fs`, `path/filepath`, `os`
- `internal/collector` consumes `RepoFileSet` outputs
- `internal/parser` supplies `SupportedFileMatcher` via
  `parser.Registry.LookupByPath`

## Telemetry

Discovery does not emit metrics or spans of its own. Counters surface through
the returned `DiscoveryStats` and are recorded by the collector snapshotter
via the `telemetry.DiscoveryFilesSkipped` instrument (metric:
`eshu_dp_discovery_files_skipped_total`, labeled `skip_reason`). The advisory
report built from `DiscoveryStats` is available via
`eshu index --discovery-report`.

## Operational notes

- `RepoRoot` and all `Files` paths are absolute. Downstream stages that need
  repo-root-relative paths must rebase using `filepath.Rel(RepoRoot, file)`.
- `Files` is sorted with `sort.Strings`. Downstream stages can rely on stable
  ordering across snapshot runs for the same repository state.
- Gitignore handling is intentionally conservative: when a `.gitignore` rule is
  ambiguous, discovery includes the file. Downstream parsers reject what they
  cannot handle.
- Root-anchored gitignore rules stay rooted at the discovered `RepoRoot`.
  Do not treat `/name` as a suffix match, or large Go repos can lose nested
  packages such as `internal/terraform`.
- `.eshuignore` filtering is applied after `.gitignore` filtering when both are
  enabled.
- Repo-local overrides live in `.eshu/discovery.json` and `.eshu/vendor-roots.json`
  in the collector, not in this package. Discovery applies `IgnoredPathGlobs`
  and `PreservedPathGlobs` from `Options` regardless of their origin.
- Per-operator overlays (ESHU_DISCOVERY_IGNORED_PATH_GLOBS,
  ESHU_DISCOVERY_PRESERVED_PATH_GLOBS) are loaded by the collector's
  discovery env loader and merged into `Options` before the discovery call.

## Extension points

- `SupportedFileMatcher` — replace with any predicate to change which files
  discovery accepts. The registry-backed matcher is the production default.
- `Options.IgnoredPathGlobs` and `Options.PreservedPathGlobs` — the primary
  operator-facing extensibility seam for excluding or preserving repo subtrees.

## Gotchas / invariants

- `nearestRepositoryRoot` walks up the directory tree for every file and caches
  results. Files in deeply nested repos with many siblings produce `O(depth)`
  stat calls on first encounter; repeated encounters hit the cache.
- `isExternalSymlink` skips symlinks that resolve outside `scanRoot`. This
  prevents accidental traversal of system paths through symlinked directories
  inside a repo.
- `HonorGitignore` and `HonorEshuIgnore` are applied after `sort.Strings`. The
  sort is stable; filter operations do not re-sort. The caller receives an
  already-sorted slice.
- If `SupportedFileMatcher` is `nil`, `ResolveRepositoryFileSetsWithStats`
  returns an error immediately. This is a programmer error, not an operational
  condition.
- The built-in `IgnoredDirs` cache skip for `.terraform` is a directory-name
  rule, not a substring rule. It should skip Terraform provider caches while
  preserving normal source directories with the same name.

## Related docs

- `docs/docs/reference/local-testing.md`
- `docs/docs/architecture.md` — collector pipeline section
- `go/internal/collector/README.md` — how discovery fits in the snapshot flow
- `go/internal/parser/README.md` — how `SupportedFileMatcher` is built from
  the parser registry
