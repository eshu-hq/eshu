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
scans a linked list, so a table of n keys decodes in about half n squared
steps (#8991). Measured on one table: 25,000 keys take 1.0 s, 50,000 take
4.3 s, and 100,000 take 18.5 s. Plain keys, dotted keys, inline-table keys,
and child tables made by headers (`[dependencies.a]`, `[a]`) all pay it, and
a repeated header pays it again: naming an already-seen table rescans the
siblings every time, so repeats cost per repeat and are not bounded by the
distinct header texts.

`CheckBounds(source, Bounds{Depth, KeyWork, KnownTables})` adds up that work
in the same pass and returns an error wrapping `ErrTooManyKeys` once the
total passes `KeyWork`. A zero field allows nothing, so a caller that omits a
bound fails closed.

- Each `=` in key position charges the entries its lookup scans: a plain
  entry of m segments charges m times its scope's entries so far plus the
  distinct headers so far, and an inline-table entry charges m times its own
  inline table's entries. A table of n entries therefore adds about half n
  squared, and the pass stops within one entry of the limit.
- Each header charges its segments plus one times the distinct headers so
  far plus the widest scope seen, repeated headers included. Distinct headers
  are remembered as seeded 64-bit hashes, one per header, capped by the
  limit.
- Every root key and every key in a table the decode target knows is charged
  in full, known field or not: go-toml tracker-checks them all and clears its
  per-key skip at the end of each key, so 30,000 unknown keys in one
  `[[package]]` element really cost about 450 million tracker steps. The one
  exemption needs the caller to name its struct target's top-level tables in
  `KnownTables`: go-toml never enters a table the target does not know, so
  plain entries under a single-segment header naming no known table (the v1
  lockfile's `[metadata]`) are not charged. Headers, inline tables, dotted
  headers, and quoted headers are charged in full either way, so a spelling
  the pass does not decode can only over-charge. A map target passes an empty
  set, so every entry is charged.

| Document | Work |
| --- | --- |
| `a=1`, `b=2`, `c=3` | 6 |
| `[x]` with 2 keys, then `[y]` with 1 | 18 |
| `d = {a=1, b=2, c=3}` then `z=1` | 9 |
| 50,000 `[[package]]` sections (12.6 MB) | 1.6 million |
| 100,000 keys in one table | 5 billion |

The count never falls below the decoder's tracker steps: each charge is an
upper bound of the list its lookup scans, every header repeat is charged, and
the unknown-table exemption mirrors the one skip the decoder really makes.
The charge may exceed the steps, and map bounds reject some documents a
struct decode would finish quickly; that over-count is policy, and
`KnownTables` exists so legal struct-decoded files (a v1 lockfile's
`[metadata]` checksums) are not refused. `TestKeyWorkNeverUndercountsTheDecoder`
and `TestStructShapeWorkCoversTheDecoder` check the work against a
step-counting copy of the go-toml tracker on seeded documents, including
2,000 unknown keys inside one `[[package]]` element so the oracle cannot
drift back to a per-key skip the decoder does not have;
`TestStructBoundsRejectUnknownKeysInKnownTable` requires the 30,000-key
document to be rejected under the lock bounds.

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
  64 and key work 25,000,000 before `toml.Unmarshal`; lockfiles pass the lock
  struct's known tables (`{"package"}`) so entries under tables the decoder
  never enters are not charged.

Any new parser that hands repository TOML to go-toml must call `CheckBounds`
first with an explicit `KeyWork` bound (and `KnownTables` for a struct
target). `Check` alone bounds depth only and gives no key-work protection,
though it allocates the same per-distinct-header hash set.

## Telemetry

The package emits no telemetry. A rejected file fails its parse, and the git
collector counts it in the existing parse-file status metric as `skipped`.
