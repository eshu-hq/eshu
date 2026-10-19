# #8506 prior-generation field-comment sweep — no-regression note

Owner P2 follow-up from PR #8500 (#8167): 17 materializer
`PriorGenerationCheck` field comments said "any prior generation" while
production wires them to the strictened `NewPriorGenerationCheck`. This
change rewords those comments to "a generation strictly older than the
given generation", matching the contract docs
(`go/internal/reducer/contract/generation_check.go`,
`go/internal/reducer/runtime.go`). Comment-only; no logic change.

No-Regression Evidence: `git diff origin/main..HEAD --stat` shows 17 files,
+18/-18, and every changed line is a `//` comment (`git diff` inspected;
`rg '^\+.*any prior generation'` on the diff has no match). `gofmt -l` on the
touched tree is clean. Comments emit no code, so no benchmark or
before/after measurement applies; the reducer contract tests that pin the
strictened predicate are unchanged and unowned by this diff.

No-Observability-Change: no metric, span, log, status, or telemetry surface
touched.
