# Vulnerability Source State

This package defines the shared, durable state contract for bounded
vulnerability intelligence source targets. It deliberately carries no storage
or runtime dependencies.

The state distinguishes configured source targets that are pending, fresh,
stale, rate limited, failed, or partial. Collectors populate the contract from
claimed source attempts, Postgres persists it with keyed upserts, and status/API
surfaces render it so empty result sets are not confused with missing source
configuration.

## Scope ids and sources

`SourceForScopeID` is the one parser from a vulnerability scope id to the
`source` token stored in `vulnerability_source_states` (#9420). The grammar is
core-owned: the workflow coordinator mints `vuln-intel://osv/<ecosystem>/...`,
and configured targets use `vuln-intel://nvd/...`, `vuln-intel://cisa/kev`,
`vuln-intel://first/epss`, and `vuln-intel://glad/<ecosystem>`. It returns
`ok=false` for anything else, so a writer never invents a token. Tests in
`coordinator/vulnerability` and `query/supply/chain/impact` pin it against the
coordinator's format strings and the readiness reader's scope table.
`DefaultFreshnessWindow` (14 days) is the shared fresh-versus-stale window for
the in-tree collector and the extension host recorder.
