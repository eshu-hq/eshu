# #8833 part 1: skeleton redactor and URL userinfo rule

Part of #8807. The design record is
[8813-credential-file-exposure.md](../design/8813-credential-file-exposure.md),
and the issue body of #8833 is the binding acceptance list. This note holds the
measurements for part 1, which adds the allowlist-skeleton redactor and the
userinfo-only rule for URL-valued keys but binds neither to any definition.
Every figure below was produced on this branch.

## What changed

- `parser.RedactorSkeleton` and `ContentStore.URLUserinfoKeys`, validated by
  `NewRegistry`. The URL key list joins the fingerprint token only when it is
  non-empty, so a #8812 marker token renders as before.
- `redactSkeleton` (`go/internal/collector/git/content/skeleton_redactor.go`)
  and `redactURLKey` (`marker_url_userinfo.go`), dispatched by
  `ApplyContentStore`.
- A `redactor` label on `eshu_dp_collector_content_store_files_total`.
- `contentStoreTally.storedBody` and `storedBytes` return before the map lookup
  when the policy map is empty. That covers an all-`store` registry and, on the
  default registry, every repository with no `.npmrc` (see the governed-default
  rows below).

Neither new mode is bound by any definition, so this part changes no stored
body, registry fingerprint, or freshness hint. The one production non-store
binding on this base is `.npmrc` under `metadata` (#8978). It is unchanged. The
only visible change is that its counter series gains `redactor="none"`.

## Performance Evidence

No-Regression Evidence: the base is `origin/main` at `a4dc8cdda8`, in a detached
worktree. The head is this branch. Host: 16 vCPU arm64 shared by several agents
(load average 12 to 25), Go 1.26.6. Command:
`go test -run '^$' -bench <name> -count=6 -benchtime=200ms` per package, means of
6 runs.

| Benchmark | Base | Head |
| --- | --- | --- |
| `BenchmarkRedactMarker/typical` | 7172 ns, 24 allocs | 7309 ns, 24 allocs |
| `BenchmarkRedactMarker/clean_1MiB` | 11.59 ms, 83891 allocs | 11.44 ms, 83891 allocs |
| `BenchmarkRedactMarker/dense_1MiB` | 108.5 ms, 192276 allocs | 108.0 ms, 192280 allocs |
| `BenchmarkContentStoreUngoverned/snapshot_stage_5000_files` | 8.91 ns | 8.89 ns |
| `BenchmarkContentStoreUngoverned/emit_stage_per_file` | 8.30 ns | 1.83 ns |

The marker redactor is unchanged within run-to-run spread. A definition without
URL keys pays one nil check per line.

The emit-stage row was investigated. A first head run read 9.91 ns against
8.26 ns on base, interleaved base and head over 3 rounds of 3 runs, so it was
not host noise. Neither `storedBytes` nor `storedBody` inlines in either tree
(`go build -gcflags=-m`). The theory was that the nil-map lookup copies the zero
`parser.ContentStore`, which grew by one slice header (24 bytes). A
`len(stores) == 0` return before the lookup measured 1.83 ns in the same
interleaved run, which confirms it. #8812's own note recorded 1.82 ns for this
stage, so the 8.30 ns base cost was already on main before this branch. The
guard brings it back.

On the current base (#8978 binds `.npmrc`, so the default registry is governed
and a repository with no `.npmrc` yields an empty, non-nil map) the guard covers
the common case too. Measured at this head against the same tree with the two
guards replaced by `if false`, 3 interleaved rounds of 3 runs each,
`-benchtime=200ms`, same host:

| Benchmark | Without guard | With guard |
| --- | --- | --- |
| `BenchmarkContentStoreGovernedDefault/emit_stage_per_file_empty_map` | 8.04 to 8.08 ns | 2.34 ns |
| `BenchmarkContentStoreUngoverned/emit_stage_per_file` | 9.81 to 10.00 ns | 1.83 ns |

So a repository with no `.npmrc` goes from about 8.1 ns to 2.35 ns per emitted
file. A repository with an `.npmrc` has a non-empty map and takes the lookup as
before.

Performance Evidence: new code, measured on the head only. The skeleton rows are
means of 6 interleaved runs against the previous commit's source, which they
match within noise; the host was busier than for the earlier rows:

| Benchmark | ns/op | MB/s | allocs |
| --- | --- | --- | --- |
| `BenchmarkRedactSkeleton/typical` (8-line tfvars) | 11300 | 12.2 | 28 |
| `BenchmarkRedactSkeleton/clean_1MiB` (allowlisted values) | 52.3 ms | 20.0 | 123400 |
| `BenchmarkRedactSkeleton/dense_1MiB` (a finding every fifth line) | 123 ms | 8.5 | 247640 |
| `BenchmarkRedactMarkerURLKeys` (typical body, URL keys listed) | 50602 | 2.8 | 40 |

The skeleton redactor parses every non-empty line, about one allocation per
line, so a clean body costs several times the marker's prefiltered clean
body. Governed bodies are capped at `MaxGovernedBodyBytes` (4 MiB). The worst
case is therefore about 0.5 s per pass at the dense rate, run twice per
collection. tfvars files are small configuration files: a typical one costs
about 11 µs.

The round-4 lexical stack was measured against `0fd28d6fff` with the same
benchmark file, 6 interleaved runs each (`-benchtime=200ms`, one host, two test
binaries run alternately), ns/op range:

| Benchmark | before (`0fd28d6fff`) | after |
| --- | --- | --- |
| `typical` | 10.78 to 11.53 µs | 10.79 to 11.31 µs |
| `clean_1MiB` | 50.5 to 53.8 ms | 50.0 to 50.2 ms |
| `dense_1MiB` | 118.6 to 120.3 ms | 118.6 to 125.5 ms |
| `nested_interp_1MiB_open` (349,525 `"${`; the sticky path) | 1.31 to 1.45 ms, 1 alloc | 1.31 to 1.37 ms, 1 alloc |
| `heredoc_4MiB` (an unclosed 4 MiB heredoc) | 26.2 to 26.8 ms, 4.09 MB | 27.7 to 29.1 ms, 4.09 MB |

B/op and allocs match on every row. The first three rows and the sticky path
sit inside the run-to-run spread. The heredoc row grows by about 1.5 ms (5 %)
on a 4 MiB body of 131,000 lines: heredoc text is now scanned for `${`, which is
what keeps `${ <<INNER` nested heredocs right (mutation m5), and the scan is two
vectorized byte searches per line plus one `TrimSpace`. A CPU profile of that
row puts the cost in the detector and the byte searches, not in a byte loop.
Neither redactor runs until part 2 binds a definition.

The round-5 bare-`\r` check (J1) adds one vectorized `IndexByte` per heredoc
line that holds no `$` or `%`, and one `case` in the string loop. It was
measured against `070d079446` the same way, 3 rounds of 2 runs each,
interleaved (`-benchtime=200ms`, load average about 5), ns/op range:

| Benchmark | before (`070d079446`) | after |
| --- | --- | --- |
| `typical` | 10.79 to 11.23 µs | 10.87 to 11.16 µs |
| `clean_1MiB` | 50.0 to 50.9 ms | 49.7 to 51.0 ms |
| `dense_1MiB` | 119.0 to 122.7 ms | 118.2 to 122.8 ms |
| `nested_interp_1MiB_open` | 1.31 to 1.35 ms | 1.31 to 1.33 ms |
| `heredoc_4MiB` | 27.5 to 27.9 ms | 27.8 to 28.1 ms |

B/op and allocs match on every row. The first four rows are inside the spread.
The heredoc row reads about 0.3 ms (1 %) higher, with the ranges just touching;
the 5 % growth disclosed above is the cost of scanning heredoc text for `${`,
and this check adds no more than the spread.

## Observability Evidence

Observability Evidence: no new metric. `eshu_dp_collector_content_store_files_total`
gains a `redactor` label (`marker`, `skeleton`, or `none` for `metadata`). The
stream log's per-key counts become `metadata_metadata` and
`redact_<redactor>_<outcome>`. The label lands on a live series:
`{policy="metadata",outcome="metadata"}` is already emitted for every repository
with an `.npmrc` (#8978), and it now carries `redactor="none"`. A `sum by`
query is unaffected; a query that matches the exact label set must add the
label. The two `redactor="marker"` and `redactor="skeleton"` series stay empty
until part 2 binds a definition. At 3 AM, a non-zero
`{policy="redact",redactor="skeleton",outcome="redactor_failed"}` means a tfvars
body fell back to a metadata row, and the WARN log names the paths and the
bounded reason. `TestContentStoreCounterSplitsByRedactorMode` reads the counter
from a real `telemetry.Instruments` and asserts the log holds no credential.
`scripts/verify-telemetry-coverage.sh` passes.

## Accuracy proof

- `TestRedactSkeletonLineRules` pins the record's line table, including the
  credential-in-key and free-text quoted-key cases.
- `TestRedactSkeletonPropertyCorpus` checks 3000 seeded bodies with nine
  credential shapes (AWS key, `sk_live_`, Slack token, PEM block in a comment
  and in a heredoc, base64, hex, a word with symbols, URL userinfo). The line
  shapes include the review's three bypasses: a flagged token wrapped as a
  `var.`/`module.`/`data.` reference, a bare token with `==` padding, and a
  heredoc or block comment opened after other text. Reverting any one of the
  three skeleton fixes (F1 to F3) fails this test and the fuzz seed corpus; the
  fourth fix (F4, the key-list check) is pinned by
  `TestNewRegistryRejectsInvalidModeFields`. The test checks
  that no planted token survives, that the line count holds, that a second pass
  changes nothing, that every stored line fits a grammar the test writes itself,
  and that every raw finding keeps `REDACTED_<kind>` while no other line gains a
  kind marker.
- `TestRedactSkeletonOracle` (rounds 4 and 5) compares the redactor with
  HashiCorp's lexer, `hclsyntax.LexConfig` (v2.24.0, already in `go.mod`, a test
  import only: `rg -n 'hashicorp/hcl' go/internal/collector/git/content/*.go`
  lists `skeleton_redactor_oracle_test.go` alone). The redactor keeps its own
  hand-written scanner (`skeleton_scan.go`) so that it needs O(line) memory; the
  lexer allocates 16 to 600 times the body per pass (ruling 5). The scanner is
  one value in `skeletonState` that crosses lines: a stack of 32 frames, each a
  quoted template (`s`), a heredoc template (`h`, with its marker, a
  `startOfLine` bit, and whether its closer goes), a `${ }` or `%{ }`
  interpolation (`i`), or a brace inside one (`b`), plus an open `/* */`
  comment and a sticky `failed` flag.
  - The oracle says which lines begin inside a block comment, a heredoc body, or
    a quoted template. Those lines are NON-CODE and must be stored whole. A
    heredoc closing line is exempt. For every body, valid or not, every NON-CODE
    line before a lexical failure is whole (no leak). After a quoted newline, or
    an invalid token inside a heredoc or template (a bare `\r`), every later line
    is whole. For a body that lexes without a diagnostic, the scanner and the
    oracle must agree on every line in both directions (no over-redaction). A
    body nested deeper than 32 is exempt from the second direction because the
    scanner fails closed there by design; `TestRedactSkeletonNestingDepth` pins
    that cap.
  - Three seeded generators (PCG, seed 8833) feed it, each checked as well by
    the property invariants (line count, fixed point, no planted token). Token
    soup throws `${ %{ $${ %%{ } { " \" <<EOF <<-EOF <<INNER # // /* */`, LF,
    CRLF, a bare `\r`, a burst of 40 `"${`, and a `Passw0rdHunter2: 1` line
    together with no regard for validity. Valid HCL builds multi-line
    interpolations, heredocs inside interpolations, interpolations and nested
    heredocs inside heredoc bodies, comments inside interpolations,
    `%{ if }`, CRLF, and (in about one body in four) a bare `\r` in a quoted
    template or heredoc body. Valid JSON builds `.tfvars.json` shapes.
  - Results at the final head (0 leaks and 0 over-redacted lines in every row):

    | Run | Generator | Bodies | Lex clean | Compared both ways | Lines compared |
    | --- | --- | --- | --- | --- | --- |
    | default | token soup | 20,000 | 3,562 | 2,632 | 6,552 |
    | default | valid HCL | 20,000 | 15,352 | 15,344 | 217,818 |
    | default | valid JSON | 20,000 | 20,000 | 20,000 | 190,425 |
    | opt-in `-skeleton-oracle-bodies=200000` | token soup | 200,000 | 35,302 | 25,953 | 64,178 |
    | opt-in | valid HCL | 200,000 | 152,941 | 152,887 | 2,156,980 |
    | opt-in | valid JSON | 200,000 | 200,000 | 200,000 | 1,907,052 |

    The opt-in run compares 4,128,210 lines. The test also fails when a
    generator reaches the two-way comparison on fewer than half its bodies
    (a fiftieth for token soup).
  - `TestRedactSkeletonOracleCorpus` (`-skeleton-oracle-corpus=<dir>`) is a
    one-shot run over a public corpus of 451 `.tfvars` files (22,710 lines, not
    committed): 451 lex clean, 451 compared both ways, 0 leaks, 0 over-redacted.
- The bare `\r` rule (round 5, J1). A `\r` that is not the one of a CRLF line
  ending, found inside a quoted template or heredoc text, sets the sticky
  `failed` flag, so every later line is whole. `hclsyntax` lexes it as
  `TokenQuotedNewline` in a string (and `\` followed by `\r` as `TokenInvalid`
  then `TokenQuotedNewline`) and as `TokenInvalid` in heredoc text, after which
  the lexer stops at `TokenEOF`; the scanner has no lexical state to follow
  after that point. A `\r` in code is a `TokenInvalid` and lexing goes on, and
  in a `#` comment it is valid, so neither is a failure. A CRLF ending is
  trimmed before the scan and a trailing `\r\r` leaves one bare `\r`. The
  committed `FuzzRedactSkeleton` failed at `070d079446` after about 16 s on
  `A=<<EOF\n\rEOF\n`, and `x = "a\rb"\nPassw0rdHunter2: 1\n` failed as well.
  `TestRedactSkeletonBareCarriageReturn` holds both inputs and seven more
  shapes that must fail closed, plus three that must not (a CRLF heredoc, a
  bare `\r` in a `#` comment, and one in code). The first seven failed before
  the fix. The two inputs are also fuzz seeds and corpus files, and the
  generators plant the byte.
- Mutations of the scanner, each red in at least one committed test. "Fuzz"
  is the `FuzzRedactSkeleton` seed corpus; "Oracle" is `TestRedactSkeletonOracle`
  at its default size; "Fails closed" is `TestRedactSkeletonFailsClosedAcrossLines`.

  | Mutation | Committed tests that fail |
  | --- | --- |
  | m1 reset the frame stack at each line (I1) | Fuzz, Fails closed, Oracle |
  | m2 overflow acts as an open block comment, not `failed` (I2) | `TestRedactSkeletonNestingDepth`, Oracle |
  | m3 one heredoc marker: an inner line closes the outer heredoc (I3) | Fuzz, Fails closed, `TestRedactSkeletonPropertyCorpus`, `TestRedactSkeletonTfvarsBody`, `TestRedactSkeletonBareCarriageReturn`, Oracle |
  | m4 `$${` and `%%{` open an interpolation | Fuzz, Fails closed, Oracle |
  | m5 heredoc text is not scanned for `${` | Fuzz, Fails closed, `TestRedactSkeletonBareCarriageReturn`, Oracle |
  | m6 `#` or `//` inside an interpolation drops the frames | Fuzz, Fails closed, Oracle |
  | m7 a closer is accepted when `startOfLine` is false (`${a}EOF`) | Fuzz, Fails closed, Oracle |
  | m8 the trailing `\r` is not trimmed before the scan | Fuzz, Fails closed, `TestRedactSkeletonBareCarriageReturn`, `TestRedactSkeletonPreservesLineGeometryAndCRLF`, Oracle |
  | m9 `*/` clears a failed scan | `TestRedactSkeletonFailureNeverClears`, Oracle |
  | m10 an unterminated literal resets to code | Fuzz, Fails closed, Oracle |
  | m11 a bare `\r` does not fail (the J1 fix removed) | Fuzz, `TestRedactSkeletonBareCarriageReturn`, Oracle |
  | m12 `\` then `\r` in a string is skipped | Fuzz, `TestRedactSkeletonBareCarriageReturn`, Oracle |
  | m13 `\r` is not checked in a string | Fuzz, `TestRedactSkeletonBareCarriageReturn`, Oracle |
  | m14 `\r` is not checked in heredoc text without `$` or `%` | Fuzz, `TestRedactSkeletonBareCarriageReturn`, Oracle |
  | m15 `\r` is not checked in heredoc text with `$` or `%` | Fuzz, `TestRedactSkeletonBareCarriageReturn`, Oracle |

  The first draft of m2 (overflow as a block comment with the frames kept) was
  equivalent to the code, because the open frames already redact every later
  line; the table lists the version that drops the frames, as the old scanner
  did. A draft of m14 passed the unit test and the fuzz seeds until a case
  with a `$` before the `\r` was added.
- `FuzzRedactSkeleton` and `FuzzRedactMarkerURLKeys` run the same invariants,
  and the skeleton target also runs the oracle's no-leak and fail-closed checks
  on every input. The 60 s local runs (`-parallel 4`) pass at the final head. A
  round-4 run found one false positive in the test grammar, not in the
  redactor: `A:1E1000` is a number the allowlist keeps, but `strconv.ParseFloat`
  reports a range error. The grammar now accepts it, and the input stays as a
  seed in the fuzz corpus directory of the package. A round-5 review run found
  the bare `\r` failure above; both inputs are in that directory now.
