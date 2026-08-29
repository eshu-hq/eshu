# #6268 / #6230 — bare-CR line comments and prefixed eshu commands

## #6268 — the loss was whole-file, not partial

Three scanners ended a line comment only at `\n`. Under classic-Mac line endings:

- **JSONC** — the first `//` ran to EOF, `Parse` returned `unexpected end of JSON
  input`, and the file yielded no entities at all.
- **Gradle** — the comment ran to EOF, and separately an unclosed single-line
  string absorbed every following line into a runaway string.
- **SQL** — a `--` between `DROP` targets swallowed the rest of that line.

All three now stop at `\r` as well as `\n`.

**The first fix was bypassed end-to-end, and review caught it.**
`splitSQLStatements` skips `--` comments with `strings.IndexByte(rest, '\n')`,
which returns `-1` on a file with no `\n`, so the first comment swallowed
everything *before* tree-sitter saw the DROP list. The original test called
`parseDropTargetTail` directly and passed while the real `Parse` path stayed
broken. Every regression here now drives `Parse` or `splitSQLStatements`.

**A claim this PR previously made was false.** It said "no other production
line-comment scan is affected". The sweep behind it grepped `!= '\n'`, which
finds character loops and misses `IndexByte`, `bufio.Scanner`, `(?m)$`,
`strings.Split` and `Cut`. The corrected sweep found 29 hand-rolled line-comment
scans, 21 of them broken. That is tracked in #6306, which fixes the class at the
read boundary; this PR fixes the three scanners it names and no longer claims
more.

## #6230 — `VAR=value eshu …` and `sudo eshu …` were invisible

The gate neither attributed nor skipped those lines, so flags on them went
unchecked. The prefixes are now stripped so the flags are validated rather than
counted, and both call sites share the helper — their disagreement about what an
eshu command line is was the defect.

Counters re-derived by running the gate: attributed 190 → 195, skipped unchanged
at 1, floor 150 → 156. 1755 references checked / 769 baselined, before and after.

No-Regression Evidence: no runtime path changed. The parser edits are
single-condition terminators on existing scans — one added `\r` comparison per
scan, no new allocation, no new pass over the source, no change to any Cypher,
query shape, writer, worker knob or batch size. LF and CRLF inputs produce
byte-identical scanner output, proven by CRLF control tests kept in each package;
the scan now stops *on* the `\r` and the following `\n` goes through the normal
path. `go test ./internal/parser/... ./cmd/docs-cli-env-refs -count=1` exit 0.

Mutation proof, `go vet` exit 0 on each mutant first so every red is behavioural:
four scanner conditions reverted one at a time, all four killed. The third
mutant survived the first draft of its test — string interiors are copied
verbatim, so a runaway string re-labels text rather than deleting it — and the
assertion was rewritten until the mutant died. The obvious assertion there is
vacuous.

The docs-cli-env-refs pins are proven exact rather than merely satisfied:
overriding pinned-skipped to 0 or 2, or the attributed floor to 196, each fails
with the count named.

Observability Evidence: no metric, span, log line or status field changes. The
operator-visible difference is that `verify-docs-cli-env-refs.sh` now names
unknown flags on prefixed commands it previously passed over silently — the
issue's own reproduction goes from exit 0 with both bogus flags invisible to
exit 1 naming both.

## Not established

- The Gradle fixture proves one post-comment dependency. A multi-`implementation`
  bare-CR build.gradle still collapses, because `splitDependencyStatements`
  flushes only on `\n` or `;`. Out of scope here; covered by #6306's read-boundary
  normalization.
- CRLF output is byte-identical for the scanners' *stripped* result, asserted by
  content rather than by byte comparison. The `\r` inside a CRLF line comment is
  now emitted where it was previously swallowed — benign, since the offset map
  stays faithful and `line_number` is unaffected.
