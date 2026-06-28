# replay/parserfixture — agent scope

## Owned surface

- `go/internal/replay/parserfixture/` — the parser-fact record/replay flavor.

## Key invariants

- Record at the REAL seam. The emitter MUST build envelopes via
  `collector.ParserFileFactEnvelope` (the exported entry over the Git collector's
  `fileFactEnvelope`), running the real `parser.Engine`. NEVER re-implement
  envelope construction or provenance population here — a re-implementation would
  let production drift from the fixture undetected.
- Provenance is first-class. `SourceURI` is a REQUIRED fixture fact field;
  `LoadFile`/`ParseAndValidate` MUST reject a fixture that drops it. A
  record→replay round-trip MUST reproduce `SourceRef.SourceURI`,
  `SourceRef.SourceRecordID`, and `SourceRef.SourceSystem` exactly. The
  provenance-regression test MUST stay failing-capable: prove it by breaking the
  production assertion path (e.g. blanking `SourceURI` in `source.go`) and seeing
  the test go red, then revert. Do not weaken the assertion to make a red test
  pass.
- Canonical determinism. `Record` MUST be byte-identical on re-record of the same
  tree. It uses `replay.Canonicalize` with the parser payload subtree marked
  opaque so parser output is preserved verbatim while object keys sort and
  generation_id derives. The emitter stamps `replay.DerivedGenerationID(scopeID)`
  so the live generation_id already equals its canonical form (live == replayed).
- `Source` MUST implement `replay.Source` (which embeds `collector.Source`) and
  emit one `CollectedGeneration`, then return `ok=false` to signal exhaustion.
- Synthetic/portable identities only in any COMMITTED fixture. The `file` payload
  embeds the parser output, which carries an absolute `path`; do not commit a
  recording made over an absolute tree as a portable corpus. The round-trip test
  records to a temp dir. A committed corpus needs path normalization first.
- Fixture format version is `"1"`. Increment with a migration note for breaking
  changes; do not silently change the shape.

## Skill routing

- `golang-engineering` for any Go change to this package.
- `eshu-golden-corpus-rigor` if a committed parser-fixture corpus is added or a
  gate begins asserting against it.
- `eshu-diagnostic-rigor` if you add telemetry or measure replay throughput.

## Do not

- Add network calls or SDK imports to this package.
- Re-implement envelope/provenance construction instead of calling the collector
  seam.
- Allow `LoadFile` to succeed when `source_uri` or other required fields are
  missing.
- Couple this package to the R-5 offline tier before R-5 is on `main`; expose the
  `Source`/`NewSource` seam and let R-5 adapt to it.
