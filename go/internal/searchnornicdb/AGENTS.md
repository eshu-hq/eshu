# Agent Notes for `go/internal/searchnornicdb`

This package adapts NornicDB search responses into Eshu's internal
`searchretrieval.Backend` contract. Keep it small, internal, and issue #417
scoped.

## Mandatory Rules

- Do not expose HTTP, MCP, OpenAPI, or CLI surfaces from this package.
- Do not return canonical graph truth. Candidates must remain derived search
  documents with truth labels.
- Do not query the whole graph. Retrieval must stay scoped to the explicit
  `SemanticContext` label and must reject label escapes.
- Do not accept NornicDB fallback as hybrid retrieval proof. Reject
  non-hybrid `search_method` values and `fallback_triggered=true`.
- Do not expand graph neighborhoods here. Return only candidate graph handles;
  bounded graph expansion belongs to a later query/retrieval stage.
- Keep tests focused on request bounds, label scope, truth labels, deterministic
  candidate mapping, fallback rejection, and scope rejection.

## Verification

Run:

```bash
go test ./internal/searchnornicdb -count=1
```

When this package is wired into live runtime code, also add the applicable
runtime, telemetry, and performance gates named by the repository root
`AGENTS.md`.
