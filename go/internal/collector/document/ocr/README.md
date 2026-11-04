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
`document/media` uses the same rule (#9786) with its own list. An
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
separator, not a letter. A symbol inside a word does not start a case split, so
`PRIV@TE KEY` and `ACCE$S KEY` match; the letters after it are also read as a
word of their own, so `DB$PWD` and `Card@PIN` match too. Each family picks the
boundary its word can bear:

| Family | Words | Matches |
|---|---|---|
| Inside any run of letters and digits | `password`, `passwd`, `passphrase`, `passcode`, `pincode`, `credential`, `apikey`, `accesskey`, `privatekey` | `MyPassword`, `passwords`, `Credentials` |
| Whole word, plural, or end of a compound | `secret`, `token` | `Secrets`, `client_secret`, `accessToken`; not `secretary`, `tokenize` |
| Whole word or plural | `pin`, `pwd` | `PIN:`, `PINs`, `pin_code`; not `spin`, `pinned`, `Pinterest`, `500 PLN` |
| Two adjacent words | `api`, `private`, `access` then `key(s)`; `pass` then `word`, `phrase`, or `code` | `API Key`, `MYAPI_KEY`, `private-key`, `Pass phrase`; not `public key`, `API gateway`, `Call the API, key facts` |

Only space, hyphen, underscore, and a case change may sit between the two words
of a phrase. A comma or any other mark ends it. Digits after a marker word do
not hide it (`Token2:`, `PIN2`, `api key1`, `API_KEY2`), and a case change
inside a word does not split it away from its marker (`ToKen:`, `SeCret:`,
`PiN:`). Fullwidth ASCII (letters, digits, `＿`, `－`, `＠`, `＄`) is read as its
ASCII twin, so `ＡＰＩ＿ＫＥＹ` and `Ｐ＠ｓｓｗｏｒｄ` match. A digit at the end of a
word is checked both ways, without it (`token2`) and as the letter the fold
reads (`Pass cod3`). A whole word that holds a capital L never matches `pin`,
because the fold reads l as i and `PLN` is a currency; lowercase `pln` and `PlN`
still match as a misread `PIN`.

The rule is a superset of the list it replaced. Every source-name marker
(`identityMarkers`: `password`, `api_key`, `private_key`, and the three synthetic
canary words) is also checked first as a raw substring in any case, so
`API_KEY2`, `PRIVATE_KEY1`, `API_KEYID`, `private_keyfile`, and `API_kEY` stay
redacted even where the word split would have broken the phrase.
`TestContainsMarkerWordIsASupersetOfTheSourceNameList` pins it with a prefix and
a suffix on every marker. Source names themselves (document IDs, source URIs,
record IDs) keep the narrow `identityMarkers` list only, so a file called
`token-rotation.png` keeps its name.

The STT twin in `document/media` (#9786, #9788) has its own rule, kept as a
second tested copy and not a shared package. The folds differ by input: OCR
reads digits and symbols in place of letters and camel case, and speech engines
write neither, so STT folds NFKD, combining marks, format characters, a
bounded Cyrillic and Greek homoglyph table, and separators (space, `-`, `_`,
`.`, `,`) to one space instead. The boundaries differ on purpose too. STT
matches `pwd` as a substring and `pins` not at all; OCR matches `pwd` and `pins`
as whole words. Both keep the same six-entry source-name list, which a change
must edit in both packages together. The decision and its reasons are in
`docs/internal/evidence/9793-fold-vs-edit-distance-arbiter-ruling.md`. A probe
of `containsMarkerWord` and `containsSensitiveKeyword` on both merged trees
gave these results (true means the text is a label):

| Text | OCR | STT | Why they differ |
|---|---|---|---|
| `Password:`, `Secret:`, `Token:`, `PIN:`, `PWD:`, `API Key: value`, `Access Key:`, `Credentials`, `accesskey`, `Private_Key`, `Pincode` | true | true | Shared vocabulary. |
| `Passw0rd:`, `P@ssword`, `Pa55word`, `Pass cod3`, `S3cret` | true | false | OCR folds digits and symbols read for letters. Speech never writes them. |
| `PINs`, `pins`, `Pln`, `clientSecret` | true | false | OCR matches the plural and a compound ending, and reads `l` as `i`. STT matches `pin` and `secret` as whole words only. |
| `P.I.N.`, `p i n`, `p w d`, `a p i key`, `A.P.I. key`, `aPI Key` | false | true | STT folds separators, so spelled-out letters join. OCR breaks a word at `.`. |
| `раssword` (Cyrillic), `pássword` | false | true | STT folds homoglyphs and accents. OCR folds only fullwidth ASCII, and a Latin-trained tesseract emits Latin. |
| `Ｐａｓｓｗｏｒｄ` (fullwidth) | true | true | Both fold width. |
| `500 PLN` | false | false | A capital L is not a misread `i` for OCR. |
| `compass word`, `access keyboard`, `secretary`, `tokenize`, `spin`, `pinned`, `secretkey`, `tokenid`, `Pasword`, `PW:`, `Pass:` | false | false | Ordinary words and unmatched shapes in both. |

Why folding and not edit distance. Edit distance one cannot be applied to the
short words: `taken` is one edit from `token`, and `pan`, `pen`, `pit`, `pie`,
and `spin` are one edit from `pin`. A bound that excludes them also excludes
every misread of them, and a longer word such as `password` gains little over
the fold, because the single-character misreads seen in practice are digits and
symbols for letters. Folding is exact, deterministic, one table lookup per
rune, and can only add a match when a word is a marker once folded. On the repo
docs corpus below it adds 5 lines in about 262,000. Letter-for-letter misreads,
such as `rn` for `m` or `vv` for `w`, are not caught. The common ones are the
digit and symbol swaps above.

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

- Column: a region below a table header whose x-position maps it to a marker
  column. A header is a marker region with a same-row neighbor to its left,
  the shape of a header cell past the first column. A label that opens its
  row, with its value to the right as forms lay out, keeps the two-region
  window below it and opens no column. A cell joins when its top edge is at
  or below the header's bottom edge and its horizontal overlap with the
  header is at least half the shorter width. The pass runs after
  `extendSecretRuns` and only turns kept regions to withheld, so a column
  cell never seeds or extends a run and the SECRET_RUNS.md counts are
  unchanged.

A secret that runs past the window (#9799). A PEM key body is 12 to 50 lines and
a token can wrap across lines, and tesseract returns one region per line, so the
window of 2 left the rest in clear. `extendSecretRuns` in `secret_runs.go` runs
after the window and row passes and only turns a region from kept to withheld,
through four clauses: chain, tail, symbol, and seed. The rule, its thresholds,
its limits, and its measured cost are in [SECRET_RUNS.md](SECRET_RUNS.md). The
decision is `docs/internal/evidence/9799-multiline-arbiter-ruling.md`.

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
- Misses found by probing, none matched: `Pasword` (a dropped letter), `PW:`,
  `Pass:`, `A.P.I. key` (punctuation inside a word), Cyrillic `раssword` and
  other homoglyphs outside fullwidth ASCII. Only digit, symbol, case, and
  fullwidth variants are handled.
- Fused or odd shapes that are not matched: `secretkey`, `tokenid`, `aPI Key`
  (a case change inside `api`, a space before `key`), and `$ecret` (a symbol at
  the start of a word is a separator, not an `s`).
- A digit does not start a new word in a camel case name, so `Token2Value`,
  `Secret2Value`, and `API2Key` are not matched. The STT rule matches the
  first two. `PaSS CoDe` is not matched either, for another reason: a case
  change inside each word splits it, so neither half is the whole word `pass`
  or `code`. The STT rule matches it.
- A marker followed directly by an in-word symbol is not matched: `PIN$Value`,
  `PWD$S`, `Token@host`, `API Key$Value`, and `x$Pass cod3`. The word after an
  in-word symbol is also read on its own, so a few ordinary strings open a
  window: `alice@pin.example.com`, `bob@pwd.example.org`, and `foo$pin`. The
  email is redacted anyway, and the window costs the two regions after it.
- A fused lowercase compound such as `clientsecret` matches only through its
  ending. `pin` and `token` in plain prose are markers like `password` in
  prose, so "Pin the base image" hides the two lines after it (#9793).
- Limits of the run rule (a wrapped line under 16 characters, a cropped body
  with a stray space in every line, a cropped letters-only or lowercase-only
  token, the seed cost class, a fence inside a run) are in
  [SECRET_RUNS.md](SECRET_RUNS.md#limits).
- A value above its label, or left of its label on the same row, is not withheld.
- A region opens one window however many marker words it holds, so
  `password: api_key:` followed by three values leaves the third in clear.
- A marker in the first column opens no column: a header row whose marker
  cell stands leftmost keeps the window over the next rows only, and each
  cell further down stays in clear. A header row that tesseract reads as one
  wide line, such as a single `Service Password Owner` region, likewise
  opens no column, because there is no cell to map by x-position. Both stay
  residuals until the OCR path's image classes show which table shape needs
  them.

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
`TestExtractLabelValueRedactionStaysTruthful`,
`TestExtractWithholdsASecretThatSpansManyLines`,
`TestExtractContinuesASecretRunPastTheWindow`,
`TestExtractContinuesAColumnValueThatWrapsBelowItsRow`,
`TestExtractRedactsCellsBelowATableHeader`,
`TestExtractTableColumnLimits`, `TestExtractTableColumnTies`,
`TestExtractTableColumnNeedsALeftNeighbor`,
`TestExtractTableColumnFeedsNoSecretRun`,
`TestSecretTokenShapes`, `TestExtendSecretRunsIsLinear`, and the
`form-stacked.png`, `form-columns.png`, and `key-body.png` fixtures in
`TestSandboxRealTesseract`.

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


Performance Evidence (#9799) and No-Regression Evidence (#9799) are in
[SECRET_RUNS.md](SECRET_RUNS.md#proof-and-cost). No-Observability-Change (#9799):
the run scan adds no metric, span, or log, and a region it withholds is a
redacted section that `eshu_dp_ocr_redacted_regions_total` already counts.

Marker-list Performance Evidence (#9793): the same two benchmarks on 500 regions,
before (origin/main) and after test binaries run in turn at `-benchtime=2000x`,
seven runs each, medians, on a shared 16-core host at load average 9.6.
Ordinary text went from 3.16 ms/op to 4.26 ms/op (13,906 to 12,906 allocs/op),
and the label-heavy case from 2.14 ms/op to 2.58 ms/op (13,415 to 12,663
allocs/op). The scan makes a fixed number of passes per region (one for the
source-name substrings, one for the words), twice per region (once for the value
window and once for the region's own text), and keeps its buffers on the stack,
so it allocates less than the lowercase copy it replaces. The added 0.4 to 1.1
ms per 500 regions is about 0.5 percent of one tesseract run.
`TestContainsMarkerWordIsLinear` pins the time on 1 MiB of symbols, camel case,
digits, and glued words. A run of `@` or `$` outside a word was once measured
again from every position; review measured 1.67 s for one 64 KiB region
through `Extract` against 5.4 ms on the old rule, and 17.6 s for 256 KiB. The
run is now measured once.

No-Observability-Change (#9793): the label matcher adds no metric, span, or log.
A region it flags is a redacted section, so `eshu_dp_ocr_redacted_regions_total`,
the `ocr.redacted_region_count` span attribute, and the `redacted_region_count`
log field count it with no change.

Marker-list False-positive Evidence (#9793): the rule is a superset of the old
one on every corpus below, and costs more only where the text holds a new word.
The unit corpus `ordinaryLines` (53 lines, each holding a marker word as a
substring or one edit away) flags none. The sandbox fixtures hold 15 distinct
texts: the old rule flagged 2 (both true labels), and the new rule flagged 4
before two fixtures were reworded, adding the value `Hunter2CanarySecret` and
the line `Credentials for the staging cluster live`. Both fixtures now avoid a
marker word and the count is back to 2 of 15. On a prose corpus of about 262,000
non-blank lines of this repo's markdown under `docs/public` and
`docs/internal`, which is security-dense and so an upper bound, the old rule
flagged 470 lines (0.18 percent) and the new rule about 6,700 (2.55 to 2.57
percent, depending on the corpus snapshot), with no line the old rule flagged
missed. By family, on lines the old rule did not flag:
`token` 2,203, `pin` 1,425, `secret` 1,383, `credential` about 1,300, `api key` 57,
`pwd` 43, `private key` 36, `access key` 32, `apikey` 7. Each costs a bounded
window of 2 regions, as `password` always did.

Marker-list Mutation Evidence (#9793): 66 planted mutants each turn at least one
test red, run on a `git archive` copy. They are each family word removed (15),
the `secret` and `token` boundary and the `pin` and `pwd` boundary loosened to a
substring, plural removed from both whole-word rules, folding removed whole and
each of 0, 5 and `$`, `@`, 3, 4, and `1`/`l` removed, camel splitting and its
plural guard removed, the phrase gap loosened and its check removed, the
first-word suffix removed, in-word symbols disabled, the symbol-run early exit
removed (the linearity test fails after minutes), the source-name substring
check and its non-ASCII path removed, trailing digits left on a word, the
run-level word check removed, the whole-word check of a digit-ended word
removed, the capital L rule removed and widened to any l, fullwidth folding
removed at the decode site and in a symbol run, `pincode` removed, the word
after an in-word symbol dropped and its use in a phrase dropped, each part of
that word's state broken (trailing digits, the capital L flag, the reset at a
later symbol and at a word end, the word before it and its link guard), a symbol
that keeps the folded rune, match-anywhere turned into a prefix match, each plural
second word removed, the six-entry source-name list shortened in either
package, region text matched with the source-name list, the window opened by
the source-name list, and the source-name list widened to the label matcher.

Column-rule False-positive Evidence: the rule is inert on prose-shaped
layouts. On the docs corpus (`git ls-files 'docs/public/*.md'
'docs/internal/*.md'`, 269,540 non-blank lines, each one region in reading
order in a single column), the new pass withholds 0 lines past the old rule:
no two regions share a row, so no marker qualifies as a header. The cost is
confined to multi-region rows, where a marker past the first column withholds
the cells below it in its band; a two-column prose page whose right column
holds a marker line pays the rest of that column. The pass is monotonic, so
the SECRET_RUNS.md corpus count (180 added lines) is unchanged.

Column-rule Performance Evidence: the same two benchmarks on 500 regions,
before and after, run in turn at `-benchtime=2000x`, six runs each, medians,
on a shared host at load average 3.5 to 8.8. Ordinary marker-free text went
from 4.20 ms/op to 4.21 ms/op (12,906 allocs/op both, so no new allocation on
the prose shape). The label-heavy case, 250 regions followed by 250 labels,
went from 2.53 ms/op to 2.84 ms/op (12,663 to 12,669 allocs/op, the marker
and header slices). The added pass reuses the window pass's marker scan and
does bounds arithmetic only: regions times marker regions, the same order as
the row clause. Either figure is small next to about 200 ms for one
tesseract run.

No-Observability-Change: the rule adds no metric, span, or log. A withheld value
is a redacted section, so `eshu_dp_ocr_redacted_regions_total`, the
`ocr.redacted_region_count` span attribute, and the `redacted_region_count` log
field count it with no change. `TestExtractRedactsValueOnLineBelowLabel` asserts
the count and that no canary reaches logs, spans, or envelopes.

No-Observability-Change (column rule): a cell the column withholds is a
redacted section, so the existing redaction counters cover it with no new
signal.

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

- WebP passes preflight through the bounded RIFF header parser in
  `preflight/picture` (no decoder dependency, #9780), so `ocr.Extract` reaches
  the engine for WebP like any other admitted format.
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

- `SECRET_RUNS.md` (the secret run rule, its limits, and its cost, #9799)
- `docs/internal/evidence/9799-multiline-arbiter-ruling.md`
- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `go/internal/collector/preflight/picture/README.md`
- `docs/public/reference/local-testing.md`
