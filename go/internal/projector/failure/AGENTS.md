# AGENTS.md — failure classification and triage guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `docs/public/reference/telemetry/index.md` for the metric dimensions these
   classes feed.

## Invariants

- **This package is a leaf.** It must not import `../canonical`, `../runtime`,
  `../stage`, `../decode`, or the root `projector` package.
- `FailureClass` and `TriageClass` are bounded metric dimensions. Adding a
  value is a telemetry-contract change, not a local edit: update the telemetry
  coverage doc in the same PR and use `telemetry-coverage-discipline`.
- `ClassifyFailure` falls back to the projection-bug class for an error it does
  not recognize. Keep it that way. Widening the transient branch to "anything
  that looks like a network error" turns a real defect into an invisible retry
  loop — the "make it reliable by hiding wrong results" failure the root
  AGENTS.md forbids.
- `TriageDispositionConflicts` exists so a retryable classification and a
  terminal triage disposition cannot silently disagree.
  `TestTriageDispositionIsConsistent` pins it; do not relax it to make a new
  class fit.
- `RetryOnceInjector` is fault injection the Ifá gates drive. It must stay
  inert unless explicitly configured.

## Verification

```bash
cd go && go test ./internal/projector/failure/... -count=1
cd go && go test ./internal/projector/... -count=1
```

A class or disposition change also needs the Ifá dead-letter matrix, which
runs in CI (`required-gates-complete`), not locally.
