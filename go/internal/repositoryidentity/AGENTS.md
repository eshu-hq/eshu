# AGENTS.md — internal/repositoryidentity guidance for LLM assistants

## Read first

1. `go/internal/repositoryidentity/README.md` — purpose, exported surface,
   invariants
2. `go/internal/repositoryidentity/identity.go` — `Metadata`, `MetadataFor`,
   `NormalizeRemoteURL`, `RepoSlugFromRemoteURL`, `CanonicalRepositoryID`;
   the entire surface fits in one file
3. `go/internal/collector/git_fact_builder.go` — main caller; shows how
   `MetadataFor` feeds fact emission

## Invariants this package enforces

- **Remote-first identity** — `CanonicalRepositoryID` at `identity.go:105`
  hashes the remote URL when present. The local path is a fallback only. This
  means two checkouts of the same remote produce the same ID even if the local
  paths differ.
- **Both fields empty is an error** — `identity.go:108` returns an error when
  both `remoteURL` (after normalization) and `localPath` are empty. Never
  construct an ID silently from a zero-value input.
- **`repository:r_` prefix is canonical** — the `fmt.Sprintf` at
  `identity.go:115` always produces `repository:r_<8-hex>`. Graph node
  MERGE keys and fact payload consumers depend on this prefix.
- **Normalization is one-way** — `NormalizeRemoteURL` is idempotent on
  already-normalized URLs but destructive on non-URL inputs (e.g., bare paths).
  Only pass git remote strings to it.

## Common changes and how to scope them

- **Change the ID prefix** — update the `fmt.Sprintf` at `identity.go:115`
  and update every downstream fact schema, graph constraint, and test that
  asserts on the `repository:r_` prefix. This is a breaking change; check all
  callers before proceeding.

- **Add normalization for a new remote protocol** — extend the switch in
  `NormalizeRemoteURL` (`identity.go:60`). Add a test case in the table-driven
  test before implementing.

- **Add a field to `Metadata`** — add to the struct at `identity.go:13`,
  populate in `MetadataFor`, and add test coverage. Fields that require
  additional I/O (e.g., fetching remote metadata) do not belong here; this
  package must remain pure value logic.

## Failure modes and how to debug

- Symptom: two checkouts of the same repo produce different `ID` values →
  likely cause: one caller is passing the raw un-normalized remote URL and the
  other is passing a normalized form → fix: ensure both callers call
  `MetadataFor` or pass the same normalized URL to `CanonicalRepositoryID`.

- Symptom: `MetadataFor` returns `resolve local path: ...` error →
  cause: `filepath.Abs` failed — unusual but can happen with nil-context
  environments → fix: pass an absolute path directly if the working directory
  is not reliable.

- Symptom: graph node lookup misses on `r.id` property → likely cause: the
  fact payload uses a different normalization path than the graph write →
  fix: verify both paths call `CanonicalRepositoryID` with the same inputs.

## Anti-patterns specific to this package

- **Adding I/O** — this package must remain a pure value library. No
  network calls, no file reads, no Postgres.
- **Constructing `Metadata` manually** — always use `MetadataFor` so
  normalization and ID derivation stay consistent.
- **Comparing IDs as strings before normalization** — normalize remote URLs
  before comparing. Two syntactically different URLs may produce the same ID.
