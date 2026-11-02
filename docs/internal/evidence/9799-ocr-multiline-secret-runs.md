# Evidence: #9799, OCR redaction of a secret that spans more lines than the window

STATUS: ruled. The arbiter accepted the rule and its four thresholds with no
change, and recorded the remaining leak classes as documented limits. The ruling
is in [9799-multiline-arbiter-ruling.md](9799-multiline-arbiter-ruling.md). The
section "Open for the arbiter" below is kept as the question the ruling answers.

## Decision and budget

Decision: which multi-line layouts leak today, and which rule closes them at a
bounded false-positive cost. Threshold: no line of any probed layout persists in
clear, the added cost on the docs corpus stays near the 0.2 percent of lines the
#9819 rule accepted for its own windows, and the scan stays linear with no edit
distance (ruling 9793). Budget: 45 minutes from 07:45Z, one theory, two attempts.
The rule changed once after the first real tesseract run (below); the deadline
held.

## Probe: what leaked before

`TestExtractWithholdsASecretThatSpansManyLines` through `Extract` on
origin/main `0e4aafe77e`, the window and row rules as merged:

| Layout | Secret lines persisted in clear |
|---|---|
| RSA key, armor and 26 body lines | 24 of 26 |
| OpenSSH key, 12 body lines | 10 of 12 |
| Encrypted key, 2 headers and 18 body lines | 18 of 18 |
| Token wrapped over 3 lines under `API Token:` | 1 of 3 |
| Token over 6 lines, caption between | 5 of 6 |
| Bearer token over 5 lines | 3 of 5 |
| Key body with no armor line | 12 of 12 |
| Wrapped value in the column beside `Token:` | 3 of 5 |
| Body as tesseract read the sandbox fixture | 9 of 9 |

The issue's premise that the armor line misses the marker list is out of date:
since #9819 `PRIVATE KEY` matches the `private` then `key` phrase, so the armor
line opens the 2-region window. The gap is the body past it. With the fix, every
row persists 0 secret lines and keeps the ordinary line after the run.

## What changed after the first real tesseract run

The first rule required lines with no space. The sandbox proof on arm64 showed
tesseract 5.5.0 drops the `BEGIN` armor line of the key fixture and puts one
space inside two of the nine body lines. A strict rule left both lines in clear.
The final rule lets a spaced line chain only after a token line, and walks back
from a seed over the token lines before it. The proof also caught a
false positive in the existing `form-columns.png` fixture, where the single word
`platform` under a withheld value was hidden by the first tail rule. The tail now
needs two token lines before it.

## False-positive cost

The docs corpus is `git ls-files 'docs/public/*.md' 'docs/internal/*.md'`
(1,740 files, 263,378 non-blank lines), each line one region in reading order.
The corpus is security-dense and full of commands, so it is an upper bound.
Regions withheld by label windows, markers, and patterns:

| Rule | Withheld | Added |
|---|---|---|
| origin/main | 19,803 | |
| Strict (no space) rule | 19,850 | 47 |
| Stray space allowed everywhere, symbols after any withheld region | 20,380 | 577 |
| Final: stray space only inside a token run, symbols only inside a run | 19,983 | 180 |

The 180 added lines split three ways (arbiter re-measurement): 39 lines with no
space (paths with line numbers, file names in backticks), 102 lines with a stray
space that chain or fall in the tail slot (`ENV=value \` continuation lines,
`-> path` rename tables, `ok  pkg 28.907s` results), and 39 symbol-only lines, of
which 36 are code fences. One line in the corpus is a seed
(`ESHU_DR_COMPOSE_PROJECT=eshu-pr6634-final-rebuild`).

Rejected variants, measured on the same corpus:

| Variant | Withheld | Added over final | Why rejected |
|---|---|---|---|
| Seed that tolerates one stray space (dense by 16, 40 non-space characters, same classes and charset) | 20,097 | 114 | 60 corpus lines become seeds: `go test ... -run TestX -count=1` commands, `export ESHU_...=...` lines, `POST /api/v0/...` routes, `CREATE INDEX ...` statements. |
| Seed that accepts lowercase only (to catch a cropped hex token) | not totalled | | It hits 30 bare 40+ hex lines and chains into the 676 corpus lines that carry a 64-hex digest. |

## Cost in time

`BenchmarkBuildSectionsNoLabels` and `BenchmarkBuildSectionsLabelHeavy`, before
(origin/main) and after test binaries run in turn at `-benchtime=2000x`, seven
runs each, medians, on a shared host at load average 22 to 24:

| Benchmark (500 regions) | Before ms/op | After ms/op | Allocs/op |
|---|---|---|---|
| NoLabels | 5.55 | 5.52 | 12,907 both |
| LabelHeavy | 3.68 | 3.79 | 12,665 both |
| KeyBody, one label and 499 body lines | 9.77 | 3.54 | 12,908 before, 12,905 after |
| TokenLinesNoLabel, 500 seed lines | 9.83 | 3.55 | 12,908 before, 12,904 after |

The key-body cases are faster because a withheld section hashes the short
placeholder and not a 64 character line. `TestExtendSecretRunsIsLinear` pins
200,000 token lines, 200,000 seed lines, and one 1 MiB token under a loose
bound.

## Mutants

About 50 planted mutants on a `git archive` copy, each run against the OCR
package tests: the length, digit and symbol, space share, and tail thresholds;
each rule removed or loosened; the seed's length, class, and charset checks;
blank and symbol handling; the or-merge into `values`; the call itself and its
position before the row clause. Two survived the first set and got rows
(`16` against `15` for the space share, and counting spaces as characters), and
the order mutant got a column row. The review pass added mutants and one more row.
A walk back that stops at a blank region survived until a row put a blank region
between a run token and a seed; that row now kills it. Tesseract's TSV reader
never emits a blank region, so the guard has no production path.

Equivalent survivors, each harmless: dropping the `!values[j]` stop in the walk
back (a walk back never reaches a withheld region another way); overwriting
`values[i]` instead of or-merging it (`withheld` starts as `values[i]` and only
rises); a tail that keeps the token count; testing `' '` instead of any
whitespace in `hasSpace`; and dropping the trim. The last two are equivalent
under tesseract, which trims words and joins them with one space (`tsv.go`).

## Corpus method

The false-positive count above is reproducible without the harness being
committed. Take `git ls-files 'docs/public/*.md' 'docs/internal/*.md'` at the
tree under test. For each file build one page whose regions are its non-blank
lines in order, one region per line. Run each page through `Extract` with a
stub engine, and count the sections persisted as the `[redacted]` placeholder.
Run it on origin/main and on the branch, and report the difference. Added
withheld lines are the ones the branch withholds and origin/main does not. The
run reads the marker, window, pattern, and run rules together, as production
does. A change to a threshold in `secret_runs.go` needs this count rerun and a
row in `TestSecretTokenShapes`.

## Real tesseract

`bash scripts/prove-ocr-sandbox.sh --platform linux/arm64`, new fixture
`key-body.png` (a PEM key, nine body lines): 14 sections, 12 redacted,
`body_regions=9 body_regions_in_clear=0`, `canary_leak` false for every fixture,
`form-columns.png` back at 4 redacted.

## Open for the arbiter

- The thresholds are heuristics with a measured cost, not derived values: 16
  characters to join a run, 40 and three classes to start one, two tokens before
  the tail, one stray space per 16 characters. A wrapped line under 16 characters
  or of letters only still leaks, as does a key whose lines hold several
  misread spaces.
- The unanchored seed rule withholds a key body that has no label at all. It is
  the only rule that acts with nothing before it, and the corpus has one such
  line in 263,378. The alternative is to drop it and accept that a cropped key
  body leaks.
- The STT twin is not changed: a transcript has no lines.
