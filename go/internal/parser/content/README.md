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
