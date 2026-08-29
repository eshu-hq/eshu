# #6268 / #6230 — bare-CR line comments and prefixed eshu commands

## #6268 — the loss was whole-file, not partial

Three scanners ended a line comment only at `\n`. Under classic-Mac line endings:

- **JSONC** — the first `//` ran to EOF, `Parse` returned `unexpected end of JSON
  input`, and the file yielded no entities at all.
- **Gradle** — the comment ran to EOF, and separately an unclosed single-line
  string absorbed every following line into a runaway string.
- **SQL** — a `--` between `DROP` targets swallowed the rest of the tail.

All three now stop at `\r` as well as `\n`.

Stopping the comment scan was necessary but not sufficient. Four consumers
downstream of it split statements or counted lines on `\n` alone, so a file that
now survived the strip was still parsed wrong. Those are fixed here too —
`collectBlocks` and `splitDependencyStatements` (Gradle), `atLineStart` and
`skipLineComment` (SQL segmenter) — along with the two line indexes,
`buildNewlineIndex` (JSON) and `newSQLLineIndex` (SQL), which would otherwise
have stamped every recovered row in a bare-CR file with line 1.

**The first fix was bypassed end-to-end, and review caught it.**
`splitSQLStatements` skips `--` comments with `strings.IndexByte(rest, '\n')`,
which returns `-1` on a file with no `\n`, so the first comment swallowed
everything *before* tree-sitter saw the DROP list. The original test called
`parseDropTargetTail` directly and passed while the real `Parse` path stayed
broken. Every regression here now drives `Parse` or `splitSQLStatements`.

**Two claims this PR previously made were false.** The first said "no other
production line-comment scan is affected". The sweep behind it grepped
`!= '\n'`, which finds character loops and misses `IndexByte`, `bufio.Scanner`,
`(?m)$`, `strings.Split` and `Cut`. The corrected sweep found 29 hand-rolled
line-comment scans, 21 of them broken. That is tracked in #6306, which fixes the
class at the read boundary; this PR fixes the three scanners it names, the
consumers they feed, and no longer claims more.

The second said CRLF input produces byte-identical scanner output. It does not,
and this PR's own tests disprove it — the No-Regression evidence below names the
byte that changed and why it is harmless.

## #6230 — `VAR=value eshu …` and `sudo eshu …` were invisible

The gate neither attributed nor skipped those lines, so flags on them went
unchecked. The prefixes are now stripped so the flags are validated rather than
counted, and both call sites share the helper — their disagreement about what an
eshu command line is was the defect.

Counters re-derived by running the gate: attributed 190 → 195, skipped unchanged
at 1, floor 150 → 156. 1755 references checked / 769 baselined, before and after.

No-Regression Evidence: no runtime path changed. Every parser edit adds `\r`
beside a `\n` that a terminator or line-break counter already handled — no new
pass over the source, no new allocation, and no change to any Cypher, query
shape, writer, worker knob or batch size. LF input is byte-identical, because
none of the added branches can fire without a `\r` in the file.

CRLF input is byte-identical everywhere except one place. The `\r` inside a
CRLF `//` line comment used to be consumed by the comment scan and is now
emitted by the JSONC and Gradle strippers, so their stripped result gains that
one byte. It is whitespace to the JSON decode and to every downstream regex, the
offset map still points at the source byte that produced each result byte, and
no reported `line_number` moves — but it is a real byte difference, so it is now
asserted exactly rather than described. `TestStripJSONCCommentsCRLFBytesAreExact`
and `TestStripCommentsCRLFBytesAreExact` pin the whole stripped result, which the
older "content found + comment removed" controls could not have noticed either
way. Everything else is genuinely unchanged: the SQL segmenter copies the
comment span verbatim and the trailing `\n` falls through the caller's normal
path, Gradle's runaway-string recovery returns before the `\r` that the main
loop then writes itself, and both line indexes count a CRLF pair once
(`TestNewlineIndexCountsBareCRAndCRLFOnce`,
`TestParseCRLFJSONCReportsRealLineNumbers`,
`TestParseCRLFMigrationKeepsEveryDropTarget`).

`go test ./internal/parser/... ./cmd/docs-cli-env-refs -count=1` exit 0, 43
packages ok.

Mutation proof, 13 substitutions, `go vet` exit 0 on each mutant before its test
ran so every red is behavioural rather than a compile error: each added `\r`
condition reverted one at a time, all 13 killed by a named regression — the
three comment terminators, the Gradle runaway-string and statement-flush
conditions, `collectBlocks`' line advance, the SQL `atLineStart` and
`skipLineComment` terminators, and the bare-CR and counts-CRLF-once halves of
both line indexes.

A 14th substitution is how a dead branch was found. `collectBlocks` carried an
explicit "skip the `\n` of a CRLF pair" step; reverting it left every test
green, because `blockHeaderAtCursor` cannot match a leading newline and the
cursor consumes that `\n` on the next pass anyway. It was deleted rather than
kept as a guard no test can hold to account.

Two assertions were vacuous in their first draft, and the mutants caught that
too: Gradle string interiors are copied verbatim, so a runaway string re-labels
text instead of deleting it, and the multi-dependency fixture never reached the
unclosed-string branch at all. Both tests were rewritten against their own
mutant until it died.

The docs-cli-env-refs pins are proven exact rather than merely satisfied:
overriding pinned-skipped to 0 or 2, or the attributed floor to 196, each fails
with the count named.

Observability Evidence: no metric, span, log line or status field changes. The
operator-visible difference is that `verify-docs-cli-env-refs.sh` now names
unknown flags on prefixed commands it previously passed over silently — the
issue's own reproduction goes from exit 0 with both bogus flags invisible to
exit 1 naming both.

## Not established

- The rest of the class. 21 of the 29 hand-rolled line-comment scans are still
  broken by a bare CR; this PR fixes only the three it names and the consumers
  they feed. #6306 covers the rest at the parser read boundary, where
  measurement established that tree-sitter does not advance its row counter on a
  bare CR (Row=0) — which is why normalizing there, rather than teaching every
  scan about `\r`, is the fix that generalizes.
