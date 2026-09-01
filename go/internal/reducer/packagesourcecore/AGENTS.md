# Reducer package-source-core package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/packagesourcecore/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- Remain a leaf below `internal/reducer`. Never import the parent `reducer`
  package or a family subpackage. Budget: `internal/facts`,
  `internal/reducer/factload`, `internal/reducer/payloadcore`,
  `internal/repositoryidentity`, and the standard library.
- `RepositoryIDFromScope` must keep returning the whole trimmed scope ID when
  the scope carries no `git-repository-scope:` prefix, NOT `""`. It differs
  from `payloadcore.RepositoryIDFromReducerScope` on purpose:
  `ExtractRepositories` uses it only as the last fallback after graph_id,
  repo_id, and repository_id payload fields all come up empty, and narrowing
  it to `""` would silently drop repositories that today extract an ID from an
  unprefixed scope.
- `MatchRepositories` must keep partitioning by `Tombstone`, not filtering
  tombstoned repositories out. The correlation handler in the reducer root
  reports a distinct "stale" outcome when a hint matches only tombstoned
  repositories, and that outcome depends on seeing the stale matches
  separately from the active ones.
- `CanonicalURLKey` must keep delegating to
  `repositoryidentity.NormalizedRemoteKey`. Reimplementing normalization here
  would let the reducer's remote-URL key drift from the git collector's.

## Common changes

Adding a new repository payload ID field: extend the `FirstNonBlank` call in
`ExtractRepositories`, keeping `RepositoryIDFromScope` as the last argument so
it stays the final fallback.

## Failure modes

- Treating `RepositoryIDFromScope`'s looser (non-`""`) fallback as a bug and
  "fixing" it to match `payloadcore.RepositoryIDFromReducerScope` changes
  which repositories `ExtractRepositories` returns for facts whose scope is
  neither `repository:`- nor `git-repository-scope:`-prefixed.
- Filtering `MatchRepositories`'s tombstoned matches out instead of returning
  them separately collapses the "stale" correlation outcome into "unresolved"
  in the reducer root's classification.
