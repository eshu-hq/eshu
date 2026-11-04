# Vulnerability Source State

This package defines the shared, durable state contract for bounded
vulnerability intelligence source targets. It deliberately carries no storage
or runtime dependencies.

The state distinguishes configured source targets that are pending, fresh,
stale, rate limited, failed, or partial. Collectors populate the contract from
claimed source attempts, Postgres persists it with keyed upserts, and status/API
surfaces render it so empty result sets are not confused with missing source
configuration.

A derived target's state also carries `PackageKeys`, the canonical package
identities it queried, with `ReplacePackageKeys` telling the store to replace
the scope's stored keys in the same upsert. Readiness matches requested
packages against those keys rather than parsing scope ids.

## Scope ids and sources

`SourceForScopeID` is the one parser from a vulnerability scope id to the
`source` token stored in `vulnerability_source_states` (#9420). The grammar is
core-owned: the workflow coordinator mints `vuln-intel://osv/<ecosystem>/...`,
`vuln-intel://nvd/_window/<start>-<end>`, and
`vuln-intel://first_epss/_findings/<chunk>`, and configured targets use
`vuln-intel://nvd/...`, `vuln-intel://cisa/kev`, `vuln-intel://first/epss`,
and `vuln-intel://glad/<ecosystem>`. It returns `ok=false` for anything else,
so a writer never invents a token. Tests in `coordinator/vulnerability` and
`query/supply/chain/impact` pin it against the coordinator's format strings
and the readiness reader's scope table. The coordinator test lists one row per
derived scope shape and fails when a non-test file in `coordinator/vulnerability`
has a `vuln-intel:` string literal whose authority has no row or cannot be read
as a literal, so a new authority cannot ship unmapped (#10009). It does not
catch a new path shape under an authority that already has a row, or a scope
minted outside that package: add the row by hand when you mint one.
`DefaultFreshnessWindow` (14 days) is the shared fresh-versus-stale window for
the in-tree collector and the extension host recorder.

## Coordinator-minted scope shapes

The extension host and the coordinator read these shapes without a collector:

- `ParseDerivedOSVSlotScope` and `IsDerivedOSVSlotScope` validate a pinned slot
  scope, `vuln-intel://osv/<ecosystem>/_slot/<vslot>/<chunk>` (#9432). The
  queries behind it ride the run's `requested_scope_set`, never the scope id.
- `DerivedScopePackageKeys` returns the package keys of a single-package or
  `_batch` derived OSV scope, with the claim path's bounds: 100 queries, blanks
  and repeats dropped. `SlotScopePackageKeys` does the same for a slot claim's
  resolved names. An ecosystem `packageidentity` cannot normalize yields no keys,
  so the scope reads as unmatched rather than guessed.
- `NVDWindowScopeID` and `ParseNVDWindowScope` mint and read
  `vuln-intel://nvd/_window/<startMicros>-<endMicros>` (#9971). Same bounds mint
  the same id, and degenerate bounds mint `""`. `NormalizeNVDWindowTime` is UTC
  truncated to the millisecond, the form the coverage descriptor persists.

`derivedOSVVirtualSlotCount` duplicates `workflow.OwnedPackageVirtualSlotCount`
so this package stays free of the reducer tree behind `workflow`; a test pins the
two together.

These functions do no I/O and hold no state, so they add no telemetry of their
own.
