# AGENTS.md - internal/parser/content guidance

## Read first

1. README.md - package boundary and the #4768 gate rationale
2. doc.go - godoc contract for the content package
3. infer.go - `Infer`, `Metadata`, suffix/family/artifact taxonomy
4. gate.go - `ShouldSkip` and the gated extension/segment/marker sets

## Invariants this package enforces

- Dependency direction stays one way: the parent `internal/parser` package
  may import this package, but this package must not import
  `internal/parser`. It depends only on the standard library.
- Exported identifiers do not repeat the package name (`Infer`, not
  `InferContent`; `Metadata`, not `ContentMetadata`) per
  `docs/internal/naming.md` rule 4.
- `ShouldSkip(path, content) == true` must imply
  `Infer(path, content) == Metadata{}`. The generative equivalence test in
  `gate_test.go` proves it across the signal cartesian product; any new
  artifact family extends that product.
- Suffix matching uses every entry of `SplitSuffixes`, never only the last
  dot-suffix. The raw-text fallback in the parent package shares
  `SplitSuffixes` so both route on identical semantics.
