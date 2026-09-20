# AGENTS.md — go/internal/reducer/codedivergence

## Read first

1. `go/internal/reducer/codedivergence/README.md` — pipeline and scaling bounds
2. `go/internal/reducer/codedivergence/doc.go` — the package contract
3. `docs/internal/evidence/6834-code-divergence-theory.md` §5 and §6 — the measured precision, threshold, and K=200 budget this package implements
4. `go/internal/reducer/AGENTS.md` — intent lifecycle, generation supersession, domain registration invariants
5. `go/internal/query/codedivergence/AGENTS.md` — suppression catalogue rules reused here, not duplicated

## Rules that shape this package

- No import of the reducer root, ever. This package is a leaf below
  `internal/reducer`: the root imports it for handler wiring, never the reverse.
- Threshold and budget constants change only with a re-run precision study
  and an evidence-doc update. They are measured bounds, not tunables.
- Suppression is counted, never silent. Reuse the per-rule functions and
  counters from `query/codedivergence`; add drift-specific reasons with
  pair tests, never a copy of a rule.
- Findings are truth level `derived`, never `exact`.
- Never read `source_cache`; verification reads the narrow `shingles`
  column and member entity rows only.
- Work is partitioned by `repo_id`. Writes are idempotent under retry and
  duplicate delivery. Reducing workers, batch size 1, or a serial drain is
  not an accepted fix for any conflict found.
