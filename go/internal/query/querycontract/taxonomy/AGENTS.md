# Agent instructions: querycontract/taxonomy

Vocabulary maps and pure functions. No reader, no I/O, no telemetry.

- Never import this package from `querycontract`; the parent is imported here
  for `EntityContent` and `RepositoryLanguageCount`, so the reverse import is a
  cycle.
- The exported maps are shared, read-only package state. Never write to them
  at runtime; add an entry here in source instead.
- Adding a language spelling goes in `language.go`'s alias table, so every
  route normalizes it the same way.
- Decode rows with `rowvalue` directly, not the parent's forwarders.
- The exported names keep their pre-move spelling (`LanguageEntitySearch` and
  friends) from the move out of `querycontract` (#6597). Renaming them is a
  separate change.

Verify with `cd go && go vet ./... && go test ./internal/query/... ./internal/queryplan/... -count=1`.
