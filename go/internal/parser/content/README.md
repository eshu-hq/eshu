# Content

## Purpose

The content package owns file content classification for the parser engine:
what kind of artifact a file is, which template dialect it uses, and whether
it is infrastructure-as-code relevant. It also owns the skip gate that lets
`ParsePath` avoid the inference cost for files that cannot produce a
non-zero classification (issue #4768).

## Ownership boundary

Pure inference over path plus bytes. The package owns `Infer`, `ShouldSkip`,
`Metadata`, and `SplitSuffixes`. It does not own parsing, dispatch, or
payload shape — callers in `internal/parser` map the result onto payloads.

## Exported surface

- `Infer(path, content) Metadata` — classify one file
- `ShouldSkip(path, content) bool` — sound skip check for `Infer`
- `Metadata` — artifact type, template dialect, IaC relevance
- `SplitSuffixes(path) []string` — lower-cased dot-suffixes, shared with the
  raw-text language fallback

See `doc.go` for the full contract.

## Dependencies

Standard library only. Must not import `internal/parser`; the engine calls
in, never the reverse.

## Telemetry

None of its own. Inference cost is visible through the existing file parse
duration metric on the calling path.

## Gotchas / invariants

- `ShouldSkip` must stay conservative: skipping a file `Infer` classifies
  non-zero corrupts the persisted payload. The generative equivalence test
  in `gate_test.go` is the guard; extend its signal cartesian product when
  adding a new artifact family, don't narrow the gate by reasoning.
- Suffix checks run over every dot-suffix `SplitSuffixes` returns, not just
  the last one. A last-suffix-only check misclassifies multi-dot names like
  `vars.tf.json`.
- The `BenchmarkParsePathLargeGatedPHP` end-to-end bench lives in the parent
  package with the call site it measures, not here.

## Related docs

- `docs/internal/naming.md` (rules 2 and 3 behind the `content/` split)

## No-Regression Evidence: content/ split moves inference with identical results

The `content/` split moves `Infer`/`ShouldSkip` out of the parser root
without changing what they compute: the engine call sites repoint to the
same functions, and the generative equivalence test in `gate_test.go`
pins gate-equals-infer across the signal cartesian product.

- Baseline: origin/main 882dd9d, linux/arm64, `go test -benchtime=1x`
  (single-shot execution parity, not a timing claim).
- After: this branch at 4e5ddba5a, same host, same command. Commits
  after that point touch comments and docs only, so the measured code
  tree is unchanged.
- Input shape: synthetic large gated PHP (400 methods), large nginx
  config (400 blocks), large gated PHP through `ParsePath`.
- Measurement: Unconditional 2630301 → 2535518 ns/op; Gated 127871 →
  129500 ns/op; ParsePath end-to-end 136415856 → 138731559 ns/op.
  Same order of magnitude throughout; the end-to-end figure is dominated
  by `ParsePath` fixed costs outside inference.
- Telemetry: no new metric, span, or log line; inference cost stays under
  the existing file parse duration metric on the calling path.

## No-Observability-Change: no new metric, span, or log line

This package emits no metric, span, status field, or runtime knob of its
own. It is diagnosed through the existing file parse duration metric that
already covers the calling path.
