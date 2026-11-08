# NVD Rolling-Window Commit Evidence (#9877 Path A)

No-Regression Evidence: the activation window mint runs once per derived
plan tick beside the OSV mint (one anchor decode plus the shared
`deriveNVDWindowTarget` math); the commit-time cursor arming runs once per
collected generation in `collectedGeneration`. No hot loop, lock, query,
or goroutine was added. Measured on linux/arm64, Go 1.26.6, shared host,
`BenchmarkCollectedGenerationArmingCost` (100k ops x3, committed in
`go/internal/collector/extensionhost`): non-window scope 395-396 ns/op
(736 B, 4 allocs — the prefix fast-path keeps it to one string comparison
inside the existing channel build), advancing window scope 739-752 ns/op
(944 B, 6 allocs — one snapshot scan plus the writer closure), all
sub-microsecond against the commit transaction they ride. Per advancing
window commit the path adds exactly one `NVDWindowCursorAdvanceQuery` Exec
inside the generation's own transaction (baseline: the writer existed but
had no production caller, so zero cursor writes; after: one CAS advance
per advancing window, zero on hold, refusal, skip, or unchanged). The CAS
keeps the max when two claims planned from one cursor commit in either
order, so no serialization was added. Input shape: window-scope and
non-window generations, verdict payloads in int64 and JSON float64 shapes.
`go test -count=1` exits 0 for `go/internal/vulnsource`,
`go/internal/collector/extensionhost`,
`go/internal/coordinator/vulnerability`, `go/internal/coordinator`, and
`go/internal/collector`; `go build ./...` and `go vet` on the touched
trees are clean. The satellite `_window` resolution and verdict stamp are
covered by `nvd_window_target_test.go` in
eshu-collector-vulnerability-intelligence (root package suite green; the
`cmd/collector` seed-binary failures are pre-existing `/tmp` disk-quota
errors, identical on untouched `origin/main`).

Observability Evidence: no new metrics, spans, or log lines. An advancing
window surfaces through the existing claim outcome (complete generation
under its `_window` scope), the fresh `vulnerability_source_states` NVD
row the host recorder already writes, the single
`vulnerability_nvd_window_cursors` row the armed CAS advances, and the
cursor-lag gauge series the QA watch reads for criterion 5. A held cursor
(no verdict or `ShouldAdvance=false`) is visible as a committing window
generation with no cursor movement on the next cycle.
