# Evidence: JS/TS/PHP Parse Byte Cap (#4766)

## Summary

Full-corpus discovery on the 896-repo corpus found 93 files over 512 KiB that
survive discovery and reach parse. A microbenchmark against real pathological
files confirmed superlinear tree-sitter parse cost on JavaScript/TypeScript/TSX
and PHP, the same defect class as the SQL segment byte cap
(`go/internal/parser/sql/segments.go`, #4422). This change adds a 1 MiB
per-file parse byte cap to the javascript-family parser
(`go/internal/parser/javascript/javascript_language.go`) and the PHP parser
(`go/internal/parser/php/parser.go`). A file over the cap has its tree-sitter
parse skipped entirely; the bound is recorded in
`payload["js_parse_bounded"]` / `payload["php_parse_bounded"]` and logged.

## Performance Evidence:

Microbenchmark (`Engine.ParsePath`) on real pathological files copied locally,
measured on this machine (`go test ./internal/parser -run
TestManualPerfCheckRealFiles`, throwaway harness, not committed):

| File | Size | Before (uncapped) | After (capped) |
| --- | --- | --- | --- |
| `mpdf.php` (mPDF library, real repo checkout) | 1,312,477 bytes (1.25 MiB) | 2.11s | 50ms |
| `axe.js` (axe-core, real `node_modules` checkout) | 1,264,134 bytes (1.21 MiB) | 3.60s | 55ms |

Prior PROVE-FIRST gate microbenchmarks (cited in the originating task) on
additional pathological files from the 896-repo corpus:

- 2.7MB WordPress webpack bundle: 15.9s (~224x a normal parse).
- 3.4MB xml2js.bc.js: 5.1s.
- 1.5MB TCPDF CID font-map: 6.0s (~273x).
- 1.3MB mPDF: 2.5s.

93 files over 512 KiB survive discovery on the 896-repo corpus (PROVE-FIRST
gate), confirming this is a recurring full-corpus cost, not an isolated
outlier.

Classification: **Handler win** (bounds worst-case per-file parse handler cost
from seconds to tens of milliseconds on the pathological tail). Full-corpus
wall-clock impact was not separately re-measured in this change; the
per-file cost reduction directly removes the multi-second-per-file tail
identified in the PROVE-FIRST gate.

Equivalence proof (output-preserving for the common case, intended-delta for
the bounded case):

- Under-cap files (the overwhelming majority of real source) are
  byte-for-byte unaffected: `TestParsePathJSSmallFileUnaffected` and
  `TestParsePathPHPSmallFileUnaffected` assert the parse output (functions
  extracted) is unchanged and no `*_parse_bounded` entry appears.
- Over-cap files are an intended behavior change (the parse is skipped, not
  merely slow): `TestParsePathJSBoundsOversizedFile`,
  `TestParsePathTSBoundsOversizedFile`, `TestParsePathTSXBoundsOversizedFile`,
  and `TestParsePathPHPBoundsOversizedFile` assert the `*_parse_bounded`
  marker is present and no entities are extracted from the bounded file.
- Confirmed the tests are a real guard, not a false green: with the byte-cap
  checks reverted, all four bounded-file tests fail (`js_parse_bounded =
  empty` / `php_parse_bounded = empty`), because the un-bounded code path
  genuinely completes the tree-sitter parse and extracts `generatedFn*`
  functions from the synthetic oversized fixtures.

## Observability Evidence:

- A bounded file appends a row to `payload["js_parse_bounded"]` or
  `payload["php_parse_bounded"]` (`path`, `original_bytes`, `action`:
  `file_skipped`), mirroring the SQL `sql_parse_bounded` marker shape.
- A bounded file emits a structured `slog.Warn` log line
  (`component=parser.javascript` or `component=parser.php`, `path`,
  `original_bytes`, `action=file_skipped`) so an operator can find a dropped
  parse in logs without inspecting payloads directly.

## Golden Corpus

No fixture or cassette file under `testdata/` or `tests/fixtures/` exceeds
512 KiB (checked via `find ... -size +900k`), so no existing golden-corpus
input crosses the 1 MiB cap. `scripts/test-verify-golden-corpus-gate.sh`
stays green without a snapshot change.
