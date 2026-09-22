# Agent instructions: internal/status/semantic

Scope: `go/internal/status/semantic` only. The parent package's instructions
in `go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import `internal/status` (the root) or any sibling
leaf. The root aggregates the family leaves into `RawSnapshot`/`Report`, so
root and leaves both import `semantic`; an import in the other direction is
an import cycle. If you find yourself needing `Reader`, `RawSnapshot`,
`SnapshotSelection`, or `FullSnapshotSelection` here, stop — those are root
types, and the code that needs them belongs in root, not in this package.

## Only the type half of semantic_provider_profile.go moves here

`semantic_provider_profile.go` splits on the move. This package gets:
`SemanticProviderProfileStatus` (as `ProviderProfileStatus`), its state
constants, `SemanticProviderProfileSupportedStates`, and the
normalize/query helpers (`normalizeSemanticProviderProfile`,
`isSemanticProviderProfileState`, `defaultSemanticProviderProfileReason`,
`semanticProfileConfigured`, `semanticProfileUnhealthy`,
`semanticProfileAllowsSource`, `cloneSemanticProviderProfiles`).

`WithSemanticProviderProfiles` and `semanticProviderProfileReader` (the
`Reader`-wrapping decorator that attaches a static profile roster to a live
status read) stay in root: they implement `Reader`/`ReadinessChecker` and
mutate `RawSnapshot.SemanticExtraction.ProviderProfiles`, which are root
types this package cannot import. Root's decorator calls
`cloneSemanticProviderProfiles` to normalize the roster it attaches — export
it (for example as `CloneProviderProfiles`) before deleting the alias in
`status/compat_semantic.go`, or root's call site will not compile.

## Exporting the render/JSON surface

`renderSemanticExtractionLine` and `semanticExtractionStatusJSON` are called
directly from root `status.go`/`json.go` and are unexported today. Export
both (and anything they call that also needs cross-package visibility) on
the move.

## Changing the wire helpers

`ExtractionStatusJSON`, `ProviderProfilesJSON`, and the queue/budget/audit
JSON helpers render operator-facing status JSON consumed by the status HTTP
surfaces and MCP status tools. Their struct tags and time formatting are the
published contract.

Before changing any of them, know that they are locked by byte-for-byte
goldens: `internal/status/testdata/render_json_golden.json`,
`render_text_golden.txt`, and the explicit dotted-key-path list in
`render_json_key_paths_golden.txt`. A failure there is a real API break to
justify, not a golden to regenerate reflexively.

## Preserve the "unaffected" language

Every non-available `ExtractionStatus` detail states that deterministic
indexing, reducer projection, API reads, MCP tools, and documentation fact
verification are unaffected by extraction being off. This is a deliberate
operator-safety property, not incidental copy — keep it true, and keep
saying it, for any new state or detail string you add.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
```

Run the recursive `./internal/status/...` path, not the bare package — the
goldens that prove this leaf's wire contract live in the parent package's
tests.
