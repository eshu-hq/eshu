# internal/cli/evidredact

Makes the values of a first-run evidence artifact safe to write to disk or paste
into a support thread.

`eshu first-run --evidence` and `eshu first-run report` produce a Markdown or
JSON packet an operator is expected to attach to a public issue. This package is
what stands between the run's raw result and that file.

## The problem it solves

A redactor keyed on FIELD NAMES is correct right up until a value is *composed*
from a raw one. `service_endpoint` gets redacted; a readiness sentence built by
interpolating the same endpoint does not. That asymmetry has now leaked four
times on four different carriers, and each round looked finished because the
reported case stopped reproducing.

The four carriers, and what closes each:

| Carrier | Example | Closed by |
| --- | --- | --- |
| Structured endpoint field | `https://u:p@h/x?api_key=…#access_token=…` | `Endpoint` — userinfo, credential-named query values, whole fragment |
| Structured filesystem target | `/Users/bob/src/demo` | `Path` — final element only |
| A URL composed into prose | `could not reach https://h/x?token=…` | `Text` finds it and runs `Endpoint` over it |
| A bare pair in prose, no URL | `body was {"error":"unauthorized","api_key":"…"}` | `Text` runs `urlredact.FreeText` over the text between the URLs |

The fourth is the one the first three hid. A 401 or 403 body reaches the artifact
as the diagnosed cause and contains no `scheme://` anywhere, so a walk that split
the text on absolute URLs sent it straight through. Measured on
`eshu first-run report`: five credentials in five free-form fields reached both
the Markdown and the JSON artifact, one line under an endpoint field that was
correctly redacted.

## How `Text` composes the stages

`Text` splits the string into absolute-URL spans and the text between them, and
the two halves are treated **differently on purpose**.

- **URL spans** go to `Endpoint`. The free-text walk must not touch them: it ends
  a value at `?`, `&` or `;` and replaces the pair including its name, so it
  would turn `https://h/x?token=…&repo=demo` into `https://h/x?[redacted]&repo=demo`
  — an endpoint an operator cannot match against their own config.
- **URL-free spans** get the raw-target substitution first, then the free-text
  walk. Substitution must not run over a URL: a repo target of `//` once rewrote
  `https://h/z` to `https:.../h/z`. Nothing leaked; nothing was readable either.

Inside a span the order is measured, not arbitrary. A raw target containing a
space — an ordinary path on a developer machine — is collapsed to `.../demo`
before the pair walk sees it. Reversed, the pair walk ends the value at the space
and strands `repos/demo` where the substitution can no longer match it.

## What it does not catch

Every rule here is structural. Nothing inspects what a value looks like — no
entropy check, no secret-pattern list, because "we scan for secrets" is a claim
nobody can falsify. What follows from that:

- A credential in a URL **path segment**.
- A credential under a parameter name `collector.IsSensitiveKeyName` does not
  match.
- A bare secret with **no key beside it** — `token is sk-live-abc`.
- A credential written after a URL that follows its own key. The header rule
  stops at the end of its span and a URL ends a span, so
  `Authorization: Bearer https://h/x <credential>` loses the header and keeps the
  trailing word. `TestTextLeavesTheResidueTheSpanSplitCannotReach` pins this so a
  future change that closes it goes red rather than passing silently.

## The cost, stated

Every free-form field is scanned, including ones built only from package
literals. That is deliberate: a list of fields judged safe has to be re-decided
every time a field changes where its bytes come from, and getting that wrong is
the exact shape of the leak this walk closes.

The price is real. `export ESHU_API_KEY=<token>` is a credential-named pair, so a
recovery step containing it comes out as `export [redacted]`. The operator loses
nothing they cannot read — `renderFirstRunHuman` prints the raw diagnostic and
the raw next steps to their terminal. Only the artifact meant for a public thread
is shortened.

## Fixed point

`eshu first-run report` re-renders an artifact from a saved envelope, so a second
pass over already-scrubbed text has to find nothing new. Proven two ways:
`TestScrubbedTextIsAFixedPoint` over the package corpus, and a driver-level
re-emit that produces byte-identical Markdown and JSON.

## Where the shared mechanism lives

`internal/urlredact` owns where a `key=value` pair begins and ends, for this
package and for `internal/reportbundle` alike. It depends on nothing but the
standard library and `collector.IsSensitiveKeyName`. Two walks that each defined
their own boundary drifted once already and shipped three credentials through the
difference, so the boundary, the percent-escape depth rule, and the free-text
walk itself all live there in one copy.

This package sits under `internal/cli` because `Endpoint`'s fallback for an
unparseable value needs `mcpsetup.RedactToken`, and because the CLI is its only
consumer.
