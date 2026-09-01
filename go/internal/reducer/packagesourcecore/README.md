# Reducer package-source core

## Purpose

`packagesourcecore` owns the primitives package-registry source-hint
correlation and its callers reduce to: the `Hint` and `Repository` shapes, the
repository extraction that reads them out of a fact-envelope batch, and the
canonical-URL matching that decides whether a hint's source URL names an
active repository.

## Ownership boundary

This package owns:

- `Hint` — one package registry `source_hint` fact, reduced to package,
  version, hint kind, and source URL.
- `Repository` — one repository fact, reduced to ID, name, remote URL, and
  tombstone state.
- `ExtractRepositories` — reads `Repository` values out of a fact-envelope
  batch, deriving each repository's ID from its payload or, failing that, its
  scope.
- `RepositoryIDFromScope` — the scope-ID fallback `ExtractRepositories` uses
  when a repository fact carries no explicit ID field.
- `MatchRepositories` — partitions repositories into active and tombstoned
  matches for one hint's canonical source-URL key.
- `CanonicalURLKey` — the canonical host/path key a git remote URL reduces to,
  shared with the git collector via `repositoryidentity`.

It does not own hint extraction, correlation-outcome classification, or the
decision types the reducer root's package-source correlation handler
produces. Those stay in `internal/reducer` because hoisting them would drag the
`PackageSourceCorrelationDecision` type and the classification logic that reads
it into a leaf whose budget is the shared shapes and matching helpers.
`package_publication_correlation.go` calls `extractPackageSourceHints` and
`classifyPackageSourceHint` directly today, so they are not handler-exclusive.

## Why a leaf and not a family move

`BuildPackageSourceCorrelationDecisions` and the handler that classifies a
hint into a correlation outcome are called only from
`package_source_correlation.go` and `package_source_correlation_handler.go`
themselves (649 lines together). Seven other reducer-root files read these
symbols directly and never call that handler, each needing a different subset
(verified against actual call sites, not inferred):

| file | symbols it reads |
| --- | --- |
| `package_consumption_correlation.go` | `Repository`, `ExtractRepositories` |
| `package_publication_correlation.go` | `Hint`, `ExtractRepositories` |
| `container_image_identity_provenance.go` | `Hint`, `Repository`, `ExtractRepositories`, `MatchRepositories`, `CanonicalURLKey` |
| `container_image_identity_slsa.go` | `ExtractRepositories` |
| `service_catalog_correlation_classify.go` | `CanonicalURLKey` |
| `service_catalog_correlation_lookup.go` | `CanonicalURLKey` |
| `supply_chain_impact_python_reachability.go` | `RepositoryIDFromScope` |

Moving the whole `packagesource` family would drag the handler's ~650 lines
along to deliver these ~65 (issue #6379, epic #6061).

## Compatibility

The reducer root keeps `type packageSourceHint = packagesourcecore.Hint` and
`type packageSourceRepository = packagesourcecore.Repository` plus forwarders
at the end of `package_source_correlation.go` (not a separate compat file:
that file was already at 199 lines pre-extraction, well under the 500-line
cap, and adding a new root `.go` file would have grown
`internal/reducer`'s dirgate-pinned non-test file count past its grandfathered
519 -- the ratchet only allows that row to move down or be removed, never
up), so the root call sites across `package_consumption_correlation.go`,
`package_publication_correlation.go`, `service_catalog_correlation_classify.go`,
`service_catalog_correlation_lookup.go`, `container_image_identity_provenance.go`,
`container_image_identity_slsa.go`, and `supply_chain_impact_python_reachability.go`
are unchanged. Those forwarders are transitional and are deleted as their
callers move into family subpackages.
