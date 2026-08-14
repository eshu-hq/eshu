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

A row one walk provably cannot handle records **why**, in
`EndpointKeepsSecret` / `FreeTextKeepsSecret`. Two rows do today:

- a **percent-encoded key name** (`api%5Fkey`) — the endpoint walk decodes the
  name before asking the predicate; the free-text walk cannot, because walking
  back from `=` over key runes stops at `%` and reads the name as `5Fkey`.
- **URL userinfo** (`http://u:<credential>@h/x`) — the token left of the `:` is
  the username, which no sensitive-key rule matches.

`CheckEndpointSecret` / `CheckFreeTextSecret` assert those reasons in **both**
directions. A walk that starts removing an exempted credential fails the test
rather than quietly widening what the corpus permits.

The corpus ships in the production package rather than a test-only sibling
because it is the contract, not a fixture: it is what "these two walks agree"
means, and putting it beside the constant they share is what keeps the two from
being edited apart. It is a few hundred bytes of string constants.

## What this package does not do

It does not judge what a **value** looks like. No entropy check, no
secret-pattern list. It asks the shared name predicate about the left half of a
pair, exactly as every other redaction walk in this repository does.

So a credential in a **path segment**, one under a name
`collector.IsSensitiveKeyName` does not match (`?session=…`), and a bare secret
with no key beside it are all invisible here — as they are everywhere else.

Assembling a redacted URL is not here either. `redactEndpoint` keeps that: it
also strips userinfo and the fragment, and falls back to `mcpsetup.RedactToken`
for a value that does not parse as a URL, which would drag a CLI dependency into
this package for no gain.
