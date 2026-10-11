# prefilter

## Purpose

`prefilter` reads the syntax tree of a compiled regular expression and derives
a sound necessary condition on text: the literal fragments any match must
contain. A caller asks `Set.Evaluate(text)` before running the expression and
skips it when the answer is false. Go's regexp engine scans the whole text even
when no match exists, so a cheap literal check removes the cost for the
majority of inputs that cannot match (#7126).

## Contract

- **Sound for every input.** A false answer proves the expression has no match
  in the text, including text that is empty, huge, multi-line, CRLF, NUL
  bearing, or invalid UTF-8. A true answer claims nothing.
- **Derived, never listed.** The only input is `Regexp.String()`. There is no
  token table to keep in step with a pattern. A fragment is required only
  when the pattern's own syntax forces it.
- **Case folding follows Go.** A `(?i)` fragment accepts the Unicode
  simple-fold orbit of each rune, so `k` also accepts U+212A KELVIN SIGN and
  `s` also accepts U+017F LONG S. An ASCII `ToLower` check would wrongly
  reject those.
- **Word boundaries follow Go.** A `\b` placed directly against a fragment is
  checked with Go's ASCII-only word rule, in which every non-ASCII rune is a
  non-word character.
- **Unprovable means ungated.** A class of four runes or fewer is a set of
  exact alternatives and a `+` part keeps its operand's requirement. Large
  classes, wildcards, and optional or starred parts add no requirement. A
  pattern with nothing derivable is reported ungated and always evaluates
  true. A pattern fragment containing U+FFFD is never gated, because invalid
  bytes decode to it.

## How the derivation works

Each node is analyzed as either *exact* (it matches one of a small set of
strings) or *need* (a boolean AND/OR condition over literals). Concatenation
joins neighbouring exact nodes into one literal, so `(?:file|templatefile)\(`
becomes `file(` OR `templatefile(`. A concatenation of non-exact parts ANDs the
literals of each part. An alternation ORs its branches, and one branch with no
requirement makes the whole alternation require nothing. Sets are capped at 16
alternatives and 128 runes; past the cap the parts are required separately,
which is less selective and still sound.

## Reading a derived gate

`Set.Describe(i)` renders the condition: `~` marks a folded literal and `\b` a
word boundary. For the module-source pattern of the HCL extractor:

```text
((("module"~ AND "\"" AND "{") OR (\b"terraform"~ AND "{")) AND \b"source"~\b AND "=")
```

## Cost

Evaluation scans each distinct literal at most once per text, shared across
the expressions of a Set, and short-circuits. It is linear in the text length
with a small constant (a byte-table scan or `strings.Index`). It allocates two
small slices per call.

## Proof

- `TestGateIsSoundOverFixturePatterns` and `TestGateIsSoundOverRandomPatterns`:
  generated matches, edits (fold swaps, boundary breaks, CRLF, truncation,
  invalid UTF-8) and noise over fixture and grammar-generated patterns.
- `FuzzGateIsSound`: `go test -fuzz FuzzGateIsSound` searches for a violation.
- `TestHarnessKillsEveryMutantAndPassesTheControl`: seeded RED/GREEN pair. The
  control is clean; each deliberately weakened gate (ASCII-only fold, flipped
  flags, dropped or required alternatives, invented boundaries) is killed.

## Ownership boundary

Standard library only. No I/O, no telemetry, no state between calls. The
consumer decides what to do with a false answer and owns any counters.
