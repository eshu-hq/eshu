# AGENTS.md — resolutionparity

Scoped agent rules for `go/internal/resolutionparity`, the per-language
resolution-tier parity gate (issue #2226).

- The golden `testdata/resolution_tiers.golden.json` is a **regression snapshot**
  of real parser + reducer output, not a hand-authored target. Never edit it by
  hand. Regenerate with
  `ESHU_UPDATE_RESOLUTION_GOLDENS=1 go test ./internal/resolutionparity -count=1`.
- A golden update is only legitimate alongside an **explained** parser or
  resolver change. Treat a tier shifting toward `repo_unique_name` (weak global
  fallback) or `unspecified` as a likely regression — investigate before
  regenerating.
- The resolution-method vocabulary is owned by `go/internal/codeprovenance` and
  ADR #2222. If you add a resolver branch / method, this gate, the codeprovenance
  vocabulary, and the ADR must change together.
- Keep `languageFixtures` deterministic: sorted file walk, stable repo_id, no
  reliance on map iteration order. The tally is order-independent; keep it so.
- This is test-only code (no runtime/hot path). Do not add Cypher, concurrency,
  or runtime behavior here.
