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
arbitrary name (`?next=sk-live-...`), a double-encoded nested query, and a
secret in a path segment all still pass.

#### Why `response.data` is exempt, and what that costs

`data` holds the answer under investigation. Judging its string values would
strip the evidence the bundle exists to carry — a code search result or a code
topic can legitimately contain `key=value` text that a query parameter never
would.

The known cost, measured rather than assumed: Eshu's API does echo request
parameters back into `data`. `GET /api/v0/supply-chain/impact/explain` returns
the caller's filter at `data.input`
(`go/internal/query/supply_chain_impact_explain.go:80`), and about a dozen
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
Errors name the field (`query.target`, `query.params.next`) and stop there.

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

The excerpt and fact sentinels are planted in BOTH variants and
`IncludePayloads` only decides whether they are expected back out. Planting
them only in the private-triage variant, as this file first did, left the
public assertion checking for bytes that were never in its input, so a
regression inlining payloads by default would have passed.

CLI integration is covered by:

```bash
cd go && go test ./cmd/eshu -run 'TestReportCapture|TestReportValidate' -count=1
```
