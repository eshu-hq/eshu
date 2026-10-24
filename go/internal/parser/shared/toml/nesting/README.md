# TOML Nesting Guard

## Purpose

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

## Cost

The pass allocates only a frame stack of at most `limit+1` entries. On a
2.5 MB legal Cargo.lock it takes about 1.7 ms with 0 B/op, against about
16 ms and 9.2 MB for the go-toml decode of the same file
(`BenchmarkCheckLegalLockfile`, `BenchmarkDecodeLegalLockfile`). At one
million levels it rejects the input within 16 KiB.

## Callers

- `internal/parser/rust` checks every `Cargo.toml` and `Cargo.lock` at depth
  64 before `toml.Unmarshal`.

Any new parser that hands repository TOML to go-toml must call `Check` first.

## Telemetry

The package emits no telemetry. A rejected file fails its parse, and the git
collector counts it in the existing parse-file status metric as `skipped`.
