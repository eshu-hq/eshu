# Collector Discovery

## Purpose

`discovery` resolves parser-supported files inside a checked-out repository
into stable repo-root file sets before the Git collector snapshots and parses
files.

## Ownership boundary

This package owns filesystem discovery, repo-root grouping, ignore handling,
external-symlink rejection, deterministic sorting, and `DiscoveryStats`. It does
not own parser behavior, fact emission, workspace sync, or telemetry emission.

## Exported surface

Use `doc.go` and `go doc ./internal/collector/discovery` for the contract. The
main surfaces are `ResolveRepositoryFileSets`, `ResolveRepositoryFileSetsWithStats`,
`Options`, `PathGlobRule`, `DiscoveryStats`, `RepoFileSet`, and
`SupportedFileMatcher`.

## Dependencies

`discovery` imports only the Go standard library. Callers provide parser support
through `SupportedFileMatcher`.

## Telemetry

This package does not emit metrics or spans. It returns `DiscoveryStats`; the
collector snapshotter records those counters with
`eshu_dp_discovery_files_skipped_total`.

## Gotchas / invariants

- `SupportedFileMatcher` is required. A nil matcher returns an error.
- `RepoFileSet.RepoRoot` and `RepoFileSet.Files` are absolute paths.
- File lists are sorted for stable parsing and fact emission.
- When no supported files are found, discovery still returns one file set for
  the scan root with an empty file list.
- Root-anchored `.gitignore` and `.eshuignore` patterns stay rooted at the
  discovered repo root. Do not treat `/name` as a suffix match.
- Symlinks that resolve outside the scan root are rejected.
- User path globs can prune broad subtrees, but preserved globs may keep a
  narrower subtree indexable.

## Related docs

- `go/internal/collector/README.md`
- `go/internal/parser/README.md`
- `docs/public/reference/local-testing.md`
- `docs/public/architecture.md`
