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

Any region with a label from `labelFamilies` in `label_markers.go` opens a value
window, not only one shaped like a label. The words around a marker are the
least reliable part of OCR text, and each shape rule tried here left a hole:
`(Password)`, `Password required`, `Password: I`. The STT twin in
`document/media` uses the same rule (#9786) with its own, shorter list. An
email, URL, path, or ticket redacts its own region. If its text has no marker
word, it opens no window: it is a value, not a word that introduces one. A
pattern region that also holds a marker word, such as
`password alice@example.com`, opens a window like any other marker region. A
region with a label also holds its own value when they share a line, so
`API Key: value` is one redacted region (#9793).

What counts as a label (#9793). The text is split into words at any character
that is not a letter or digit, and again at a lower to upper case change, so
`API Key`, `API-Key`, `API_KEY`, and `apiKey` are one label. Matching ignores
case. A digit or symbol that OCR reads for a letter folds to that letter before
matching: `0` to o, `3` to e, `4` and `@` to a, `5` and `$` to s, and `1`, `l`,
and `i` are one letter, so `Passw0rd`, `P@ssword`, `Pa55word`, `S3cret`, and
`Credentia1s` match. A symbol at the edge of a word (`$PIN`, `@token`) is a
separator, not a letter. Each family picks the boundary its word can bear:

| Family | Words | Matches |
|---|---|---|
| Inside any run of letters and digits | `password`, `passwd`, `passphrase`, `passcode`, `credential`, `apikey`, `privatekey` | `MyPassword`, `passwords`, `Credentials` |
| Whole word, plural, or end of a compound | `secret`, `token` | `Secrets`, `client_secret`, `accessToken`; not `secretary`, `tokenize` |
| Whole word or plural | `pin`, `pwd` | `PIN:`, `PINs`; not `spin`, `pinned`, `Pinterest` |
| Two adjacent words | `api`, `private`, `access` then `key(s)`; `pass` then `word`, `phrase`, or `code` | `API Key`, `MYAPI_KEY`, `private-key`, `Pass phrase`; not `public key`, `API gateway`, `Call the API, key facts` |

Only space, hyphen, underscore, and a case change may sit between the two words
of a phrase. A comma or any other mark ends it. Source names (document IDs,
source URIs, record IDs) keep the narrow `identityMarkers` list, so a file
called `token-rotation.png` keeps its name.

Why folding and not edit distance. Edit distance one cannot be applied to the
short words: `taken` is one edit from `token`, and `pan`, `pen`, `pit`, `pie`,
and `spin` are one edit from `pin`. A bound that excludes them also excludes
every misread of them, and a longer word such as `password` gains little over
the fold, because the single-character misreads seen in practice are digits and
symbols for letters. Folding is exact, deterministic, one table lookup per
rune, and can only add a match when a word is a marker once folded. On the repo
docs corpus below it adds 5 lines in about 261,800. Letter-for-letter misreads, such
as `rn` for `m` or `vv` for `w`, are not caught. The common ones are the digit
and symbol swaps above.

A region is a value when either holds:

- Window: it is one of the 2 regions after a marker region in engine order
  (`labelValueWindow`). One covers a value directly below its label, and the
  second covers a caption or other line between the two. Each marker region adds
  a full window once, so stacked labels each cover their own value. A region
  with no letter or digit does not use up the window, because box edges reach
  OCR as `|` or `_`. That region is still withheld, since a password can be all
  symbols. A blank region never becomes a section.
- Row: it starts past the marker region's right edge and overlaps that region's
  row by at least half the shorter height (`minLabelRowOverlap`). Tesseract
  reads columns as separate blocks, which can put a value many regions after its
  label. The order of the two columns does not matter. A region with no height
  or width, or with a NaN or infinite bound, has no row, so it gets the window
  only. A region that starts exactly at the marker's right edge, or overlaps
  exactly half the shorter height, counts as on the row.

`Extract` never sorts. `EngineResult.Regions` must be in reading order, because
layout order is more accurate than a position sort on a multi-column page.

The tradeoff is deliberate. Up to 2 ordinary lines after any marker region are
hidden, including after a line that already holds its value, such as
`password: hunter2`, and after prose that mentions a password. That costs a
little documentation evidence. A leaked secret costs more, and ordinary text
that follows no marker region is untouched. Windows add but stay bounded: k marker
regions in a row hide at most k+1 regions with a letter or digit after them.
Symbol-only regions between them are also withheld without using up the window,
so the number of regions withheld can exceed k+1.

Limits:

- Marker words decide everything, so a label the word list misses opens no
  window. A label the folding does not reach, such as a letter-for-letter
  misread (`Passvvord`), opens none. In a probe of degraded renders, `Password`
  came back verbatim or was lost whole, as in `Confirm passwort`. The list has
  no `passwort`, `passkey`, or `ssh key`.
- A label glued to digits, such as `token2`, is not a whole word, and a fused
  lowercase compound such as `clientsecret` matches only through its suffix.
  `pin` and `token` in plain prose are markers like `password` in prose, so
  "Pin the base image" hides the two lines after it (#9793).
- A value more than 2 regions after its marker region, on another row, is not
  caught.
- A value above its label, or left of its label on the same row, is not withheld.
- A region opens one window however many marker words it holds, so
  `password: api_key:` followed by three values leaves the third in clear.
- A table column is not covered. A header row such as `Service Password Owner`
  opens a window over the next rows only, and each cell further down stays in
  clear. That needs a column rule, not this one.

Proof: `TestContainsMarkerWordMatchesEveryLabelForm`,
`TestContainsMarkerWordLeavesOrdinaryTextAlone`,
`TestContainsMarkerWordBoundaries`,
`TestExtractRedactsTheValueAfterEveryLabelFamilyStacked`,
`TestExtractRedactsTheValueAfterEveryLabelFamilyInAColumn`,
`TestExtractRedactsALabelWithItsValueOnOneLine`,
`TestExtractKeepsOrdinaryTextThatHoldsNoLabel`,
`TestSourceIdentityKeepsTheNarrowMarkerList`,
`TestExtractRedactsValuesThatFollowALabelInReadingOrder`,
`TestExtractRedactsValueInTheColumnBesideALabel`,
`TestExtractFollowsEngineRegionOrder`,
`TestExtractRowClauseHandlesInvalidAndTiedGeometry`,
`TestExtractLabelValueRedactionStaysTruthful`, and the `form-stacked.png` and
`form-columns.png` fixtures in `TestSandboxRealTesseract`.

Performance Evidence: `BenchmarkBuildSectionsNoLabels` and
`BenchmarkBuildSectionsLabelHeavy` on 500 regions, the tesseract default cap,
with before and after test binaries run in turn at `-benchtime=2000x`, six runs
each. Medians on a shared host: ordinary text went from 3.06 ms/op to 3.14 ms/op
(13,405 to 13,906 allocs/op, one lowercase per region). The worst case for the
row clause, 250 regions followed by 250 labels, went from 1.88 ms/op to 2.06
ms/op. The row clause is regions times marker regions. The default cap of 500
bounds it, but `Config.MaxRegions` has no upper limit, so a caller that raises
the cap raises this cost. Either figure is small next to about 200 ms for one
tesseract run.

No-Regression Evidence: `go test -race ./internal/collector/document/ocr/...
-count=1` passes, including the pre-existing extraction, skip, GIF, redaction,
and local-engine PII suites.

Marker-list Performance Evidence (#9793): the same two benchmarks on 500 regions,
before (origin/main) and after test binaries run in turn at `-benchtime=2000x`,
seven runs each, medians, on a shared 16-core host at load average 44, so the
spread is wide (the best runs are 3.15 and 3.75 ms/op for ordinary text).
Ordinary text went from 3.59 ms/op to 4.12 ms/op (13,907 to 12,907 allocs/op),
and the label-heavy case from 2.22 ms/op to 2.86 ms/op (13,414 to 12,665
allocs/op). The scan is one pass per region, twice per region (once for the
value window and once for the region's own text), and keeps its buffers on the
stack, so it allocates less than the lowercase copy it replaces. The added
0.5 to 0.6 ms per 500 regions is about 0.3 percent of one tesseract run.

Marker-list False-positive Evidence (#9793): the rule is a superset of the old
one on every corpus below, and costs more only where the text holds a new word.
The unit corpus `ordinaryLines` (43 lines, each holding a marker word as a
substring or one edit away) flags none. The sandbox fixtures hold 15 distinct
texts: the old rule flagged 2 (both true labels), and the new rule flagged 4
before two fixtures were reworded, adding the value `Hunter2CanarySecret` and
the line `Credentials for the staging cluster live`. Both fixtures now avoid a
marker word and the count is back to 2 of 15. On a prose corpus of about 261,800
non-blank lines of this repo's markdown under `docs/public` and
`docs/internal`, which is security-dense and so an upper bound, the old rule
flagged 470 lines (0.18 percent) and the new rule 6,674 (2.55 percent), with no
line the old rule flagged missed. By family, on lines the old rule did not flag:
`token` 2,203, `pin` 1,425, `secret` 1,383, `credential` 1,302, `api key` 57,
`pwd` 43, `private key` 36, `access key` 32, `apikey` 7. Each costs a bounded
window of 2 regions, as `password` always did.

Marker-list Mutation Evidence (#9793): 34 planted mutants each turn at least one
test red: each family word removed (14), the `secret` and `token` boundary and
the `pin` and `pwd` boundary loosened to a substring, plural removed from both
whole-word rules, folding removed whole and each of 0, 5 and `$`, `@`, 3, and
`1`/`l` removed, camel splitting and its plural guard removed, the phrase gap
loosened and its check removed, the first-word suffix removed, a word-edge
symbol read as a letter, in-word symbols disabled, region text matched with the
source-name list, the window opened by the source-name list, and the source-name
list widened to the label matcher.

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
