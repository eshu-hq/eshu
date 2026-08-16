# URL Credential Redaction Boundaries

## Purpose

`urlredact` owns one narrow thing: **where a `key=value` pair begins and ends**,
and removing the value of every pair whose name `collector.IsSensitiveKeyName`
flags. It does that in two shapes — a parsed query string (`Query`) and prose
(`FreeText`).

It exists because two redaction walks had to agree on that boundary and did not.

## The drift this package ends

Two walks clean credentials out of operator-facing artifacts. Both now live
here, which is the strongest form of the invariant below: a boundary change
cannot reach one and miss the other.

| Walk | Called by | Cleans |
| --- | --- | --- |
| `Query` | `evidredact.Endpoint` (`go/internal/cli/evidredact`) | the query string of a structured endpoint field in a first-run evidence report |
| `FreeText` | `reportbundle.redactFreeText`, `evidredact.Text` | prose in a wrong-answer report bundle, and every free-form field of a first-run evidence artifact |

Both ask `collector.IsSensitiveKeyName` about the name half of a pair, and a
comment in each said the two therefore "cannot disagree". That is true about
**names** and false about **boundaries**. The free-text walk ended a value at
`?`, `&` or `;`. The endpoint walk called `strings.Split(rawQuery, "&")`. So
these three came out of an operator artifact verbatim, each measured, none caught
by a test:

```text
…/x?a=1;token=<credential>
…/x?next=/v0/y?api_key=<credential>
…/x?redirect_uri=/cb?access_token=<credential>
```

The separators now live here once, and there were **three** copies to collapse,
not two:

| Consumer | Constant | What it bounds |
| --- | --- | --- |
| `evidredact` | calls `Query` | a pair inside a structured endpoint's query string |
| `reportbundle` | `queryPairSeparators` | a pair inside a query-shaped parameter VALUE |
| this package | `freeTextValueTerminators` | where a pair's value ends in prose |

The third one is the one `FreeText` actually reads, and it was found only by
breaking `PairSeparators` and watching which tests moved: sharing the first two
left `reportbundle` green. It splices `PairSeparators` into its wider set
(`" \t\r\n" + PairSeparators + "'\"` + backtick + `"`), so one edit moves every
walk. It lived in `reportbundle` until the free-text walk moved here beside it;
splitting a constant from the walk that reads it is what let it drift the first
time.

## Exported surface

- `PairSeparators` — `"?&;"`. The one definition both walks read.
- `Query(rawQuery, marker string) string` — returns `rawQuery` with the value of
  every credential-named parameter replaced by `marker`. Names, other
  parameters, their order, and the **original separator bytes** all survive.
- `Sentinel`, `BoundaryCase`, `BoundaryCases()` — the shared conformance corpus,
  described below.
- `TailSentinel`, `DifferentialCase`, `DifferentialCases()` — the generated
  cross-product that compares the two walks to **each other** rather than to a
  written-down expectation. Also below.
- `Authority(value)` / `CarriesUserinfo(value)` — the userinfo question for
  values `url.Parse` reads as opaque, described next.
- `ParseErrorReason(err)` — the reason half of a net/url parse failure, spelled
  without any of the parsed input. Also below.

## The authority question (authority.go)

`url.Parse` only surfaces userinfo inside an authority, and a value only has an
authority after `//`. `svc:PASSWORD@h.internal:5432/tool` — the same shape an
operator produces by omitting `https://` — parses as scheme `svc` with the
password in `Opaque` and `User == nil`, and `String()` round-trips it verbatim.
Every sanitizer that tested `parsed.User` after a plain parse read that password
as no credential at all.

`Authority` re-parses `"//"+value` when the opaque body is authority-shaped (an
`@` ahead of its first `/`) and lets net/url decide what userinfo is; the `@`
test only selects which values are worth re-asking about, which is why
`mcp:tool/name` and `pkg:npm/lodash@4.17.21` are never rewritten — read as an
authority they would refuse values that were never credentials.
`CarriesUserinfo` is the fail-closed boolean over it: userinfo under either
reading, or a value that cannot be parsed at all, reports true, because nothing
can prove a string credential-free when it cannot be taken apart. The accepted
cost is `mailto:user@example.com`, which is structurally identical to
`svc:PASSWORD@example.com`.

Authority's errors reach terminals and logs — the places the artifact beside
them is redacted for — so they carry a reason but never the value. Stripping
the `*url.Error` envelope is only half of that promise: the envelope quotes
the whole URL, and the NESTED error can quote input again — `invalid port
":secret" after host` repeats an unbounded slice of the value, and that is
exactly where a secret sits when an operator writes `svc:user@host:SECRET/x`.
`ParseErrorReason` is the classifier both halves go through: messages known to
be static net/url constants pass verbatim, the input-quoting shapes map onto
fixed text, and an unrecognized message fails closed to a generic reason.
`cli/report`'s `requestErrorWithoutURL` reads it too, for the parse-shaped
`*url.Error` `http.NewRequest` returns.

Consumers: `cli/report`'s `targetAuthority` (the refusal), and the collector
sanitizers in `securityalerts`, `servicecatalog`, `ociregistry`,
`vulnerabilityintelligence`, `kuberneteslive`, `packageregistry`
(+ `packageruntime`), `sbomruntime`, `ospackagevulnerability`, and `cicdrun`
(both its deployment-URL sanitizer and the envelope `stripSensitiveURL` every
source_ref passes through), which drop a non-hierarchical value that carries
userinfo instead of re-stringing it. `sdk/go/collector`'s `validateSourceURI`
applies the same rule with its own copy of the opaque-authority test and of
the parse-reason classifier — the sdk module cannot import this one. An
earlier version of this list read as complete while `servicecatalog` and
`cicdrun`'s envelope sanitizer still carried the defect; count call sites
before trusting it again (`rg -n "CarriesUserinfo\(" go sdk`).

No-Regression Evidence: in every consumer, `CarriesUserinfo` runs only on the
branch where the plain parse found no host — the branch that previously
returned the value unsanitized or re-strung it — so hierarchical URLs, which
is every well-formed `https://` value these sanitizers see, take exactly the
parse they took before. The added cost on the rare non-hierarchical branch is
one extra `url.Parse` of an already-short string during fact-envelope
construction, not on any query or graph path.

No-Observability-Change: string sanitization only; no metric, span, log, or
status surface moves.

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

Four rows vary **where** in the value the escape sits, and they exist because
every row above put it after at least one value byte. All of them therefore
landed in the same cell — the pair already carries a value — while the cell
where the escape is the value's *first* byte shipped a whole credential.
`TestBoundaryCasesOpenAValueWithAnEscape` fails when any separator loses its
opening-position row.

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

## The differential, and why the corpus was not enough

A corpus row is written by somebody who already knows the case exists. Both
walks passed every row above while disagreeing on **72 of 594** generated
inputs: the endpoint walk shipped a whole credential when an escape opened the
value, and the free-text walk cut one in half at an escaped space, quote or tab
inside an already-encoded pair. Neither cell had a row, because neither was
known.

`DifferentialCases()` crosses the axes instead of enumerating the cases — the
key name, how the pair's own `=` was spelled, which byte the escape stands for,
where in the value it sits, and what follows the pair. Each generated value
carries a second sentinel *after* the escape, so truncation shows up as one
fragment removed and one kept.

Every row declares its fragments in two lists, and the split is what makes the
row count mean something:

- `Removable` — inside the value of a credential-named pair. **Both** walks must
  remove them, and `CheckRemoval` says so outright. 378 of the 594 rows carry at
  least one.
- `Outside` — past the separator that ended the pair, or under a name no
  sensitive-key rule matches. `CheckAgreement` requires only that the two walks
  decided them the same way; over-removal is this package's accepted cost, so
  neither keeping nor removing is required.

Both lists were one list, and it hid a hole. 234 of the 594 rows landed where
**both** walks keep the fragment — 36 of those under a credential-shaped name,
every `token%3D`/`api_key%3D` row whose escape is a separator. That is not a
leak: one layer down `token%3D%26X` really does carry an empty value and `X` is
a separate bare parameter. **18** of those 36 declared nothing else, so they
carried no removal assertion at all; the other 18 also declared a fragment that
really is in the value, and still signalled. A check that only compares the two
walks to each other is silent when they are both right *and* when they are both
wrong, so a regression that stopped removing anything would have passed on every
row, not just those 18.

Seven totals are pinned, and they answer four different questions. **How many
rows:** `594`, of which `378` carry a removable fragment. **How many
fragments:** `492` removable, `300` outside. **Which fragment:** `TailSentinel`
declared removable on `114` rows and outside on `84`. **How much room the table
has:** `198` inputs hold `TailSentinel`.

Each level is there because a weakening walked past the one above it, and both
were run rather than imagined:

- 114 rows carry two removable fragments (492 over 378 rows, two at most per
  row). Demote the second to `Outside` and both row totals sit exactly where
  they were, while the removal assertions drop `492` → `378` and outside goes
  `300` → `414`.
- Declare `Sentinel` twice in the "inside the value" position instead of
  `Sentinel` plus `TailSentinel`, and all four counts stay exact — every
  fragment is still contained in its input, so nothing else notices. But
  `TailSentinel` is then declared by nothing, and it is the only fragment
  sitting *after* the escape, which is the only way a partial leak is visible at
  all.
- Count those two identities per *occurrence* rather than per row, and the
  aggregate is redistributable in turn: 57 rows declaring `TailSentinel` twice
  and 57 declaring it not at all sum to the same `114`.
- Point one axis cell at another cell's text — the `"an escaped line feed"`
  entry at `%20` — and the cross-product still has eleven escapes, every literal
  holds, and the line-feed byte is crossed with nothing. Cardinality pins do not
  look inside a cell.
- Let a fragment be anything the input contains, and a row buys itself capacity
  with a proper prefix of a sentinel. Drop the head-of-value `Sentinel` from all
  114 credential *inside* rows and pay the count back with a prefix on the 114
  credential *opening* rows: every literal holds, no fragment is declared twice,
  every fragment is contained — and 114 head-of-value assertions have moved off
  the rows that carried them.

Every one of those weakenings leaves the package green except for the pin
written to catch it, and every one takes the same half of the coverage with it.
The both-walks-wrong mutation drops from 36 red subtests to 18 under either
fragment weakening, and to 27 under the occurrence one — how far it falls
depends on which rows are demoted, but what goes quiet is always the
partial-leak case.

Three rules make the numbers *forcing* rather than merely consistent. The
counters are per row. No row may declare a fragment twice. And a fragment must
BE one of the two sentinels — containment alone would admit any substring, which
is what the last weakening above exploited. A fourth rule sits beside rather
than under them: no two cells of one axis may spell the same text, because the
seven literals count cells and never look inside one.

The budget is then arithmetic, and the equality is *forced* rather than
observed. Vocabulary and no-duplicates cap each row at the distinct sentinels
its input holds, so capacity is at most `594 + 198 = 792`; containment puts the
`792` the table declares at or below capacity. Both bounds are `792`, so every
row declares every sentinel its input holds — which is also why every input
holds `Sentinel`, a consequence of the arithmetic rather than a premise of it.
No row can carry a fragment another row gave up.

What stays free is which rows take which classification, and that is the walks'
job rather than the ledger's — declaring an outside fragment removable turns 36
differential subtests red. `AGENTS.md` records the assumption still underneath
all of this.

Agreement is about the **decision**, not the bytes: the two walks emit different
text on purpose, so comparing output would need an exemption on nearly every
row. `WalksDisagree` records a row the two provably cannot agree on, asserted in
both directions like the corpus reasons. No row carries one today — and a row
where both walks keep a fragment is not one of them, because that is agreement;
it belongs in `Outside`.

Every axis carries the model's own answer as a hand-written literal —
`credentialShaped`, `encoded`, `pairSeparator` — rather than calling
`collector.IsSensitiveKeyName` or reading `PairSeparators`. An oracle that asked
the production code would move with it: break the name predicate and every row
would reclassify its fragments as `Outside`, the table would assert no removal
at all, and the differential would go green on a walk that had stopped
redacting. Both totals in `TestDifferentialCasesAreSelfConsistent` are written
down for the same reason.

`reportbundle`'s `TestRedactionWalksAgreeOnTheSharedDifferential` runs the
endpoint side through `Query`, because `package main` is not importable.
`cmd/eshu`'s `TestRedactEndpointDelegatesTheQueryWalkForTheDifferential` drives
`redactEndpoint` through the identical rows and goes red if that delegation ever
stops holding, so the substitution is pinned rather than assumed.

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
both walks pick from the width their `=` was found at. At the literal depth
nothing is special-cased per separator: `%26`, `%3B`, `%3F`, `%20`, `%22` and
`%27` all behaved the same way and one rule fixes all of them.

One layer down it is **not** one rule, and saying it was is what leaked next.
Only `?`, `&` and `;` are URL structure there. `reportbundle` also ends a value
at whitespace, a quote or a backtick, because those bound a pasted shell word —
and an encoder writes `%20` precisely because the space is *inside* a value, so
the escaped spelling is evidence of content, not of a boundary. Counting the
prose half a layer down cut the credential out of the nested callback URL and
left the tail:

```text
redactFreeText("curl 'https://h/cb?redirect_uri=%2Fx%3Faccess_token%3D<credential>%20TAIL'")
  was  curl 'https://h/cb?redirect_uri=%2Fx%3F[redacted]%20TAIL'
  now  curl 'https://h/cb?redirect_uri=%2Fx%3F[redacted]'
```

`IndexBoundaryBySpelling(s, literal, escaped)` is where a walk says which set it
means in which spelling; `IndexBoundary` is the shorthand for the same set both
ways.

Depth is one axis and **position** is another. The rule asks whether the name is
credential-shaped and whether its `=` was literal, and it must not also ask
whether a value is already there. When the escape is the value's first byte the
pending text is a bare `token=`, that second question answered "no pair here",
and the endpoint walk returned the credential whole:

```text
Query("token=%26<credential>", "redacted")   was  token=%26<credential>
                                             now  token=redacted
```

Every corpus row and every unit row put the escape after at least one value
byte, so the position axis had no coverage at all while the depth axis looked
thorough.

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
- `IndexBoundaryBySpelling(s, literal, escaped)` — the same, for a caller whose
  set differs by spelling: `literal` counts when written as itself, `escaped`
  also counts as one percent-escape. `IndexBoundary` is a caller of it.
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

Assembling a redacted URL is not here either. `evidredact.Endpoint` keeps that:
it also strips userinfo and the fragment, and falls back to
`mcpsetup.RedactToken` for a value that does not parse as a URL, which would drag
a CLI dependency into this package for no gain.
