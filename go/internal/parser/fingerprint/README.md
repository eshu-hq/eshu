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
(#6834). Do not change them
without re-running the precision study and updating that evidence doc.

## Notes

- Whitespace never appears as tree-sitter leaves, so no whitespace
  normalization applies.
- Comments are excluded via the per-language comment-kind sets
  (`tables` for full tiers, `exactOnlyComments` for exact-only tiers).
- An empty Go body (`{}`) yields exactly the two brace leaves.
