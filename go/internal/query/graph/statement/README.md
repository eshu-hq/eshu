# Cypher statement redaction

## Purpose

`Redact` reduces a Cypher statement to its literal-free shape: every integer,
float, single-quoted string, and double-quoted string becomes `<REDACTED>`,
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
| integer, float, exponent, `f`/`d` suffix, hex `0x..`, octal `0o..`, a minus touching the digits | `<REDACTED>` |
| `'single'` and `"double"` quoted strings (backslash escapes, doubled quotes) | `<REDACTED>` |
| `//` and `/* */` comments | dropped |
| keywords, identifiers (`n1`), labels, relationship types, property keys, `$parameters`, `` `backtick identifiers` `` | kept |
| unterminated string, block comment, or backtick identifier | redacted to the end of the statement |

The token classes follow NornicDB's `RedactLiterals` (`pkg/cypher/redaction.go`)
and are a strict superset: NornicDB leaves single-quoted strings, hex/octal
numbers, and comments in its slow-query `query` field; this package redacts
them. The operator recipe for lining the two up is in
[Graph-read safety](../../../../../docs/public/reference/telemetry/graph-read-safety.md).

## Telemetry

No-Observability-Change (#7035): this package emits no metric, span, or log.
It is a pure function whose output feeds the `eshu.graph_read.statement_fingerprint`
span attribute and the `graph_read.statement_head` log field owned by
`internal/query`.

## Gotchas

- Whitespace is ASCII only (space, tab, newline, vertical tab, form feed,
  carriage return). A non-breaking space is treated as part of an identifier,
  like any other non-ASCII byte.
- The scanner is not a parser. It does not reject malformed Cypher and never
  errors; malformed input redacts more, not less.
