# URL Credential Redaction Boundaries

## Purpose

`urlredact` owns one narrow thing: **where a `key=value` pair ends inside
query-shaped text**, and removing the value of every pair whose name
`collector.IsSensitiveKeyName` flags.

It exists because two redaction walks had to agree on that boundary and did not.

## The drift this package ends

Two walks clean credentials out of operator-facing artifacts:

| Walk | Where | Cleans |
| --- | --- | --- |
| `redactEndpoint` | `go/cmd/eshu/first_run_evidence.go` | a structured endpoint field in a first-run evidence report |
| `redactFreeText` | `go/internal/reportbundle/redact_free_text.go` | prose in a wrong-answer report bundle |

Both ask `collector.IsSensitiveKeyName` about the name half of a pair, and a
comment in each said the two therefore "cannot disagree". That is true about
**names** and false about **boundaries**. `redactFreeText` ended a value at `?`,
`&` or `;`. `redactEndpoint` called `strings.Split(rawQuery, "&")`. So these
three came out of an operator artifact verbatim, each measured, none caught by a
test:

```text
…/x?a=1;token=<credential>
…/x?next=/v0/y?api_key=<credential>
…/x?redirect_uri=/cb?access_token=<credential>
```

The separators now live here once, and there were **three** copies to collapse,
not two:

| Consumer | Constant | What it bounds |
| --- | --- | --- |
| `cmd/eshu` | calls `Query` | a pair inside a structured endpoint's query string |
| `reportbundle` | `queryPairSeparators` | a pair inside a query-shaped parameter VALUE |
| `reportbundle` | `freeTextValueTerminators` | where a pair's value ends in prose |

The third one is the one `redactFreeText` actually reads, and it was found only
by breaking `PairSeparators` and watching which tests moved: sharing the first
two left `reportbundle` green. It now splices `PairSeparators` into its wider
set (`" \t\r\n" + PairSeparators + "'\"` + backtick + `"`), so one edit moves
every walk.

## Exported surface

- `PairSeparators` — `"?&;"`. The one definition both walks read.
- `Query(rawQuery, marker string) string` — returns `rawQuery` with the value of
  every credential-named parameter replaced by `marker`. Names, other
  parameters, their order, and the **original separator bytes** all survive.
- `Sentinel`, `BoundaryCase`, `BoundaryCases()` — the shared conformance corpus,
  described below.

## Why the walk is an index walk

`Query` walks by index rather than `Split` then `Join`, because the separators
are not interchangeable: `?a=1;token=x` must come back with its `;` intact.
Rebuilding on one chosen separator rewrites an endpoint nobody asked to have
rewritten, and the operator then cannot match it against their own config.

`url.ParseQuery` plus `Encode` is out for the same reason one level up — that
pair sorts the parameters and re-encodes the ones it kept.

Two shapes are deliberately left alone: a pair with no `=` (`?token`) and a pair
with an empty value (`?token=`). Neither has a value to remove, and writing
`token=redacted` over them would report a credential the URL never carried.

## The shared corpus, and why it lives in a production package

`BoundaryCases()` is one table driven through **both** walks: `cmd/eshu`'s
`TestRedactEndpointBoundaryCorpus` and `reportbundle`'s
`TestRedactFreeTextBoundaryCorpus`. Neither package can import the other's
unexported functions, so the table is what they share.

Each row carries the exact output of both walks. They differ on most rows and
that difference is not interesting — `redactEndpoint` keeps the parameter name
so an operator can see which parameter was dropped, while `redactFreeText`
removes the name too, because leaving `api_key=` standing would be re-found on
the next pass and `reportbundle.Capture` runs `Validate` over its own output.

What must hold on every row is narrower, and is the thing that actually broke:
the credential is gone from both outputs, and the text around it survives.

Two rows put the credential in a **non-final** position, with text after it.
They look redundant next to the rows above and are not: when a credential is
last, its value runs to the end of the string whether or not a walk knows the
separator, so every last-position row passes against a terminator set that has
drifted. Those two rows are what makes `freeTextValueTerminators` load-bearing.

A second group of rows varies the **encoded spelling** of the same boundaries,
because a corpus that covered every separator and wrote each one literally is
what let `?a=1%3Btoken%3D…` through while `?a=1;token=…` was pinned.
`TestBoundaryCasesCoverTheEncodedSpelling` and `TestBoundaryCasesVaryTheHexCase`
fail if that stops being true — but only after both were repaired, and the
repair is the point. Each was satisfiable by a row nothing is ever removed from,
so both now ask `BoundaryCase.ProvesRemoval`: a row with a credential and no
recorded exemption. The hex-case guard also matched
`%[0-9a-fA-F]*[a-f][0-9a-fA-F]*`, which is not anchored to a two-digit escape
and ran on into ordinary text, so an uppercase `%2Fcb` satisfied a
lowercase-hex requirement through its `cb`. Deleting the only lowercase row left
that guard green. It now matches `%([0-9a-fA-F]{2})` and tests the two digits
alone.

Two more rows put an escaped separator **inside** a value whose pair was written
with a literal `=`. That is the reverse boundary, and making the scan
decode-aware broke it: see the section below.

A row a walk provably cannot handle records **why**, in `EndpointKeepsSecret` /
`FreeTextKeepsSecret`. Three rows carry a reason today, covering two cases:

- **URL userinfo** (`http://u:<credential>@h/x`, and the same URL with a query
  beside it) — the token left of the `:` is the username, which no sensitive-key
  rule matches, so only the endpoint walk closes it. Two rows.
- a **double-encoded** separator (`%253D`) — neither walk reaches it, and that
  row is where widening the unwrap to a loop would have to be argued. It is the
  only row where both reasons are set.

`CheckEndpointSecret` / `CheckFreeTextSecret` assert those reasons in **both**
directions. A walk that starts removing an exempted credential fails the test
rather than quietly widening what the corpus permits.

The corpus ships in the production package rather than a test-only sibling
because it is the contract, not a fixture: it is what "these two walks agree"
means, and putting it beside the constant they share is what keeps the two from
being edited apart. It is a few hundred bytes of string constants.

## The encoded spelling of a separator

Knowing which bytes bound a pair does not help when the bytes are written
`%26`. An HTTP client building a nested URL percent-encodes the structure, so
`?redirect_uri=%2Fcb%3Faccess_token=…` has no bare `?` and no bare `=` for a
split to find — and that is the same credential as
`?redirect_uri=/cb?access_token=…`, one of the three the separator constant was
introduced for.

So every read of a separator here goes through `escape.go`, which unwraps
**exactly one layer**, in either hex case. `Query` copies the escape through in
the spelling it arrived in; the operator's endpoint is not rewritten.

One layer, never a loop, for two reasons:

- `%253F` asks for the literal text `%3F`. A second layer would describe a
  request no server received, and a loop would let the input decide how long it
  runs.
- `reportbundle`'s free-text walk **emits** the text it scanned, and `Capture`
  runs `Validate` over its own bundle. A reader that peeled until the text
  stopped changing would hand back a string one layer shallower than it arrived,
  the next pass would peel one more and find something new, and `Capture` would
  reject the bundle it just built — leaving the reporter with nothing.

`Decode` is `url.PathUnescape`, not `url.QueryUnescape`, and the difference is
`+`. `QueryUnescape` turns `+` into a space, which is right for one parsed query
parameter and wrong for a decoder that also feeds a walk over prose: it can only
lose matches, because `token+%3Dx` then reads as `token =x`, whose key holds
whitespace and is skipped. It is the only decoder here, and a second one is the
defect this package exists to remove — `redactPair` had `url.QueryUnescape`
thirty lines from the rule saying so.

## Depth: an escape is a separator only where its pair is

Reading every escape as the byte it stands for is too much on its own, and it
introduced a partial leak worse than the whole one it fixed. `?token=AAAA%26BBBB`
joins its name to its value with a **literal** `=`, so the reporter typed it at
the surface and its `%26` is two characters the credential contains — the value
is `AAAA&BBBB`. Cutting there shipped `BBBB`:

```text
Query("token=AAAA%26BBBB", "redacted")   was  token=redacted%26BBBB
                                         now  token=redacted
```

`?a=1%26token%3D<credential>%26repo%3Ddemo` joins with `%3D`, so its whole
structure is one layer down and `%26` there is the separator it stands for.

So the spelling of a pair's own `=` decides which spellings of a terminator end
its value. `BoundaryDepth` names the two, `IndexBoundary` scans at either, and
both walks pick from the width their `=` was found at. Nothing about the
literal-`=` case is special-cased per separator: `%3B`, `%3F`, `%20`, `%22` and
`%27` all behaved the same way and are all fixed by the same rule.

The cost is **over-removal**: `?token=x%26repo=demo` loses the `repo=demo`,
because a benign parameter after an escaped separator cannot be told apart from
the tail of a credential containing an `&`. Losing a parameter is the side to
err on when the alternative is shipping half a credential.

One residue stays open, and it is not fixable from inside a string: at the
encoded depth, a value that genuinely contains an `&` is spelled `%26` — the
same bytes as the separator. `?a=1%26token%3Dse%26cret` is read as a token of
`se` followed by a parameter `cret`. Distinguishing them needs the encoder's
intent, which the bytes do not carry.

## Exported escape surface

- `EscapeWidth` — `3`, the byte length of one escape.
- `DecodedByteAt(s, i)` — the byte `s[i]` stands for, and its width.
- `DecodedEscapeBefore(s, i)` — the byte an escape ending at `s[:i]` stands for,
  for the backwards walk that reads a key name leftwards from its separator.
- `BoundaryDepth`, `LiteralOnly`, `LiteralOrEscaped` — which spellings of a
  structural byte a scan counts as that byte.
- `IndexBoundary(s, set, depth)` — offset and width of the first position
  standing for one of `set` at that depth. At `LiteralOnly` the width is always
  `1`.
- `Decode(s)` — one layer across a whole string, for a name or a value rather
  than a position.

## What this package does not do

It does not judge what a **value** looks like. No entropy check, no
secret-pattern list. It asks the shared name predicate about the left half of a
pair, exactly as every other redaction walk in this repository does.

So a credential in a **path segment**, one under a name
`collector.IsSensitiveKeyName` does not match (`?session=…`), and a bare secret
with no key beside it are all invisible here — as they are everywhere else. So
is a separator encoded twice (`%253D`), for the reason above, and so is the tail
of a credential that holds an `&` inside an already-encoded pair, for the reason
in the depth section.

Assembling a redacted URL is not here either. `redactEndpoint` keeps that: it
also strips userinfo and the fragment, and falls back to `mcpsetup.RedactToken`
for a value that does not parse as a URL, which would drag a CLI dependency into
this package for no gain.
