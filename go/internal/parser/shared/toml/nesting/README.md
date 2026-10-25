# TOML Nesting Guard

## Purpose

The package bounds two decoder costs before `go-toml` sees a document: nesting
depth (below) and key-lookup work (see [Key Work](#key-work)).

`internal/parser/shared/toml/nesting` checks how deeply a TOML document nests
before a decoder parses it. `github.com/pelletier/go-toml/v2` v2.3.1 recurses
once per array or inline-table level and once per dotted-key segment, with no
depth bound. A file of about 0.9 MB of nested `[` overflows the goroutine
stack, a fatal error that `recover()` cannot catch, so one hostile repository
file used to kill the whole ingester (#8985).

## How It Works

`Check(source, limit)` makes one linear pass and returns:

- `nil` when the document nests no deeper than `limit`;
- an error wrapping `ErrTooDeep` when it does, naming the depth and byte
  offset;
- an error wrapping `ErrUnterminatedString` when a string never closes. Such a
  document is invalid TOML, so the decoder would reject it too.

Depth at a point is the number of arrays and inline tables around it plus the
dots in the dotted keys that lead to it. Key/value lines under a table header
start at the header's depth, because the decoder handles them inside the
header's recursion. Header brackets do not count, and a new header replaces the
previous header's depth.

| Document | Depth |
| --- | --- |
| `a = 1` | 0 |
| `a.b = [1]` | 2 |
| `x = {a.b = {c = 1}}` | 3 |
| `[a.b.c]` then `d = [1]` | 3 |

The lexer skips comments and the four TOML string forms with the go-toml
scanner's rules: escapes in basic strings, none in literal strings, newlines
only in multi-line strings, and a multi-line close that may carry one or two
extra quotes of content, so a run of up to five quotes ends the string. Brackets, braces, and dots inside strings and
comments never count. Where the lexer could disagree with the decoder, the
decoder has already failed on an earlier byte, so nothing after it is decoded.

## Key Work

go-toml v2.3.1 finds a key in a table with `tracker.SeenTracker.find`, which
scans a linked list, so a table of n keys decodes in n squared steps (#8991).
Measured on one table: 25,000 keys take 1.0 s, 50,000 take 4.3 s, and 100,000
take 18.5 s. Plain keys, dotted keys, inline-table keys, and child tables made
by headers (`[dependencies.a]`, `[a]`) all pay it. Repeated `[[package]]`
sections and long arrays do not.

`CheckBounds(source, Bounds{Depth, KeyWork})` adds up the squared entry count
of every table in the same pass and returns an error wrapping `ErrTooManyKeys`
once the total passes `KeyWork`. A zero field allows nothing, so a caller that
omits a bound fails closed.

- Each `=` that starts an entry counts toward the table that holds it: the
  table under the latest header, or the inline table around it. Dotted keys
  count one per `=`, which over-counts the child tables they build and never
  under-counts.
- Each distinct header text counts as one entry of some parent table, since
  `[dependencies.serde]` defines a key of `dependencies`. A repeated header
  adds nothing. Distinct headers are remembered as seeded 64-bit hashes, one
  per header, capped by the limit.
- A table of n entries adds n squared. The pass keeps the total by adding
  2n-1 per new entry, so it stops within one entry of the limit.

| Document | Work |
| --- | --- |
| `a=1`, `b=2`, `c=3` | 9 |
| `[x]` with 2 keys, then `[y]` with 1 | 4 + 1 + 4 (two headers) |
| `d = {a=1, b=2, c=3}` then `z=1` | 9 + 4 |
| 50,000 `[[package]]` sections (12.6 MB) | 1.25 million |
| 100,000 keys in one table | 10 billion |

The count never falls below half the square of the largest table go-toml
builds: a table's entries come from `=` in one place and from header children,
so its size squared is at most twice the sum of the two squared counts.
`TestKeyWorkNeverUndercountsTheDecoder` checks that against go-toml on seeded
documents.

## Cost

Depth only: the pass allocates a frame stack of at most `limit+1` entries.
`CheckBounds` adds a small set of header hashes: on a 2.5 MB legal Cargo.lock
of 10,000 packages `Check` takes 1.68 ms with 0 B/op before the key-work
change and 2.13 ms with 192 B/op after, against about 16 ms and 9.2 MB for the
go-toml decode of the same file (`BenchmarkCheckLegalLockfile`,
`BenchmarkCheckBoundsLegalLockfile`, `BenchmarkDecodeLegalLockfile`). At one
million levels the guard rejects the input within 16 KiB, and a 100,000-key
table within 1 MiB (the header set).

## Callers

- `internal/parser/rust` checks every `Cargo.toml` and `Cargo.lock` at depth
  64 and key work 25,000,000 before `toml.Unmarshal`.

Any new parser that hands repository TOML to go-toml must call `Check` first.

## Telemetry

The package emits no telemetry. A rejected file fails its parse, and the git
collector counts it in the existing parse-file status metric as `skipped`.

## Evidence

No-Regression Evidence: the guard adds one linear pass before each Cargo
decode and changes no output for legal files. Baseline is the go-toml v2.3.1
decode alone; after is guard plus decode, measured on the same host with
`go test -bench LegalLockfile -benchmem -count=5` over a generated 2.5 MB
Cargo.lock with 10,000 `[[package]]` tables: `BenchmarkCheckLegalLockfile`
about 1.67 ms/op and 0 B/op, `BenchmarkDecodeLegalLockfile` about 15.9 ms/op
and 9.2 MB/op, so the guard adds about 10% of decode time and no allocation.
`TestCargoLargeLegalLockfileMatchesUnguardedDecode` proves a 50,000-package
Cargo.lock yields the same 50,000 rows with and without the guard. No queue,
worker, lease, batch, or graph-write path changes.

No-Observability-Change: no metric, span, or log key is added. A rejected file
fails its parse through the existing error path, and the git collector already
records it in the parse-file status metric as `skipped`.
