# Cypher statement redaction

## Purpose

`Redact` reduces a Cypher statement to its literal-free shape: every integer,
float, single-quoted string, and double-quoted string becomes `<REDACTED>`
(booleans and null are kept),
comments are dropped, and whitespace runs collapse to one space. The
graph-read policy in `internal/query` hashes that text into the statement
fingerprint and truncates it into the `graph_read.statement_head` log field
(#7035), so a value typed into ad-hoc Cypher (the `/api/v0/code/cypher` route
runs a caller's query with every value inline) never reaches a log, span, or
hash.

## Ownership boundary

This package owns the lexical scan only. It does not decide what is logged,
where the fingerprint is attached, or how long the head may be; those stay in
`internal/query/neo4j_read_policy.go`.

## Exported surface

`Redact` and the `Placeholder` constant. See [doc.go](doc.go).

## Dependencies

The Go standard library only.

## What it keeps and what it replaces

| Token | Result |
| --- | --- |
| integer, float, exponent, `f`/`d` suffix, hex `0x..`, octal `0o..`, Neo4j 5 digit separators (`4111_1111`), every trailing identifier character, a minus touching the digits | `<REDACTED>` |
| `'single'` and `"double"` quoted strings (backslash escapes, doubled quotes) | `<REDACTED>` |
| `//` and `/* */` comments | dropped |
| keywords, `true`/`false`/`null`, identifiers (`n1`), labels, relationship types, property keys, `$parameters`, `` `backtick identifiers` `` | kept |
| unterminated string, block comment, or backtick identifier | redacted to the end of the statement |

The token classes are a superset of NornicDB's `RedactLiterals`
(`pkg/cypher/redaction.go`) and of the Neo4j 5 lexer's literal forms
(`Cypher5Lexer.g4`: `INTEGER_PART` digit separators, trailing `PART_LETTER`
characters on a number token, and the `SPACE` whitespace set). NornicDB leaves
single-quoted strings, hex/octal numbers, and comments in its slow-query
`query` field; this package redacts them. The operator recipe for lining the two up is in
[Graph-read safety](../../../../../docs/public/reference/telemetry/graph-read-safety.md).

## Telemetry

No-Observability-Change (#7035): this package emits no metric, span, or log.
It is a pure function whose output feeds the `eshu.graph_read.statement_fingerprint`
span attribute and the `graph_read.statement_head` log field owned by
`internal/query`.

## Gotchas

- Whitespace is ASCII space, tab, newline, vertical tab, form feed, carriage
  return and the separators 0x1C-0x1F, plus every character `unicode.IsSpace`
  accepts. That covers Neo4j 5's `SPACE` set. U+180E, U+200B and U+FEFF are
  whitespace in neither and stay identifier characters.
- The scanner is not a parser. It does not reject malformed Cypher and never
  errors; malformed input redacts more, not less.
