# OCR Documentation

## Purpose

`collector/document/ocr` turns reviewed local OCR output for image documentation
artifacts into source-neutral documentation document and section facts. It
exists so image OCR can be tested and read back without enabling a hosted
collector path or treating screenshot text as operational truth.

## Ownership boundary

This package owns the post-preflight OCR fact boundary for `.png`, `.jpg`,
`.jpeg`, and first-frame `.gif` inputs. It calls `picture` preflight first, invokes
only an injected OCR engine for supported inputs, redacts sensitive-looking OCR
regions, and emits `documentation_document` plus OCR-region
`documentation_section` envelopes. Emitted document and section metadata carries
`incident_media_source_class=ocr_region` so downstream correlation can keep OCR
evidence separate from stronger operational truth.

It does not discover repositories, add runtime flags, create temp files, call
cloud OCR or vision APIs, persist raw pixels, emit graph edges, infer service or
deployment truth, create documentation mentions, or create claim candidates.
Runtime enablement still requires sandbox, dependency, telemetry, ACL, and
security review.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Engine` is the injected local OCR boundary.
- `Request` describes one image artifact revision and durable scope metadata.
- `Options` forwards image preflight limits and section text bounds.
- `Image`, `EngineResult`, `Region`, and `Bounds` describe bounded OCR input and
  output without source-specific types.
- `Extract` runs preflight, calls the engine when allowed, and returns
  ready-to-persist fact envelopes.
- `LocalEngine` (with `LocalOptions`) is the in-tree deterministic fixture
  backend and fail-closed fallback: dependency-free 5x7 template matching
  over PNG, JPEG, and first-frame GIF. No network, subprocess, temp file,
  cgo, or logging.
- The general-purpose backend is the `tesseract` subpackage: a pinned
  tesseract 5.5.0 LSTM binary run out of process per image, with its own
  bounds, digest pinning, and OCR telemetry. See its README and
  `docs/internal/design/9728-ocr-engine-backend.md`.

## Local engine bounds

`LocalOptions` caps every recognition; non-positive fields take defaults:

| Field | Default | Meaning |
|---|---|---|
| `MaxInputBytes` | 8 MiB | Rejects larger encoded bodies before decoding |
| `MaxPixels` | 8,000,000 | Rejects larger decoded images (bounds allocations) |
| `Timeout` | 10s | Bounds one `Recognize` call; context cancel always honored |
| `MaxRegions` | 512 | Fails past the cap instead of silently truncating |
| `MinConfidence` | 0.60 | Minimum template match fraction kept as text |

Recognized charset: `A-Z 0-9 @ . , - _ : / +`. Lowercase, proportional
fonts, and tight multi-line pitch are known QA-tier limits: unsupported
glyphs split words and are skipped, and close line bands may merge.
Recognized text reaches facts only through `Extract`, which redacts
sensitive regions before persistence.

Fixture evidence for this backend (it does not discharge gate 2 of #8879;
the general-purpose backend is the `tesseract` subpackage):

```sh
cd go && go test ./internal/collector/document/ocr -count=1
cd go && go test -race ./internal/collector/document/ocr -count=1
```

The engine tests prove bounded recognition, cap rejection, cancellation,
determinism, template-free input handling, and end-to-end PII redaction
before facts.

Benchmark Evidence: `go test -run=NONE
-bench=BenchmarkLocalEngineRecognize -benchmem
./internal/collector/document/ocr -count=1` on backend `eshu-local-ocr`
0.1.0 recognizes a two-word 232x44 fixture as 2 regions in ~0.12 ms/op at
~106 KB/op and 90 allocs/op (down from ~10298 allocs/op via packed Pix
sampling), bounded by the 8 MiB input, 8M pixel, 10s, and 512-region caps.

No-Regression Evidence: `go test ./internal/collector/document/ocr
-count=1` and `go test -race ./internal/collector/document/ocr
./internal/collector/preflight/picture -count=1` pass, including the
pre-existing extraction, skip, GIF first-frame, and redaction suites.

No-Observability-Change: this backend emits no metric, span, log, or status
row of its own; extraction throughput stays covered by the git-source
collector metrics that cover documentation export throughput (see the
collector documentation row in `docs/public/observability/telemetry-coverage.md`).

## Label and value redaction

A region is judged on its own text, so a bare value such as `Hunter2` matches no
marker when OCR places it away from its `Password:` label. `Extract` closes that
gap before any content is persisted (#9782). It withholds those values with the
same `[redacted]` content, metadata, and warning as a marker hit. The rule lives
in `labelValueRegions` in `sanitize.go`.

Any region with a marker word from `sensitiveMarkers` opens a value window, not
only one shaped like a label. The words around a marker are the least reliable
part of OCR text, and each shape rule tried here left a hole: `(Password)`,
`Password required`, `Password: I`. The STT twin in `document/media` uses the
same rule (#9786). An email, URL, path, or ticket redacts its own region but
opens no window. It is a value, not a word that introduces one.

A region is a value when either holds:

- Window: it is one of the 2 non-blank regions after a marker word in engine
  order (`labelValueWindow`). One covers a value directly below its label, and
  the second covers a caption or other line between the two. Each marker word
  adds a full window, so stacked labels each cover their own value. A blank
  region does not use up the window, and neither does a region with no letter
  or digit, because box edges reach OCR as `|` or `_`. That region is still
  withheld, since a password can be all symbols.
- Row: it starts past the marker region's right edge and overlaps that region's
  row by at least half the shorter height (`minLabelRowOverlap`). Tesseract
  reads columns as separate blocks, which can put a value many regions after its
  label. The order of the two columns does not matter. An engine that reports no
  height gets the window only.

`Extract` never sorts. `EngineResult.Regions` must be in reading order, because
layout order is more accurate than a position sort on a multi-column page.

The tradeoff is deliberate. Up to 2 ordinary lines after any marker word are
hidden, including after a line that already holds its value, such as
`password: hunter2`, and after prose that mentions a password. That costs a
little documentation evidence. A leaked secret costs more, and ordinary text
that follows no marker word is untouched. Windows add but stay bounded: k marker
regions in a row hide at most k+1 regions after them.

Limits:

- Marker words decide everything, so a label the word list misses opens no
  window: `Secret:`, `API Key:`, and a misread `Passw0rd:` are not markers. In a
  probe of degraded renders, `Password` came back verbatim or was lost whole, as
  in `Confirm passwort`.
- A value more than 2 regions after its marker word, on another row, is not
  caught.
- A table column is not covered. A header row such as `Service Password Owner`
  opens a window over the next rows only, and each cell further down stays in
  clear. That needs a column rule, not this one.

Proof: `TestExtractRedactsValuesThatFollowALabelInReadingOrder`,
`TestExtractRedactsValueInTheColumnBesideALabel`,
`TestExtractFollowsEngineRegionOrder`,
`TestExtractLabelValueRedactionStaysTruthful`, and the `form-stacked.png` and
`form-columns.png` fixtures in `TestSandboxRealTesseract`.

Performance Evidence: `BenchmarkBuildSectionsNoLabels` and
`BenchmarkBuildSectionsLabelHeavy` on 500 regions, the tesseract default cap,
with before and after test binaries run in turn at `-benchtime=2000x`, six runs
each. Medians on a shared host: ordinary text went from 3.06 ms/op to 3.14 ms/op
(13,405 to 13,906 allocs/op, one lowercase per region). The worst case for the
row clause, 250 regions followed by 250 labels, went from 1.88 ms/op to 2.02
ms/op. The row clause is regions times marker regions, so 500 regions bound it.
Either figure is small next to about 200 ms for one tesseract run.

No-Regression Evidence: `go test -race ./internal/collector/document/ocr/...
-count=1` passes, including the pre-existing extraction, skip, GIF, redaction,
and local-engine PII suites.

No-Observability-Change: the rule adds no metric, span, or log. A withheld value
is a redacted section, so `eshu_dp_ocr_redacted_regions_total`, the
`ocr.redacted_region_count` span attribute, and the `redacted_region_count` log
field count it with no change. `TestExtractRedactsValueOnLineBelowLabel` asserts
the count and that no canary reaches logs, spans, or envelopes.

## Dependencies

The package depends on `internal/collector/preflight/picture` for metadata-only
image gating, `internal/facts` for documentation payload and envelope contracts,
and `internal/scope` for the documentation collector kind. It uses only the Go
standard library otherwise.

## Telemetry

None directly. This is a default-off helper with no runtime side effects. A
future runtime integration must add bounded extraction attempt metrics, warning
class counts, elapsed time, source bytes, image dimensions, OCR region counts,
redaction counts, logs, spans, and status evidence before enablement.

Collector Performance Evidence: `go test ./internal/collector/document/ocr
./internal/collector/preflight/picture -count=1` proves OCR fact construction is
bounded by image preflight limits, synthetic OCR regions, and section text
bounds without adding discovery, queue, graph, or runtime work.

Collector Observability Evidence: this package emits no facts outside the
returned envelopes and has no runtime side effects. Future runtime wiring must
add collector signals for attempted OCR extraction, warning classes, elapsed
time, bytes inspected, image dimensions, frame counts, OCR region counts,
redaction counts, and resource outcomes before enabling image ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service,
ServiceMonitor, collector binary, runtime flag, environment-variable path, or
hosted collector route changes in this slice. OCR remains callable only through
an injected engine in tests or future reviewed code.

No-Observability-Change: this package adds no worker, queue consumer, database
query, graph write, HTTP handler, MCP tool, runtime flag, hosted collector path,
metric, span, log, or status row. Existing collector, fact-store, HTTP, and MCP
readback surfaces already carry the emitted documentation facts.

## Gotchas / invariants

- WebP remains unsupported unless a decoder dependency review lands first.
- Multi-frame GIF input is OCRed as frame zero only and keeps the
  `partial_extraction` warning.
- Source identity fields are redacted when they contain scheme-bearing URIs,
  private URLs, hostnames, or embedded local paths. Keep raw locations out of
  document IDs, external IDs, canonical URIs, source URIs, and source record
  IDs.
- Sensitive-looking OCR text is not persisted as content. The section keeps
  redaction metadata instead. Its `text_hash` and `excerpt_hash` are hashes of
  the `[redacted]` placeholder, because an unsalted SHA-256 of a short secret
  confirms a guess at it. `section_id` and `ordinal_path` keep the stable IDs of
  redacted sections distinct.
- A value that follows a sensitive label is withheld too, even when its own
  text looks harmless. See "Label and value redaction".
- OCR text remains document evidence. This package must not emit entity
  mentions, claim candidates, graph relationships, ownership truth, deployment
  truth, incident truth, or service truth.
- A caller must provide an OCR engine; there is no default engine or hosted
  provider fallback.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `go/internal/collector/preflight/picture/README.md`
- `docs/public/reference/local-testing.md`
