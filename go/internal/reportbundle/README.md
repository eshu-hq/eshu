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

Redaction is key-name based, reusing the SAME rule
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
NAMES, neither ever reads a string value. Any field that carries user input as
a bare string is therefore outside the gate's reach, and has to be handled by
name before it lands in a bundle.

`Query.Target` is the case that bit us: `eshu report capture` puts whatever
follows `--endpoint` into it, so `--endpoint "/path?api_key=sk-live-..."` used
to place a live credential in a bundle stamped `public` whose own validation
passed. `SplitTargetQuery` (exported, also used by the CLI to build the request
URL) splits the query string off and `Capture` folds its parameters into
`Params`, where the ordinary redaction walk sees them as keys. Benign
parameters survive the split, because a bundle that drops the query it captured
cannot be reproduced.

`Validate` re-checks this independently (`target_query_string` in
`ValidationChecks`) rather than trusting that `Capture` produced the file — a
maintainer runs `--require-public` against bundles other people send them.

If you add another free-string field to `Bundle`, ask what happens when a user
pastes a credential into it. The share-safe gate will not catch it.

`--include-payloads` (private-triage only) attaches raw citation excerpts and
resolved fact envelopes verbatim under `Bundle.Payloads`; every other section
of the bundle is still redacted and re-validated. `reportbundle.Validate`
excludes only the `Payloads` section from the share-safe walk in that case.

## Verification

Focused package gate:

```bash
cd go && go test ./internal/reportbundle -count=1
```

The redaction canary (`TestCapture_RedactionCanary` in `capture_test.go`) is
the acceptance-criterion test: it plants sensitive-shaped key names with
unique sentinel values in query params, response data, and (private-triage
only) fact payloads, then asserts the serialized default bundle's BYTES never
contain a sentinel value — not merely that the keys were renamed.

CLI integration is covered by:

```bash
cd go && go test ./cmd/eshu -run 'TestReportCapture|TestReportValidate' -count=1
```
