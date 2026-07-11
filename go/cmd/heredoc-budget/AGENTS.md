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
  converting all ~120 existing offenders — that conversion is out of scope
  for #5074 Slice 1 and belongs to later slices.
- **`<<<` here-strings are never heredocs.** `findOpener` in `scanner.go`
  explicitly skips past a third `<` so a here-string's trailing `<<` cannot
  be mistaken for a heredoc opener with a garbage delimiter. Do not "simplify"
  this into a plain `<<` regex — that regression is exactly what the
  `TestScanContent_HereStringIgnored` test guards against.
- **One heredoc open at a time.** The scanner only reinterprets text as a
  potential opener when it is *not* currently inside a body. This is what
  keeps a DELIM-like word inside another heredoc's body from mis-closing it
  early (`TestScanContent_DelimWordInsideOtherBodyNotMisclosed`).
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
- **New heredoc opener syntax to recognize** → extend `findOpener` /
  `parseDelim` in `scanner.go`, and add a fixture-backed test case mirroring
  the existing `TestScanContent_*` tests (see `golang-engineering`: tests
  must exercise the real scanner, not a re-implementation of it).
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
  `findOpener`/`closesHeredoc` altered detection broadly; diff the baseline
  and check whether the new counts make sense per-file before committing.
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
