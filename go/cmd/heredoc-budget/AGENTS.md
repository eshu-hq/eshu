# AGENTS.md — cmd/heredoc-budget guidance for LLM assistants

## Read first

1. `go/cmd/heredoc-budget/README.md` — purpose, flags, wiring.
2. `go/cmd/heredoc-budget/doc.go` — the bash 5.1+/macOS pipe-buffer deadlock
   background and the safe alternatives to a large heredoc.
3. `scripts/heredoc-budget-baseline.txt` — the current burn-down baseline.

## Invariants this package enforces

- **Burn-down, not a ban.** The gate fails only on regression: a new file
  with an over-budget heredoc, or an existing baselined file's over-budget
  count going up. A count staying the same or decreasing always passes. Do
  not change this to a hard "zero over-budget heredocs" gate without first
  converting every baselined offender in `scripts/heredoc-budget-baseline.txt`
  — that conversion belongs to later #5074 slices, not this package.
  **Known gap (pre-existing, not introduced or fixed by this package's
  changes):** `CheckBaseline` in `baseline.go` compares a per-file violation
  **count**, not the identity of which heredoc it is. Fixing one baselined
  violation while a different, unrelated over-budget heredoc appears
  elsewhere in the same file leaves the count unchanged, so `CheckBaseline`
  reports `OK: true` with no regression signal — a real "swap" blind spot,
  disproven against this exact package (see `doc.go`/README "Known
  limitations"). This is not something to redesign in this package without
  an ADR from the #5074 owner; it is documented here so it is not
  rediscovered as a surprise.
- **`<<<` here-strings are never heredocs.** `findAllOpeners` in `scanner_lexer.go`
  explicitly skips past a third `<` so a here-string's trailing `<<` cannot
  be mistaken for a heredoc opener with a garbage delimiter. Do not "simplify"
  this into a plain `<<` regex — that regression is exactly what the
  `TestScanContent_HereStringIgnored` test guards against.
- **Only one heredoc BODY is measured at a time.** The scanner only
  reinterprets text as a potential opener when it is *not* currently inside a
  body. This is what keeps a DELIM-like word inside another heredoc's body
  from mis-closing it early (`TestScanContent_DelimWordInsideOtherBodyNotMisclosed`).
  A line that opens more than one heredoc (`cmd <<A <<B`) queues the extra
  openers (`pending` in `ScanContent`) and processes them in order right
  after the current one closes — bash reads their bodies back to back
  immediately after the command line (`TestScanContent_TwoOpenersOnOneLineBothMeasured`).
- **`findAllOpeners` tracks quote/substitution context as a STACK, persisted
  ACROSS LINES.** It is not a per-line quote toggle. The stack (`frameSingle`
  /`frameDouble`/`frameAnsiC`/`frameSubst` in `scanner_lexer.go`) models: a `'...'`
  or `"..."` string (so a `<<IDENT` inside one is never mistaken for a real
  opener — bash never treats `<<` as redirection inside a quoted string);
  an ANSI-C `$'...'` string with backslash-escape awareness (so `\'` does
  not end it early); and `$(...)` command substitution as a FRESH unquoted
  scope that opens even while nested inside an outer double-quoted string
  that has not closed yet (bash does not suppress command substitution
  inside double quotes, only inside single quotes). `ScanContent` passes the
  stack from one line's `findAllOpeners` call into the next — this is load
  bearing: a double-quoted string spanning multiple physical lines, or a
  `$(...)` opened on one line and closed on a later one, must stay open
  across that line boundary. Do not reset the stack per line or per call;
  that reintroduces the exact adversarial-review false negatives these
  regression tests guard (`TestScanContent_HeredocMarkerInsideStringLiteralNotPhantomOpened`,
  `TestScanContent_AnsiCQuoteEscapedApostropheNotMisreadAsClose`,
  `TestScanContent_CommandSubstitutionInsideDoubleQuoteRecognizesHeredoc`,
  `TestScanContent_DoubleQuoteSpanningMultipleLinesTracksAcrossLines`, in
  `scanner_test.go`/`scanner_quoting_test.go`). The full-line `#`-comment
  shortcut in `ScanContent` is gated on `inQuoteFrame` for the same reason: a
  "#"-looking line that is really the tail of a still-open multi-line string
  must not be skipped, or its closing quote is never found and the leaked
  open-quote state corrupts every later line. The `default` case's FIRST
  check MUST stay the backslash-escape check (`c == '\\' && i+1 <
  len(line)`) — it is what makes the extremely common `'\''`
  embedded-single-quote idiom (close, escaped literal quote, reopen) work.
  Without it, the escaped `\'` reads as opening a fresh quote frame that the
  immediately following reopen-`'` instantly closes again, landing back at
  base one idiom-cycle early; a literal `"` still inside what bash considers
  the reopened string is then misread as a real double-quote open that never
  finds its close, desyncing the stack for the rest of the file. This was
  found — not designed up front — while proving the quote-stack fix against
  this repo's own `scripts/verify-remote-e2e-remediation-benchmark.sh`; see
  `TestScanContent_EmbeddedSingleQuoteIdiomDoesNotDesyncStack`. **Do not trust
  a quote-tracking change just because its own unit tests pass — always
  re-run the old-vs-new comparison (below) against the real `scripts/` tree
  before calling a scanner change safe.**
  **Known gap, not fixed here:** legacy backtick `` `cmd` `` substitution is
  NOT given its own stack frame the way `$(...)` is.
- **`findAllOpeners` also recognizes an unquoted, word-starting `#` as a real
  bash comment that ends scanning for the rest of the line.** This closes a
  P1 fail-open found after the full-line-comment fix above shipped: only a
  FULL-line comment was recognized, so a comment trailing real code on the
  same line (`echo x # <<EOF`) was not treated as a comment at all, and the
  `<<EOF` fragment inside it phantom-opened the scanner exactly like the
  full-line case — silently swallowing a real over-budget heredoc elsewhere
  in the file (0 detected, exit 0) whenever no later line happened to be
  literally its delimiter. The check (`case c == '#' && (i == 0 ||
  line[i-1] == ' ' || line[i-1] == '\t')` in the `default` case's inner
  switch) MUST come after the backslash-escape check described above, not
  before it, to preserve that invariant. It fires only in the base/`$(...)`
  context (the outer `switch top()` already routes a `#` inside an actual
  quote to the frame-specific cases, where `#` has no special meaning), and
  only when the preceding byte is a blank or the `#` is the first byte of the
  line — matching real bash's "comment starts a word" rule closely enough to
  stay conservative: `echo foo#bar`, `${x#pat}`, and `$#` all fail this
  start-of-word check and stay literal, exactly as verified against real
  `/bin/bash`. See `TestScanContent_TrailingCommentOpenerDoesNotHideRealHeredoc`
  (the RED/GREEN regression) and `TestScanContent_HashNotStartingWordStaysLiteral`
  (the non-comment edge cases) in `scanner_quoting_test.go`. This fix is a
  narrowing, like the unquoted-margin fix above, not a closure of #5079: a
  small literal heredoc referencing an unbounded expansion is still a
  separate, open gap (see `doc.go`).
- **`scanner.go` and `scanner_lexer.go` are one logical unit, split only to
  stay under the repo's 500-line-per-file cap.** `scanner.go` owns the
  line-by-line `ScanContent`/`ScanFile`/`ScanTree`/`closesHeredoc` state
  machine; `scanner_lexer.go` owns the character-by-character
  quote/substitution/comment lexer (`opener`, the `frame*` constants,
  `inQuoteFrame`, `findAllOpeners`, `parseDelim`, `isIdentifier`,
  `isIdentByte`) that `ScanContent` drives once per line. Change them
  together; do not let one drift ahead of the other's tests.
- **Unquoted heredocs get a stricter effective budget — but the margin is a
  heuristic, not a closed fix.** `Heredoc.Unquoted` (set from whether the
  delimiter was `<<'DELIM'`/`<<"DELIM"` vs bare `<<DELIM`) drives
  `unquotedThreshold` in `ScanTree`: an unquoted heredoc is compared against
  `budget - budget/unquotedMarginDivisor` (384 bytes at the default 512-byte
  budget), not the raw budget, because bash expands `${var}`/`$(cmd)` in its
  body at runtime and a literal-under-budget source can still deadlock once
  expanded (#5085). Do not compare `Heredoc.Size` directly against `budget`
  for an unquoted heredoc without going through `unquotedThreshold` — that
  reintroduces the #5085 blind spot. **But do not describe this margin as
  closing #5085 in docs, commits, or PR text:** it only narrows the window
  for a source body already near budget. A tiny literal body referencing an
  unbounded expansion (e.g. `${arr[*]}` over a large array) still passes
  silently — disproven with a real 17-byte-source/607-runtime-byte
  counter-example. A construct-based rule (flag any unquoted heredoc whose
  body contains `$(...)` regardless of size) was measured against this
  repo's real `scripts/**/*.sh` and would newly flag roughly a third of the
  existing baselined files, almost all ordinary bounded command
  substitution — too noisy to ship. See `doc.go`/README "Known limitations"
  for the full writeup; do not re-run that measurement without cause, it is
  already recorded.
- **Baseline is generated, never hand-written.** `RenderBaseline` is the only
  writer; it must exactly match what `ScanTree` finds, or the gate becomes
  unreliable (either extra false failures or holes an author could hide new
  offenders in). Regenerate with `-update`, never by editing the file.
- **The baseline entry format is stable to line-number churn.** It is
  `<path> <count>`, not `<path> <line>`, specifically so an unrelated diff
  elsewhere in a script does not spuriously bump the count.

## Common changes and how to scope them

- **Changing the byte budget** → the `-budget` flag already supports this;
  do not hardcode a new constant. The default (512) is the macOS pipe-buffer
  size from the deadlock itself — changing it without re-deriving that
  number from the actual OS behavior would misrepresent the safety margin.
- **New heredoc opener syntax to recognize** → extend `findAllOpeners` /
  `parseDelim` in `scanner_lexer.go`, and add a fixture-backed test case mirroring
  the existing `TestScanContent_*` tests in `scanner_test.go` or
  `scanner_quoting_test.go` (quote/substitution-context cases live in the
  latter) (see `golang-engineering`: tests must exercise the real scanner,
  not a re-implementation of it). Before shipping a fix, reproduce the
  claimed bug against REAL `/bin/bash` first (a throwaway script + `bash
  script.sh`), not just against this scanner's own output — several of the
  fixes in this file were adversarial-review findings that turned out to
  need real-bash ground truth to pin down the exact broken construct.
- **Converting an offending script** (later slices) → after rewriting a
  script's heredocs, rerun `-update` so its baseline count drops (or its
  entry disappears entirely at zero); do not manually edit the baseline
  number to match.

## Failure modes and how to debug

- Symptom: gate fails with `FAIL scripts/x.sh: N heredoc(s) over 512 bytes
  (baseline M)` → either `scripts/x.sh` is new (baseline has no entry, so
  `known` is false and any violation fails) or an existing baselined file's
  count went up. Fix the heredoc (see `doc.go` for the safe alternatives) or,
  if the addition is intentional and reviewed, run `-update` to accept the
  new count into the baseline.
- Symptom: `-update` produces an unexpectedly large diff → likely a change to
  `findAllOpeners`/`closesHeredoc`/`unquotedThreshold` altered detection
  broadly; diff the baseline and check whether the new counts make sense
  per-file before committing.
- Symptom: gate hangs — it should never hang; it does no `bash` heredoc
  execution of its own, only static Go string scanning. A hang here signals
  a bug in `ScanTree`'s directory walk (e.g. a symlink cycle under
  `scripts/`), not the bash deadlock this gate exists to catch.

## What NOT to change without an ADR

- The flag names (`-baseline`, `-update`, `-budget`) — the CI gate command in
  `specs/ci-gates.v1.yaml` depends on them.
- The baseline file's location (`scripts/heredoc-budget-baseline.txt`) —
  it doubles as the scan root (`-baseline`'s directory), so moving it changes
  what gets scanned.
