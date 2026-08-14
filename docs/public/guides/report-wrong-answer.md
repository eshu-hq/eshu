# Report A Wrong Answer

Eshu told you something you know is false. The owner it named is wrong, the
dependency it missed is right there in the lockfile, the trace stops one hop
short. This page turns that into a report a maintainer can reproduce.

The thing you send is a **report bundle**: one JSON file holding the query you
ran, the response that came back, and the truth labels Eshu attached to it. It
is designed to be safe to post on a public issue.

## Capture the bundle

Run the query that went wrong through `eshu report capture` instead of the
normal verb:

```bash
eshu report capture \
  --endpoint /api/v0/services/checkout/story \
  --params '{"repo":"your-org/your-repo"}' \
  --note "expected the platform team as owner, got an empty list" \
  --out report-bundle.json
```

`--endpoint` is the API path, `--params` is a JSON object of query parameters,
and `--note` is your one-line description of what you expected. Without
`--out`, the bundle goes to stdout.

Capture issues the query itself and records what comes back. If the query
returns an error rather than a wrong answer, that is still worth capturing: the
error envelope goes into the bundle.

For a question you asked through an MCP tool rather than the HTTP API, add
`--tool` to record which tool it came from. Today `--endpoint` is still what
resolves the answer; capture records the MCP surface, it does not call MCP
itself.

```bash
eshu report capture \
  --tool get_service_story \
  --endpoint /api/v0/services/checkout/story \
  --params '{"repo":"your-org/your-repo"}' \
  --out report-bundle.json
```

## Check it before you post it

```bash
eshu report validate --from report-bundle.json --require-public
```

```text
report bundle validation: passed
```

It exits 0 when the bundle is share-safe and 1 when it is not, printing the
reason. Omit `--from` to read the bundle from stdin instead.

`--require-public` is the check that matters before you attach anything. Run it
even when you are sure, and especially when the bundle came from somebody else.

## What is in a bundle, and what is not

A captured bundle looks like this:

```json
{
  "schema_version": "wrong_answer_report.v1",
  "bundle_id": "93abb72cfa31b263cba7e43fe955d151cf62c37ebd2dddcd0234c19d5726bbf7",
  "created_at": "2026-08-12T14:49:47Z",
  "reporter_note": "expected the platform team as owner, got an empty list",
  "query": {
    "surface": "api",
    "target": "/api/v0/services/checkout/story",
    "method": "GET",
    "params": { "repo": "demo/service" },
    "profile": "local_authoritative"
  },
  "response": {
    "truth": {
      "level": "exact",
      "capability": "trace.service_story",
      "profile": "local_authoritative",
      "basis": "authoritative_graph",
      "freshness": { "state": "" }
    },
    "truncated": false,
    "data": { "owners": [] },
    "data_digest": "69ab1c2e39b44d97cdd54cb9c76c5f1b7d7ddd529f932578da0324040072df3e"
  },
  "evidence": {
    "citations": [],
    "fact_refs": [],
    "fact_refs_state": "unavailable",
    "fact_refs_reason": "no public fact-record read surface"
  },
  "redaction": { "profile": "public", "rules": [] },
  "validation": {
    "status": "passed",
    "checks": [
      "schema_version",
      "bundle_id",
      "profile_payloads_consistency",
      "query_inputs",
      "reporter_note",
      "response_error_text",
      "share_safe_keys"
    ]
  }
}
```

`response.truth` is the part a maintainer reads first. It is stored exactly as
the API returned it, never re-derived, and it says how confident Eshu was and
what it based the answer on. A wrong answer labeled `exact` is a different bug
from a wrong answer labeled `derived`.

`data_digest` is a hash of the recorded response. It is what a later
conformance test compares against to prove the wrong answer stopped coming
back.

What a bundle never carries by default:

- Source excerpts. Citations keep their line numbers, file paths, and content
  hashes, but the actual lines of your code are dropped.
- Raw fact payloads. Facts appear as references (id, kind, scope, generation),
  not contents.
- Anything under a key that looks credential-shaped. `token`, `secret`,
  `password`, `credential`, `api_key`, and `authorization` keys are removed
  wherever they appear, at any nesting depth, in both your parameters and the
  response. Every removed key is listed in `redaction.rules`, so the bundle
  says what it dropped rather than quietly shrinking.

A credential you typed into `--endpoint` itself is covered too, and so is one
you passed in `--params`. Capture merges the endpoint's query string into your
parameters first and then makes one pass over the result, keeping the harmless
ones so the query is still reproducible. A parameter that hides a credential
inside its own value, as in `?next=/api/v0/x?api_key=...`, is dropped whole and
listed in `redaction.rules` — at any nesting depth, whichever way it arrived.
If the query string is malformed and cannot be taken apart at all, capture stops
and tells you rather than guess which part was secret. Capture's own error
messages name the field, never the value, so nothing leaks to your terminal
either.

The request Eshu issues still carries everything you typed — it is your API
call, and the wrong answer under investigation is the one it returns. The
redaction applies to the bundle you attach to an issue.

Your `--note` is scanned as well. People describe a wrong answer by pasting the
command they ran, so the note is where a `curl` with your own credential in it
tends to land. Two shapes are removed:

- a credential in a URL, `...?api_key=sk-live-...`, cut out of the line and
  replaced with `[redacted]`;
- a credential in a header, `-H 'Authorization: Bearer ...'` or
  `-H 'X-Api-Key: ...'`. A header value can contain spaces, so there is no safe
  place to stop early — everything from the header name to the end of that line
  goes.

The rest of the note is kept, so the repro you wrote is still readable, and
`redaction.rules` lists `reporter_note` whenever anything was taken out.

The error message Eshu returned gets the same scan. That is not a formality: a
few routes build their message by quoting the selector you asked about, so a
selector like `checkout?token=...` comes back inside `response.error.message`
even when the structured `response.error.details` beside it was already cleaned.
The credential is cut out of the sentence and the sentence is kept, listed as
`response_error_message` in `redaction.rules`. `response.error.correlation_id`
is scanned the same way, because it is your own `X-Correlation-ID` header when
you sent one.

Six limits, worth knowing before you paste something unusual:

- The rule matches credential-shaped key **names**. A secret sitting in a
  parameter named something innocuous, with no `key=` in front of it, is not
  detected.
- A credential in the **user info** part of a URL is not detected in free text.
  In `https://alice:s3cr3t@host/x` the name to the left of the `:` is `alice`,
  which is not credential-shaped, so the password after it is stored as you
  wrote it. A URL sitting in a structured field is handled elsewhere and does
  lose its user info; a URL inside your note or inside a returned error message
  does not.
- The same goes for your note. `sk-live-abc` written in a sentence, or sitting
  in a URL path rather than a parameter, is stored as you wrote it. Nothing here
  guesses whether a piece of text looks like a secret; it only recognises a
  credential-shaped **name** next to an `=` or a `:`.
- The note scan can also fire when you did not mean it to. A line like
  `no authorization: the call 403s` loses everything after `no`. If your note
  comes back shorter than you wrote it, that is why, and `redaction.rules` says
  so.
- A percent-encoded parameter (`/x%3Fapi_key%3D...`, the form a browser or an
  HTTP client writes) is unwrapped one layer before the check, so it is found
  wherever you typed it. Encode it a second time (`%253F`) and it is not: the
  unwrap runs exactly one layer, never in a loop.
- `response.data` is scanned by key name only. Some Eshu routes echo your
  request parameters back inside the answer, so a credential you typed can
  reappear there even though it was dropped from `query.params`. Read the
  recorded `data` before you attach the bundle.

The safest habit is still the obvious one: do not paste a real credential into
a bug report. Replace it with a placeholder before you run `capture`.

`fact_refs_state: "unavailable"` in the example above is expected right now, not
a capture failure. There is no public route for reading fact records, so a
capture run against a remote API cannot resolve them; a maintainer resolves
them locally during triage.

## The private-triage flag

`--include-payloads` attaches the raw fact payloads and source excerpts the
default bundle deliberately leaves out. It exists for debugging on your own
machine when the redacted bundle is not enough.

A bundle captured that way is flagged `private-triage`, carries a warning as the
first field of its payload section, and fails `--require-public`. Capture also
prints a warning to stderr. Never attach one to a public issue. If a maintainer
asks for one, send it the way you would send any other internal artifact.

## File the issue

Open a [Wrong answer](https://github.com/eshu-hq/eshu/issues/new?template=wrong-answer.yml)
issue and attach `report-bundle.json`. The form asks for the bundle and one
optional line about what you expected. That is the whole report; you do not
need to write the query out again in prose.

## What happens next

A maintainer reproduces the query from the bundle. If the answer really is
wrong, the bundle becomes a permanent conformance case: the recorded facts get
replayed on every future build, with the correct answer as the expectation. The
wrong answer then cannot come back without a test going red.

That conversion is not automatic and will not be. A person confirms the report
first.

The converter that turns a confirmed bundle into a conformance case is not
built yet. It is waiting on a decision about exposing a public route for reading
fact records, the same gap `fact_refs_state: "unavailable"` reports. Until then
a maintainer does the conversion by hand. Capture, validation, and the issue
form all work today.
