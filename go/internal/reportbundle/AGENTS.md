# AGENTS.md - reportbundle

## Ownership

This package owns only the pure `wrong_answer_report.v1` schema, the `Capture`
composer, the key-name redaction walk, and `Validate`. It must not open
stores, call providers, query graph backends, perform HTTP or MCP calls, or
read durable fact records itself — callers supply an already-resolved
`query.ResponseEnvelope` and any evidence/fact hydration via `CaptureInput`.

## Rules

- Redaction decides by KEY NAME, via `collector.IsSensitiveKeyName` /
  `collector.ValidateShareSafeKeys` (`sdk/go/collector`). Never add a second,
  local sensitive-key heuristic — reuse the SDK export so the redactor and the
  fail-closed gate can never disagree.
- There is exactly one place this package reads a value, and it is not a
  content scan: `embeddedSensitiveKey` (`redact.go`) re-parses a value that is
  itself shaped like a query string back into `key=value` pairs and asks the
  SDK predicate about those keys. It changes where key names are looked for,
  not what counts as one. It exists because `SplitTargetQuery` splits on the
  first `?` only, so `?next=/x?api_key=sk-live` hides a credential inside a
  benign parameter's value. Scope it to target query parameters. Do not extend
  it to response data: judging `Response.Data` string content on a substring
  match would strip the answer under investigation, which is the evidence the
  bundle exists to carry.
- A sensitive-named key is REMOVED from the tree, not masked in place. Do not
  "fix" this by reintroducing an inline `"[REDACTED:key]"` marker on the same
  key — see `redact.go`'s design note; it would make every redacted bundle
  fail its own `Validate` gate.
- `Capture` MUST call `Validate` before returning and MUST return an error
  instead of a bundle that fails it. A capture tool refuses to write a bundle
  that trips its own gate; it does not ship one with a warning.
- The key-name rule cannot see string VALUES. Any `Bundle` field holding raw
  user input as a bare string needs its own handling before capture — the
  share-safe gate will not catch a credential inside one. `Query.Target` is the
  worked example: `SplitTargetQuery` turns its query string back into keys so
  the ordinary walk applies, and `Validate` re-checks it independently. Adding
  a new free-string field means answering this question again.
- `SplitTargetQuery` returns an ERROR for a query string `net/url` cannot
  parse, and both callers refuse the target. Do not "simplify" that back into
  returning no parameters: an empty result reads to `Validate` as "nothing
  sensitive here" while the raw string, credential included, stays in
  `Query.Target`. That was the bug, not the conservative option.
- `Bundle.Payloads` is the only field allowed to carry raw excerpt bytes or
  fact payloads, and only when the caller set `CaptureInput.IncludePayloads`.
  Never populate it, or leave an `Excerpt`-carrying type, anywhere else in the
  schema.
- Add or update tests before changing the schema, the redaction walk, or
  `Validate`'s checks — especially `TestCapture_RedactionCanary`, the
  acceptance-criterion test for this package.
- Keep this package under the 500-line-per-file cap; split before a file
  approaches it.
