# parser/fingerprint

Language-generic tree-sitter leaf-walk fingerprints for function bodies,
feeding the code-divergence report (epic #6833).

## Contract

`FingerprintBody(lang, body, src) (*Result, error)` walks the leaf nodes of
one function body and returns:

- `Exact`: sha256 over the `kind+text` leaf stream, comments excluded.
- `Renamed`: sha256 with identifiers and literals replaced by positional
  placeholders (`I0`, `L0`, ...). Empty unless `RenamedSupported`.
- `Sketch`: 128 MinHash registers over 5-token shingles (stdlib FNV-1a +
  splitmix64 mixing). Nil for exact-only tiers.
- `Bands`: 32 LSH band hashes (4 rows each) over the sketch.
- `TokenCount`: leaf count of the walked body.

Full tiers (renamed + sketch + bands): Go, Python, TypeScript, TSX,
JavaScript, Java. Every other wired language is exact-only: exact hash plus
token count, no renamed hash, sketch, or bands. Comments are excluded from
the exact stream on every wired tier (both streams on full tiers); only
truly unknown languages keep comment leaves in the stream.

Tunables (`SketchRegs=128`, `ShingleK=5`, `LSHBands=32`, `LSHRows=4`) are
fixed by the #6834 theory proof in
`docs/internal/evidence/6834-code-divergence-theory.md`. Do not change them
without re-running the precision study and updating that evidence doc.

## Notes

- Whitespace never appears as tree-sitter leaves, so no whitespace
  normalization applies.
- Comments are excluded via the per-language comment-kind sets
  (`tables` for full tiers, `exactOnlyComments` for exact-only tiers).
- An empty Go body (`{}`) yields exactly the two brace leaves.

## Evidence

- No-Regression Evidence: B-7 golden-corpus gate on this tree (local
  profile, same corpus/profile/topology/storage as the named baseline):
  560 pass, 0 required-fail, pipeline wall 2m55s (baseline 15m0s, ceiling
  30m0s); phase_collect 5.0s (baseline 20.0s), phase_first_drain 67.0s
  (baseline 75.0s), phase_graph_query 4.0s (baseline 3.0s, ceiling 8.0s).
  Fingerprint work is one bounded leaf walk per function body, skipped
  below `MinTokenCount` (50) and on error parses, so collector and drain
  timings stay within baseline bands.
- Observability Evidence: new instruments
  `eshu_dp_code_fingerprint_entities_total` (fingerprinted-vs-skipped
  counter by outcome, reason, and language) and
  `eshu_dp_code_fingerprint_duration_seconds` (per-file fingerprint-time
  histogram), emitted from `recordFingerprintStats` in
  `go/internal/collector/gitrepo/git_snapshot_prescan_stats.go`.
