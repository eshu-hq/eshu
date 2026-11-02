# OCR Secret Runs

This page holds the rule that withholds a secret which runs past the label
window (#9799), its limits, and its measured cost. The label and value rules it
builds on are in [README.md](README.md#label-and-value-redaction). The decision
is in `docs/internal/evidence/9799-multiline-arbiter-ruling.md`, and the
measurements are in `docs/internal/evidence/9799-ocr-multiline-secret-runs.md`.
The code is `secret_runs.go`.

A secret that runs past the window (#9799). A PEM key body is 12 to 50 lines and
a token can wrap across lines. Tesseract returns one region per line, so the
window of 2 left the rest in clear. `extendSecretRuns` in `secret_runs.go` runs
after the window and row passes, in one pass over engine order, and only turns a
region from kept to withheld. A region joins a run when it is not withheld yet
and one of these holds:

- Chain: the region before it was withheld and it is a run token: at least 16
  characters apart from whitespace, with a digit or one of `+/=`. A token with
  no space always chains. A token with a stray space chains only when the region
  before it was a run token too, because a bullet of inline code under a label is
  not a line of a key. A stray space is one per 16 characters (`denseSpaceShare`).
- Tail: the two regions before it were withheld run tokens, and it has no more
  than a stray space. A body ends on a short line (`Qk9x==`). It holds for one
  region, so one withheld value does not hide the single word under it.
- Symbol: it has no letter or digit and the region before it was a withheld run
  token. Box edges reach OCR as `|`.
- Seed: it is 40 or more characters of letters, digits, and `+/=_.-` with no
  space, with a capital, a lowercase letter, and a digit. A key body cropped
  from its armor line has no label, and tesseract dropped the `BEGIN` line of the
  sandbox fixture, so a seed starts a run alone. The run tokens right before the
  seed join it.

Prose has a space every five or six characters, so the first line of ordinary
text ends a run. The scan reads each region a fixed number of times and the
walk back before a seed stops at any region that is withheld or is not a run
token, so it is linear. `TestExtendSecretRunsIsLinear` pins 200,000 token lines
and 200,000 seed lines.

What tesseract returned for the `key-body.png` fixture (arm64, 5.5.0): the
`BEGIN` armor line is absent, the nine body lines come back as nine regions,
two of them with one space inside (`zwZWd fRLg...`), and `O`, `0`, `l`, `1`, and
`@` are misread in several. The `END` armor line reads `---- END PRIVATE KEY-----`
and is a label like any other. `TestExtractWithholdsASecretThatSpansManyLines`
has that text as a row, and `liveKeyBody` compares what tesseract read, not what
was drawn, so a misread cannot hide a leak.

## Limits

- A value more than 2 regions after its marker region, on another row, is caught
  only when it is a run of token lines (above). A value of ordinary words is not.
- A run token needs 16 characters and a digit or one of `+/=`. A wrapped line of
  fewer characters, or of letters only, ends the run, and the rest of the body
  stays in clear until a seed starts a new run. A line the OCR breaks into
  words by a space every few characters ends it too.
- A seed has no space at all, so a lone first line of a key with a stray space is
  withheld only when a later seed walks back to it. A key or token cropped from
  its label with lines under 40 characters opens no run. The STT twin has no
  lines and is unchanged.
- The short last line of a body is withheld only after two run tokens, so a
  column value of one long line followed by a short last line leaves that line in
  clear. It also hides one single-word line after a run, such as `platform`.
- A symbol-only region at the start of a run, before its first token, is not
  withheld. A PEM header past the 2 window regions (`Proc-Type` and `DEK-Info`
  fill them) ends the run before the body, and the body is withheld again from its
  first seed.
- After a withheld region, a line of 16 or more characters with a digit, such
  as a file path with a line number or an `ENV=value \` continuation, joins the
  run. On the docs corpus the rule adds 180 withheld lines in 263,378: 39 with no
  space, 102 with a stray space that chain or fall in the tail slot, and 39 with
  only symbols (36 are code fences).
- A body cropped from its label whose every line holds a stray space, or a
  character outside `A-Za-z0-9+/=_.-` such as a misread `@` or a non-ASCII
  letter, leaks whole when no line is a seed, because a seed has no space and
  only that charset, and no label opens a run. A probe of 11 lines of 64
  column base64 under `Deploy notes` leaked all 11 with one such character per
  line. `TestExtractContinuesASecretRunPastTheWindow` pins the `@` case. With a label the
  rule tolerates up to 4 stray spaces in a 64-character line (one per 16
  characters). At 5 in every line, 3 of 5 lines leak. If one line is clean, its
  seed walks back and the rest is withheld.
- A token or body cropped from its label that is letters only, or lowercase
  only such as a hex string, has no seed and opens no run. A 64-hex line after
  a withheld region is still chained. Letting a lowercase string seed was
  measured and rejected: it hits 30 bare hex lines and chains into 676 digest
  lines on the docs corpus.
- Seed cost class: a 40 or more character identifier or assignment with a
  capital, a lowercase letter, and a digit, such as
  `ESHU_DR_COMPOSE_PROJECT=eshu-pr6634-final-rebuild` or a long test name, is
  withheld as a seed, and so is the one short line after it. `TestSecretTokenShapes`
  pins one. The docs corpus holds 1 such line in 263,378.
- A fence or `|` line inside a run is withheld by the symbol clause. Of the 39
  symbol-only lines the rule adds on the docs corpus, 36 are code fences.

## Proof and cost

Performance Evidence (#9799): `BenchmarkBuildSectionsNoLabels`,
`BenchmarkBuildSectionsLabelHeavy`, `BenchmarkBuildSectionsKeyBody`, and
`BenchmarkBuildSectionsTokenLinesNoLabel` on 500 regions, before (origin/main)
and after test binaries run in turn at `-benchtime=2000x`, seven runs each,
medians, on a shared host at load average 22 to 24. Ordinary text went from 5.55
ms/op to 5.52 ms/op and the label-heavy case from 3.68 to 3.79, with the same
allocations (12,907 and 12,665 per op). A key body and 500 seed lines went from
9.77 and 9.83 ms/op to 3.54 and 3.55, because a withheld section hashes the
short placeholder and not the line. The run scan reads each region a fixed
number of times. `TestExtendSecretRunsIsLinear` pins 200,000 token lines,
200,000 seed lines, and one 1 MiB token. The per-measure method, the docs-corpus
false-positive table, and the real-tesseract numbers are in
`docs/internal/evidence/9799-ocr-multiline-secret-runs.md`.

No-Regression Evidence (#9799): `go test -race ./internal/collector/document/ocr/...
-count=1` passes, and `bash scripts/prove-ocr-sandbox.sh --platform linux/arm64`
passes with the new `key-body.png` fixture.

No-Observability-Change (#9799): the run scan adds no metric, span, or log. A
region it withholds is a redacted section, so `eshu_dp_ocr_redacted_regions_total`
and the `ocr.redacted_region_count` span attribute count it with no change.
