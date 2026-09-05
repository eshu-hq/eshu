# reportbundle

## Purpose

`reportbundle` owns the deterministic `wrong_answer_report.v1` schema, the
`Capture` composer, the key-name redaction walk, and the `Validate`
fail-closed gate. A report bundle packages one captured query/response pair —
surface, target, params, the verbatim `query.TruthEnvelope`, redacted response
data plus its replay-equality digest, and evidence references — into one
share-safe artifact a user attaches to a wrong-answer GitHub issue.

This is the artifact `eshu report capture` (`go/cmd/eshu/report_cmd.go`)
produces and `eshu report validate` checks. It is a sibling of, and
deliberately separate from, `go/internal/evidencebundle`: that package owns a
different artifact (`evidence_bundle.v1`, an operator-state demo/proof
snapshot) with a different lifecycle; this package's bundle is reporter-driven
and captures one wrong answer, not an operator's evidence trail.

## Ownership boundary

The package does not perform HTTP calls, MCP invocations, durable fact-store
reads, or Git access. Callers (the CLI command) resolve the
`query.ResponseEnvelope` and any evidence/fact hydration and pass it in via
`CaptureInput`; `reportbundle.Capture` is a pure composer, redactor, digester,
and validator.

## Redaction contract

Redaction decides by key name, reusing the SAME rule
`sdk/go/collector`'s fact emission path enforces
(`collector.IsSensitiveKeyName`, a thin exported wrapper over the unexported
`sensitiveQueryPattern` / `redactionSafePayloadKeys` / `validatePayloadKeys` in
`sdk/go/collector/validation.go`), not a value-content scan — matching Ifá's
established posture that eshu redacts by key name, never by masking value
content.

A sensitive-named key is **removed from the tree entirely**, not kept with a
masked value. This is a deliberate design choice, documented in `redact.go`:
`validatePayloadKeys` flags a key by name alone regardless of its value, and
the sensitive-key regex is a substring match, so no masked-but-present or
renamed-but-recognizable key can survive `reportbundle.Validate`'s fail-closed
`collector.ValidateShareSafeKeys` gate over the finished document. Removing the
key instead means a properly redacted bundle can never trip its own validator;
the stripped key names are still recorded in `Redaction.Rules` (a `[]string` of
values, which the key-name walk never inspects).

`Capture` refuses to return a bundle that fails its own `Validate` call — a
public-profile bundle that would trip the gate is a bug, not something the CLI
silently ships.

### Values are invisible to the key-name rule

Because both the redaction walk and `collector.ValidateShareSafeKeys` judge key
NAMES, neither ever reads a string value. A credential pasted into a string
value is outside the gate's reach no matter which field holds it.

The rule is attached to where the data came from, not to how it arrived:

| Provenance domain | Fields | Treatment |
| --- | --- | --- |
| Reporter-typed query input | `query.target`, all of `query.params`, `response.error.details` | key-name walk **plus** the `embeddedSensitiveKey` structural scan, at any depth; unparseable target fails closed |
| Free text | `reporter_note`, `response.error.message`, `response.error.correlation_id` | line-by-line scan for a sensitive-named key beside `=` or `:` (`redactFreeText`); the matched span is replaced, the rest of the text kept |
| Server-produced evidence | `response.data`, `response.truth` | key-name walk only |

`Capture` merges the target's query string into `query.params` first and then
runs the query-input walk once over the merged map, so there is no route into
`query.params` — the `--endpoint` query string, a nested second `?` inside one
of its values, `--params`, or a programmatic `CaptureInput` — that can reach a
bundle without being scanned. Benign parameters survive, because a bundle that
drops the query it captured cannot be reproduced.

Three review rounds got here by closing one arrival path at a time, and the
same credential came back through the next path every time. That is why the
domain, not the path, is what the rule is written against. If you add a
free-string field to the Query section, it is scanned by default; exempting it
needs a reason recorded next to the exemption, not silence.

The scan finds a `key=value` shape, not secrets. A bare credential under an
arbitrary name (`?next=sk-live-...`) and a secret in a path segment still pass.

Each value is scanned as it arrived and once percent-decoded. Only the target's
query string is decoded upstream by `url.ParseQuery`, so
`/x%3Fapi_key%3Dsk-live` reached the scan with its escapes intact when it came
in through `--params` or a programmatic `CaptureInput`: no literal `?`, no
literal `=`, nothing for the split to find, and both `Capture` and the
`Validate` half that mirrors it accepted it. Which flag the reporter used is not
a property of the text, so the decode happens inside `embeddedSensitiveKey`
rather than at one arrival point. It runs **exactly one layer** and never feeds
its own output back in — re-decoding until the text stops changing would let a
crafted value drive the loop — so a double-encoded nesting (`%253F`) still
passes. The decoder is `urlredact.Decode`, shared with the free-text walk and
with `cmd/eshu`, so the three cannot drift on how deep they look.

#### The note is scanned too, and why it is scanned differently

`reporter_note` used to be assigned verbatim. The bytes
`next=/api/v0/x?api_key=sk-live` were rejected in `query.target` and accepted in
`reporter_note` — same text, same person typed it, two verdicts decided by which
field held it. The plan this package implements annotates the field as "key-name
walked like everything else"
(`docs/internal/design/4595-wrong-answer-report-capture-plan.md:203`), which was
never true of a bare string: there are no key names in it to walk, and
`reporter_note` matches nothing in the sensitive pattern. So the field was not
overlooked; it was believed covered by a rule that could not apply to it.

The note is where a pasted `curl` lands, because the reporter guide asks for a
repro. Two shapes are removed, both structural, both asking
`collector.IsSensitiveKeyName` about the token beside the separator:

- `key=value` — the URL form. The value is cut at the first whitespace, quote,
  `?`, `&`, or `;`, so `curl 'https://h/x?repo=demo&api_key=sk-live'` becomes
  `curl 'https://h/x?repo=demo&[redacted]'`.
- `key: value` — the header form, `-H 'Authorization: Bearer …'` and
  `-H 'X-Api-Key: …'`. An HTTP header value may contain spaces, so there is no
  safe inner boundary and the removal runs to the **end of the line**.

The header form is covered because a pasted `curl` carries the same token
twice — once in the header, once in the URL — and stripping one copy while
shipping the other would be worse than doing nothing, since the bundle would
then claim a redaction happened.

The key is removed along with the value, never masked in place. Leaving
`Authorization:` standing would be re-found on the next pass, and since `Capture`
runs `Validate` over its own output, it would then refuse to emit any bundle
whose note had ever been redacted. Idempotency is asserted per case in
`TestRedactReporterNote`.

The span is replaced rather than the whole note dropped. The note is the
reporter's own account of what they expected, and a pasted repro with the secret
cut out is still the repro — dropping the field would cost a maintainer the most
useful prose in the bundle on a false positive and a true one alike. Every
removal adds `reporter_note` to `redaction.rules`, so a shortened note is
accounted for rather than silent.

What the note scan does **not** find, stated so nobody reads more into it:

- A bare secret with no key beside it (`I authenticated with sk-live-abc`).
- A secret in a path segment, or under a parameter name the sensitive-key
  predicate does not match.
- No entropy or secret-pattern heuristic is used, deliberately. "We scan for
  secrets" is a claim nobody can check; this one is narrow enough to state.

The cost is a false positive on prose: `no authorization: the call 403s` loses
the rest of that line, and `SELECT token = 1` loses the pair. That trade is a
shortened note against a live credential on a public issue.

#### The error envelope's prose is scanned too

`response.error.message` was left alone on the recorded belief that it was a
"fixed contract field the redactor has no key names to work with", covered by
`Validate`. Neither half was true. The message is composed: an ambiguous service
story formats the caller's selector into
`service selector %q matched multiple services`
(`query/service_workload_resolution.go:39`) and emits it beside a
`details.selector` holding the same string
(`query/service_story_seam.go:129`). So the redactor dropped the selector from
`details` — recording rule `selector` — and shipped it verbatim one field over,
in a bundle stamped `profile: public` / `validation: passed`, which the reporter
guide tells people to attach to a public issue. And `Validate` never named
`message` at all.

The mechanism is worth stating because it generalises: `redactErrorEnvelope`
does `redacted := *envelope` and then walks only `Details`. A struct copy takes
every scalar field along, so any field not explicitly scanned afterwards is
shipped as-is. `Message` is assigned a non-literal value at more than twenty
sites in `internal/query` — some interpolating `err.Error()` directly, most
passing a variable composed further up — so the rule lives at this egress
boundary rather than at each composer.

`correlation_id` is scanned for the same reason one level over: it is the
request's own `X-Correlation-ID` / `X-Request-ID` header when the caller sent one
(`query/documentation.go:470`), and `query/auth.go:430` puts it in an error
envelope without the character allowlist the audit path applies.

`code`, `capability` and `profiles` are still copied unscanned. That is a
different claim, not the same one restated: each is a server-side constant — an
`ErrorCode` enum value, a capability name declared as a package const, a
`QueryProfile` pair — with no route from caller input.

The false positives are the same trade the note scan makes, and a message pays
it more often than a note does, because a message is short. `encode oidc secret:
%w` (`query/admin_provider_config_build.go:107`) has a sensitive name directly
before its `:`, so the bundle records `encode oidc [redacted]` and lists
`response_error_message`. A maintainer loses that sentence; the alternative is
shipping the class of message likeliest to have a real secret in it.

A credential carried as a **query parameter** is covered. `https://host/mcp?token=…`
inside a message or a note keeps the URL and loses the pair.
`evidredact.Endpoint` (`go/internal/cli/evidredact`) does the same for a
structured endpoint field.

Sharing `collector.IsSensitiveKeyName` is what stops the two walks drifting on
which NAMES count. It said nothing about where a pair ENDS, and that is where
they did drift: this package ended a value at `?`, `&` or `;`, the endpoint
walk split on `&` alone, and a comment claiming the two could not disagree was
read as covering both. Three credentials shipped through the gap —
`?a=1;token=…`, `?next=/v0/y?api_key=…`, `?redirect_uri=/cb?access_token=…`.
The separators now live once, in `internal/urlredact`, and so does the prose
scan itself: `redactFreeText` here is a thin call to `urlredact.FreeText` that
supplies only the DOMAIN answer — which fields are free text, and this package's
extra inline-content key. `queryPairSeparators` for the parameter-value scan is
the one boundary constant still derived here. Both walks are then driven through
one shared corpus (`urlredact.BoundaryCases`) that records every row either walk
cannot handle, with its reason.

The next axis under the same boundary was **how the separator is spelled**. Both
walks read only the literal bytes, so
`?redirect_uri=%2Fcb%3Faccess_token%3D…` — the third credential in that list,
written the way a browser or an HTTP client writes it — went straight back
through. Only the structured domain decoded, because `embeddedSensitiveKey` had
its own unwrap, so a reviewer flipping a test constant to its encoded spelling
watched `query.target` stay green while `reporter_note` and
`response.error.message` both leaked: one verdict per field, for text the same
person typed.

Reading a separator through one layer of percent-encoding now lives in
`urlredact/escape.go` and every walk uses it, `embeddedSensitiveKey` included.
The free-text scan reads `%3D` as `=`, `%3A` as `:`, `%26`/`%3F`/`%3B` as pair
boundaries and `api%5Fkey` as `api_key`, in either hex case, and emits the bytes
around the removal in the spelling they arrived in. **One layer, never a loop** —
here the reason is sharper than for the detector: this walk emits what it
scanned and `Validate` scans that output again, so a reader that peeled until the
text stopped changing would make `Capture` reject its own bundle. `%253D` is
therefore three characters of text, and that is pinned by a corpus row rather
than left implicit.

Decoding a separator is not the same as decoding a **boundary**, and treating
them alike introduced a partial leak worse than the whole one it closed. A pair
joined by a literal `=` is text at the depth the reporter typed, so an escape
inside its value belongs to the value: `?token=aa%26bb` is a token whose value
is `aa&bb`. Cutting there left `bb` in the note. An escape now ends a value only
when that value's own `=` arrived encoded too — one rule at that depth, covering
`%26`, `%3B`, `%3F`, `%20`, `%22` and `%27`, which all truncated identically.
The accepted cost is over-removal, the same trade the header rule already makes.

One layer down, where the pair's own `=` arrived encoded, it is **not** one
rule. Only `?`, `&` and `;` are structure there. Whitespace, a quote and a
backtick end a value because they bound a pasted shell word, and an encoder
writes `%20` precisely because the space is *inside* a value — so the escaped
spelling is evidence of content. Counting it a layer down cut the credential out
of the nested callback URL and left the tail:

```text
redactFreeText("curl 'https://h/cb?redirect_uri=%2Fx%3Faccess_token%3D<credential>%20TAIL'")
  was  curl 'https://h/cb?redirect_uri=%2Fx%3F[redacted]%20TAIL'
  now  curl 'https://h/cb?redirect_uri=%2Fx%3F[redacted]'
```

`urlredact`'s `freeTextEscapedValueTerminators` returns the set that also counts
escaped — `urlredact.PairSeparators` one layer down, nothing at the surface — and
`urlredact.IndexBoundaryBySpelling` takes the literal and escaped sets apart.
`%22`, `%27`, `%09` and `%0A` all leaked the same way and are all closed by the
same split.

This walk and the endpoint walk decide depth independently, which is how both
leaks reached review: each passed every row of the shared corpus.
`TestRedactionWalksAgreeOnTheSharedDifferential` now compares the two to each
other over 594 generated inputs, where they disagreed on 72 before the fixes. It
also asserts outright that both walks removed the credential on the 378 rows
that carry one — comparing the walks to each other says nothing when they stop
removing together, which is what breaking the name predicate they share does.

Four gaps remain, each the reverse of something above:

- Free text has no **userinfo** rule, so `https://alice:s3cr3t@host` passes this
  scan untouched. The token left of the `:` is `alice`, which no sensitive-key
  rule matches. A structured field gets that case from `evidredact.Endpoint`; a
  note or an error message does not.
- A credential on the **next line** is found only when a backslash says the line
  carried on — a wrapped `curl` writes `-H 'Authorization: \` and puts
  `Bearer …` underneath, and both lines go. Wrapped without the backslash, the
  second line is a bare secret with no key in front of it, which is the standing
  limit.
- A separator encoded **twice** is out of reach, by the rule above.
- Inside an **already-encoded** pair, a value that genuinely contains an `&` is
  spelled `%26` — the same bytes as the separator there. `?a=1%26token%3Dse%26cret`
  reads as a token of `se` followed by a parameter `cret`, so `cret` stays. The
  bytes do not carry the encoder's intent, so nothing inside this walk can tell
  the two apart.

#### Why `response.data` is exempt, and what that costs

`data` holds the answer under investigation. Judging its string values would
strip the evidence the bundle exists to carry — a code search result or a code
topic can legitimately contain `key=value` text that a query parameter never
would.

The known cost, measured rather than assumed: Eshu's API does echo request
parameters back into `data`. `GET /api/v0/supply-chain/impact/explain` returns
the caller's filter at `data.input`
(`go/internal/query/supplychain/impact/supply_chain_impact_explain.go::SupplyChainImpactExplanationResult`), and about a dozen
routes echo `query`, `question`, `subject`, `environment`, or `intent`. So a
credential a reporter typed into `--endpoint` is dropped from `query.params`
and can still return through the server's echo. Closing that means treating
named `data` subfields as query input, which is a scope and ownership decision
this package has not taken. Until it does, a maintainer reviewing a bundle
should read `data` as unscanned.

`Validate` re-checks the whole query-input domain independently (`query_inputs`
in `ValidationChecks`) rather than trusting that `Capture` produced the file — a
maintainer runs `--require-public` against bundles other people send them, and a
hand-edited `"params":{"next":"/x?api_key=..."}` used to pass every check here.

### Errors never echo a value

No user-supplied string is interpolated into an error this package returns.
Errors name the field (`query.target`, `query.params.filters.redirect[1]`) and
stop there.

A parameter NAME is reporter-typed too, and `url.ParseQuery` percent-decodes key
names, so `--endpoint '/x?api_key%3Dsk-live-...'` produces one parameter whose
name is literally `api_key=sk-live-...`. Building a path out of that name put
the credential straight back into the message, which is the defect this whole
check exists to catch, one level up. `safePathSegment` replaces a key that is
itself a sensitive `key=value` pair with `[redacted-key]`; a merely
sensitive-NAMED key such as `api_key` is still named, because that is a field
name and naming it is the point of the message. The same limit applies as
everywhere else here — a credential under a benign-looking name is repeated.

These errors land in terminals, CI logs, and pasted bug reports — the same
places the bundle itself is redacted for, so a message that quoted the target
undid the redaction beside it. `url.ParseQuery`'s own error is wrapped with
`%w`: its two shapes are an `EscapeError` that quotes the three-byte escape
token and a fixed semicolon sentence, neither of which repeats a value. The
full-egress canary, not that argument, is what keeps it true.

The CLI follows the same rule. `eshu report capture` must put the reporter's
real query string on the wire, and `net/http` quotes the whole request URL in
its transport errors, so a wrong port used to print the credential to stderr.
`requestErrorWithoutURL` (`go/cmd/eshu/report_cmd.go`) replaces the URL with the
bare endpoint path and keeps the transport cause. A 4xx/5xx body that echoes the
URL back is not covered.

`--include-payloads` (private-triage only) attaches raw citation excerpts and
resolved fact envelopes verbatim under `Bundle.Payloads`; every other section
of the bundle is still redacted and re-validated. `reportbundle.Validate`
excludes only the `Payloads` section from the share-safe walk in that case.

## Verification

Focused package gate:

```bash
cd go && go test ./internal/reportbundle -count=1
```

Two canaries, and they check different channels.

`redaction_canary_test.go` (`TestCapture_RedactionCanary`) plants
sensitive-shaped key names with unique sentinel values in query params,
response data, citation excerpts, and fact payloads, then asserts the
serialized default bundle's BYTES never contain a sentinel value — not merely
that the keys were renamed.

`redaction_egress_test.go` widens that to the whole package boundary. It plants
a sentinel in each reporter-typed query input — the target's query string, a
nested `?` inside one of its values, an explicit `--params` value, a value
nested inside a `--params` object, `error.details`, and the malformed-query
variant — and asserts the sentinel appears in no output the package produces:
not the marshaled bundle, and not the `.Error()` string of any error returned
by `Capture` or `Validate`, on the success path or the parse-failure path.

The error half is the part that was missing. Nothing in the suite ever read a
returned error, which is how `fmt.Errorf("parse query string of target %q", …)`
echoed the reporter's full endpoint through three review rounds.

`TestReporterInputPlacementSymmetry` is the control the note scan was built
against: one byte sequence, placed in `query.target` and in `reporter_note`, must
get the same verdict from `Capture` and from `Validate`. The asymmetry was the
defect, so symmetry is the guard.

Every sentinel in the egress canary is planted in a **value**, so none of it can
catch a credential planted in a **key**. That is a measured, open gap —
`url.ParseQuery` percent-decodes key names, so `?api_key%3Dsk-live-X` becomes a
parameter literally named `api_key=sk-live-X`, which `Capture` drops and then
copies verbatim into `redaction.rules`, and which the share-safe gate quotes back
in `Validate`'s rejection message. The reasoning and the reproduction are in the
header comment of `redaction_egress_test.go`; closing it needs two decisions this
package has not taken.

The excerpt and fact sentinels are planted in BOTH variants and
`IncludePayloads` only decides whether they are expected back out. Planting
them only in the private-triage variant, as this file first did, left the
public assertion checking for bytes that were never in its input, so a
regression inlining payloads by default would have passed.

CLI integration is covered by:

```bash
cd go && go test ./cmd/eshu -run 'TestReportCapture|TestReportValidate' -count=1
```
