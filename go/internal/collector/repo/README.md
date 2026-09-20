# Collector repo contracts

## Purpose

`collector/repo` groups the collector-side packages that read
repository-local state. It is a documentation-only namespace;
implementation belongs in its leaf packages and collection behavior stays
in the owning collector packages.

## Ownership boundary

The namespace owns no runtime declarations, extraction, provider access,
fact emission, storage calls, graph writes, or telemetry. Its `git`
child owns only git history scanning and ref selection. Its `discovery`
child owns only repo-root file-set resolution with `.gitignore` and
`.eshuignore` handling. Its `submodule` child owns only `.gitmodules`
parsing. Its `codeowners` child owns only `CODEOWNERS` rule parsing.

## Exported surface

None. This parent is documentation-only. See the `git`, `discovery`,
`submodule`, and `codeowners` children for their exported surfaces.
