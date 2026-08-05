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
  A dequeued opener (the `pending[0], pending[1:]` -> `current` promotion in
  `scanner.go`) keeps its OWN `quoted` and `tabStrip` state (`opener` fields
  in `scanner_lexer.go`) and its OWN `bodySize`, reset to `0` on every
  dequeue — it must never inherit the just-closed opener's state. A leaked
  `quoted` flag either hides a real unquoted-margin violation
  (quoted-leaks-onto-bare) or wrongly tightens a heredoc bash never expands
  (bare-leaks-onto-quoted); a leaked `tabStrip` misapplies `<<-` dash-stripping
  to the wrong opener's closing line; a leaked (non-reset) `bodySize` folds
  the previous opener's byte count into the next one's measurement. Guarded
  by `TestScanContent_PerOpenerQuotedSurvivesQueue`. A leaked `quoted` reds all
  three of its subtests; the other invariants each have a mutation that no other
  test in this package catches: dropping the `bodySize = 0` reset on dequeue
  is caught only by its `quoted_then_bare` subtest; a `tabStrip` leak
  (`nextOpener.tabStrip = current.tabStrip`) is caught only by
  `bare_then_dash_quoted` — the `<<-'B'` form there is chosen specifically
  because it is the only construct in the package that can observe a
  `tabStrip` leak through the queue, not merely "the dash form too"; and LIFO
  dequeue order, or capping openers at two, is caught only by
  `three_openers_alternating`.
- **`findAllOpeners` tracks quote/substitution context as a STACK, persisted
  ACROSS LINES.** It is not a per-line quote toggle. The stack (`frameSingle`
  /`frameDouble`/`frameAnsiC`/`frameSubst`/`frameArith` in `scanner_lexer.go`)
  models: a `'...'` or `"..."` string (so a `<<IDENT` inside one is never
  mistaken for a real opener — bash never treats `<<` as redirection inside a
  quoted string); an ANSI-C `$'...'` string with backslash-escape awareness
  (so `\'` does not end it early); `$(...)` command substitution as a FRESH
  unquoted scope that opens even while nested inside an outer double-quoted
  string that has not closed yet (bash does not suppress command
  substitution inside double quotes, only inside single quotes); and (2026-07
  hardening review, F1) `$((...))` arithmetic evaluation as its OWN scope,
  pushing one `frameArith` per open paren (so nested grouping parens inside
  the expression are depth-tracked, not mistaken for the closing `))`) —
  `<<` and word-starting `#` are both suppressed while `frameArith` is on
  top, since arithmetic has no heredoc or comment syntax in real bash. See
  the frameArith fix writeup further down for the fail-open this closed.
  `ScanContent` passes the
  stack from one line's `findAllOpeners` call into the next — this is load
  bearing: a double-quoted string spanning multiple physical lines, or a
  `$(...)` opened on one line and closed on a later one, must stay open
  across that line boundary. Do not reset the stack per line or per call;
  that reintroduces the exact adversarial-review false negatives these
  regression tests guard (`TestScanContent_HeredocMarkerInsideStringLiteralNotPhantomOpened`,
  `TestScanContent_AnsiCQuoteEscapedApostropheNotMisreadAsClose`,
  `TestScanContent_CommandSubstitutionInsideDoubleQuoteRecognizesHeredoc`,
  `TestScanContent_DoubleQuoteSpanningMultipleLinesTracksAcrossLines`,
  `TestScanContent_QuoteCharacterInsideOtherQuoteTypeStaysInert`, in
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
  literally its delimiter. The check (`case c == '#' && atWordStart` in the
  `default` case's inner switch) MUST come after the backslash-escape check
  described above, not before it, to preserve that invariant. It fires only
  in the base/`$(...)` context (the outer `switch top()` already routes a
  `#` inside an actual quote to the frame-specific cases, where `#` has no
  special meaning), and only when `atWordStart` is true — matching real
  bash's "comment starts a word" rule closely enough to stay conservative:
  `echo foo#bar`, `${x#pat}`, and `$#` all fail the word-start check and
  stay literal, exactly as verified against real `/bin/bash`. See
  `TestScanContent_TrailingCommentOpenerDoesNotHideRealHeredoc`
  (the RED/GREEN regression) and `TestScanContent_HashNotStartingWordStaysLiteral`
  (the non-comment edge cases) in `scanner_quoting_test.go`. This fix is a
  narrowing, like the unquoted-margin fix above, not a closure of #5079: a
  small literal heredoc referencing an unbounded expansion is still a
  separate, open gap (see `doc.go`).
- **Word-start (`atWordStart`) is EXPLICIT STATE (a `wordStart` local in
  `findAllOpeners`), never a raw byte lookback at `line[i-1]`.** A raw
  lookback cannot tell a genuine separator apart from one already consumed
  as half of a wider, variable-width unit — and a P1 REGRESSION shipped
  exactly that way from the trailing-comment fix above: a backslash-escaped
  blank (`x\ #<<EOF`) leaves that blank byte physically sitting at
  `line[i-1]` even though the escape branch (`case c == '\\' && i+1 <
  len(line): i += 2`) already consumed it as the second half of a two-byte
  unit, so the old `line[i-1] == ' ' || line[i-1] == '\t'` check wrongly
  read it as a real separator that bash itself does not treat as one —
  misreading a genuine heredoc opener as a trailing comment (0 heredocs
  detected, exit 0, the identical fail-open the trailing-comment fix itself
  was written to close). `wordStart` is instead captured into `atWordStart`
  at the TOP of each loop iteration (before that iteration mutates
  anything), then explicitly reset for the NEXT iteration by what actually
  happened: true only after a real (unescaped, unquoted) blank or a
  statement-separator operator (`;`, `|`, `&`) was itself consumed as its
  own token — false after everything else, including an escape's second
  byte, any quote/substitution open or close, and a matched heredoc opener.
  The `;`/`|`/`&` recognition is a related fix shipped in the same change:
  the old raw-byte check only ever recognized blank/start-of-line, so
  `true;#<<EOF` and `true|#<<EOF` were NOT treated as comments even though
  real bash treats both as one — which manifested as the SAME dangerous
  fail-open (the phantom-opened `<<EOF`/`<<FAKE` fragment swallows a real
  heredoc later in the file, since it never finds a literal closing line and
  is dropped as unterminated). Both regressions are proven RED-then-GREEN in
  `TestScanContent_EscapedWhitespaceBeforeHashStaysLiteral` and
  `TestScanContent_RealWordBoundaryBeforeHashIsGenuineComment` in
  `scanner_quoting_test.go`, each verified against real `/bin/bash` first.
  The `;`/`|`/`&` set was, at the time, deliberately narrow — only the bytes
  actually verified against real bash. **CLOSED (2026-07 hardening review,
  F4):** a bare `(`, a bare `)` (not the one closing `$(...)`), a bare `<`,
  and a bare `>` are now ALSO in the word-separator set (see
  `isShellWordSeparator` in `scanner_delim.go` and the F4 writeup further
  down) — do not describe this set as covering only `;`/`|`/`&` in new docs
  or comments; it is shared between `findAllOpeners`'s `wordStart` and
  `parseDelim`'s unquoted-word scan.
- **CLOSED (2026-07 hardening review, F3), was previously a "Known gap"
  here:** a heredoc opener split immediately after `<<`/`<<-` by a
  backslash-newline line continuation (`cat <<\` then a newline, then the
  delimiter on the next physical line) is valid bash, and this scanner now
  finds it — see the line-continuation fix writeup further down.
  `TestScanContent_ContinuationSplitRightAfterHeredocOperatorNowClosed` in
  `scanner_continuation_test.go` is the regression proof. Do not
  re-introduce a per-physical-line-only comment/opener scan in
  `ScanContent` without re-checking that test.
- **`scanner.go`, `scanner_lexer.go`, and `scanner_delim.go` are one logical
  unit, split only to stay under the repo's 500-line-per-file cap.**
  `scanner.go` owns the line-by-line `ScanContent`/`ScanFile`/`ScanTree`/
  `closesHeredoc` state machine, including the backslash-newline
  line-continuation fusion loop (F3, see below). `scanner_lexer.go` owns the
  character-by-character quote/substitution/comment/arithmetic lexer
  (`opener`, the `frame*` constants, `inQuoteFrame`, `findAllOpeners`) that
  `ScanContent` drives once per (possibly fused) logical line.
  `scanner_delim.go` owns `parseDelim` and the `isShellWordSeparator`
  word-boundary predicate it shares with `findAllOpeners`'s `wordStart`
  tracking. Change them together; do not let one drift ahead of the other's
  tests.
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
  already recorded. **Do not restate the margin's justification as resting
  on expansion being one-directional** — unset or empty variable references
  SHRINK a body at runtime (proven against real bash: a 30-byte source body
  of `${V}${V}${V}${LONG_UNSET_VAR}`, the scanner's own reported `Size`,
  delivers 1 byte with every variable unset), so shrinkage alone would not
  justify comparing against a threshold below the raw budget. The actual
  justification is that a body's growth at runtime is statically
  unbounded — an array or command substitution can expand to any size — so
  any margin below the raw budget can only ADD flags relative to the raw
  budget alone, never remove any.
- **Baseline is generated, never hand-written.** `RenderBaseline` is the only
  writer; it must exactly match what `ScanTree` finds, or the gate becomes
  unreliable (either extra false failures or holes an author could hide new
  offenders in). Regenerate with `-update`, never by editing the file.
- **The baseline entry format is stable to line-number churn.** It is
  `<path> <count>`, not `<path> <line>`, specifically so an unrelated diff
  elsewhere in a script does not spuriously bump the count.

## 2026-07 hardening review: F1-F4 (four more P1 fail-opens, closed)

A fourth review found four more constructs where the scanner reported ZERO
heredocs on a file with a genuine over-budget one, each verified against
real `/bin/bash` and production `ScanContent` first. All four are fixed at
the mechanism level; see `doc.go` for the full writeup and `scanner_arith_test.go`,
`scanner_delim_test.go`, `scanner_continuation_test.go`, `scanner_wordsep_test.go`,
and `scanner_probes_test.go` for the regression proof:

- **F1 — arithmetic (`frameArith`, `scanner_lexer.go`).** `$((` now pushes
  its own frame (one per open paren, so nested grouping parens are
  depth-tracked), distinct from `frameSubst`. `<<` and word-starting `#` are
  both suppressed while it is on top of the stack. Do not route `$((` through
  the plain `$(` case — that is exactly the bug this closed (an identifier-
  shaped shift operand, e.g. `x=$(( flags << shiftamount ))`, was read as a
  real heredoc opener).
- **F2 — delimiter character set (`parseDelim`, `scanner_delim.go`).**
  `parseDelim` no longer uses an identifier-shaped character check. It now
  derives the accepted character set from `isShellWordSeparator` (shared
  with F4 below), handles a backslash anywhere in an unquoted delimiter as
  an escape (stripped from the name, forces `quoted = true` — covers both
  the `<<\DELIM` idiom and a mid-word escape), and handles a quote character
  appearing MID-word as a concatenated quoted segment (`<<FOO'BAR'` →
  delimiter "FOOBAR", `quoted = true`) rather than literal quote bytes in the
  name. The QUOTED-delimiter branch no longer runs any identifier-shaped
  check on its content either — any bytes between the matching quotes are
  accepted, including empty. Do not reintroduce `isIdentByte`/`isIdentifier`
  or an identifier-shaped restriction on either branch.
- **F3 — line continuation (`ScanContent`, `scanner.go`).** Before checking
  for a full-line comment or calling `findAllOpeners`, `ScanContent` now asks
  `findAllOpeners` (via its third return value) whether the line ends in a
  dangling backslash that real bash would splice onto the next physical
  line, and if so fuses the next physical line directly onto it (repeating
  for consecutive continuations) before doing anything else with the text.
  This applies at the base level and inside `$(...)`/`$((...))`/double
  quotes, but NOT inside a single-quoted or ANSI-C `$'...'` string (verified:
  backslash has no escape meaning there, so a trailing one is kept literally,
  never spliced). Do not special-case `#` instead of fixing this at the
  line-joining level — that was the P1 shape of this exact bug (a
  continuation fusing mid-word onto a line starting with `#` is NOT a real
  bash comment, but a per-physical-line-only comment shortcut and
  `wordStart` reset both wrongly treated it as one).
- **F4 — word-separator set (`isShellWordSeparator`, `scanner_delim.go`).**
  Bare `(`, `)` (excluding the one that closes `$(...)`), `<`, and `>` are
  now word separators, alongside the pre-existing blank/`;`/`|`/`&`. Shared
  by both `findAllOpeners`'s `wordStart` tracking and F2's `parseDelim`
  unquoted-word scan — do not hand-maintain two separate separator lists
  again; that drift is exactly what let F2's bug (an unquoted delimiter's
  end-of-word rule not matching the comment rule's word-start definition)
  exist in the first place.

Also see `scanner_probes_test.go` for the mandated post-fix adversarial hunt
(arithmetic nested both directions with command substitution, `<<` inside
`[[ ]]`, `#` after `!`, `<<` inside an array literal, `<<-` with mixed
tabs/spaces) — all matched already-correct behavior, recorded as named
regression guards, except the two F2 findings folded into the bullet above.

## 2026-07 hardening review round 2: two more P1 fail-opens in F2 itself (P1-1, P1-2)

A codex review of PR #5890 — the PR that shipped the F2 rewrite above — found
two more constructs where F2's broadened word scan still reported ZERO
heredocs (or a mismeasured one) on a script with a genuine over-budget body.
Both live in `parseDelim` (`scanner_delim.go`); both verified against real
`/bin/bash` first; see `scanner_crlf_test.go` and `scanner_quoted_prefix_test.go`
for the transcripts and regression proof:

- **P1-1 (CRLF bare delimiter, `stripTrailingCR`, `scanner_delim.go`).** F2's
  word scan does not stop at `\r` (correctly — real bash does not treat `\r`
  as a word separator either), so a CRLF script's bare opener parses to a
  delimiter that keeps the trailing `\r`. `closesHeredoc` (`scanner.go`)
  already stripped a trailing `\r` from the candidate closing line
  (pre-existing CRLF tolerance, predating F2), so the two sides never
  matched and the heredoc was dropped as unterminated. Fixed with ONE shared
  `stripTrailingCR` helper that both `parseDelim` and `closesHeredoc` route
  through. Do not reintroduce a second hand-written `TrimSuffix(x, "\r")` on
  only one side — that asymmetry is exactly what caused this bug.
- **P1-2 (quoted-leading concatenated delimiter, `parseDelim`,
  `scanner_delim.go`).** The separate top-level branch that used to handle a
  delimiter STARTING with a quote returned immediately once the quote
  closed, instead of continuing to read the rest of the word the way the
  mid-word quote case (`<<FOO'BAR'`) already did. An opener whose delimiter
  was an empty quoted pair directly followed by a literal returned an empty
  delimiter, so the scanner waited for a blank closing line and folded every
  intervening line — including a real heredoc's own opener/body/closer text
  — into one phantom "quoted" heredoc measured against the wrong (full, not
  margin) budget. The severe consequence: a later genuine unquoted heredoc
  in the 385-512 byte margin window could be swallowed into that phantom and
  never independently measured against the margin, bypassing it entirely
  (see `TestScanContent_PhantomQuotedBodySwallowsLaterUnquotedMarginHeredoc`).
  Fixed by deleting the separate leading-quote branch entirely: a leading
  quote now falls into the SAME word-scan loop and quote case that already
  handles a quote appearing mid-word. Do not reintroduce a special-cased
  leading-quote branch — the whole point of the fix is that leading and
  mid-word quotes share one code path.

This round's class hunt additionally probed a `\r` mid-word, `<<` at
end-of-line, a delimiter that is only quotes, a quoted segment containing
`\r` or a separator byte, a trailing backslash, CRLF combined with `<<-`, and
CRLF combined with a line continuation — all either fixed by the same two
changes above or already correct (see `scanner_crlf_test.go` and
`scanner_quoted_prefix_test.go` for each as a named regression guard).

Deliberate limitation kept from this round: `stripTrailingCR` normalises a
trailing `\r` identically whether it is a CRLF artifact or genuinely quoted
content, so a script with MISMATCHED opener/closing-line endings now closes
under this scanner even though real bash would call it unterminated —
intentional, safety-favoring leniency (see `stripTrailingCR`'s doc comment),
not something to "fix" toward stricter bash fidelity.

## Common changes and how to scope them

- **Changing the byte budget** → the `-budget` flag already supports this;
  do not hardcode a new constant. The default (512) is the macOS pipe-buffer
  size from the deadlock itself — changing it without re-deriving that
  number from the actual OS behavior would misrepresent the safety margin.
- **New heredoc opener syntax to recognize** → extend `findAllOpeners` in
  `scanner_lexer.go` and/or `parseDelim`/`isShellWordSeparator` in
  `scanner_delim.go`, and add a fixture-backed test case mirroring the
  existing `TestScanContent_*` tests in `scanner_test.go`,
  `scanner_quoting_test.go`, `scanner_arith_test.go`, `scanner_delim_test.go`,
  `scanner_continuation_test.go`, or `scanner_wordsep_test.go` (quote/
  substitution-context cases live in `scanner_quoting_test.go`; arithmetic,
  delimiter-charset, continuation, and word-separator cases live in their
  own like-named files) (see `golang-engineering`: tests must exercise the
  real scanner, not a re-implementation of it). Before shipping a fix,
  reproduce the claimed bug against REAL `/bin/bash` first (a throwaway
  script + `bash script.sh`), not just against this scanner's own output —
  several of the fixes in this file were adversarial-review findings that
  turned out to need real-bash ground truth to pin down the exact broken
  construct.
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
