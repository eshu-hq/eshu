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
- Redaction is scoped by PROVENANCE DOMAIN, not by arrival path. Two domains:
  - **Reporter-typed query input** — `Query.Target`, all of `Query.Params`,
    and `Response.Error.Details` (which echoes the caller's own selector back).
    These get the key-name walk PLUS the `embeddedSensitiveKey` structural
    re-parse, at any depth, via `redactQueryInput`.
  - **Server-produced evidence** — `Response.Data`, `Response.Truth`. Key-name
    walk only, via `redactValue`.
- Structural re-parsing is the DEFAULT for any new free-string field in the
  Query section. `embeddedSensitiveKey` re-parses a value that is itself shaped
  like a query string back into `key=value` pairs and asks the SDK predicate
  about those keys — it changes where key names are looked for, not what counts
  as one. Adding a Query-section field means it is scanned; exempting it
  requires a recorded reason in this file, not silence.
- `Response.Data` is the one recorded exemption, and the reason is that judging
  its string content would strip the answer under investigation — the evidence
  the bundle exists to carry. The cost is known and named in `README.md`: the
  API echoes request parameters back into `data` (`data.input` on the
  supply-chain routes, `question`/`query`/`subject` elsewhere), so a credential
  can return through the server's echo. Closing that needs an owner decision
  about which `data` subfields count as query input; do not widen `Data`
  wholesale.
- Value-CONTENT judging stays banned everywhere. Structural re-parsing asks
  "does this text contain a `key=value` pair whose KEY is sensitive". A
  substring heuristic on what a value IS ("looks like a token") is a different
  thing and must not be added to any domain.
- Never scope the redaction to one arrival path. Three review rounds each fixed
  the leak for the path a reviewer found — the target string, then its parse
  failure, then its nested values — and the same credential reappeared through
  the next path each time. `Capture` merges every query-input source and scans
  the merged result once, so a new source cannot bypass the scan by
  construction. Keep it that way; do not reintroduce a per-source pre-step.
- No user-supplied string is interpolated into an error this package returns.
  Name the field (`query.target`, `query.params.next`), never the value. These
  errors reach terminals, CI logs, and pasted bug reports — the same places the
  bundle is redacted for. Wrapping `url.ParseQuery`'s error with `%w` is
  allowed because its shapes quote at most a three-byte escape token; the
  full-egress canary is what proves that stays true.
- A sensitive-named key is REMOVED from the tree, not masked in place. Do not
  "fix" this by reintroducing an inline `"[REDACTED:key]"` marker on the same
  key — see `redact.go`'s design note; it would make every redacted bundle
  fail its own `Validate` gate.
- `Capture` MUST call `Validate` before returning and MUST return an error
  instead of a bundle that fails it. A capture tool refuses to write a bundle
  that trips its own gate; it does not ship one with a warning.
- The key-name rule cannot see string VALUES, so the share-safe gate will not
  catch a credential inside one. `Query.Target` is the worked example:
  `SplitTargetQuery` turns its query string back into keys so the ordinary walk
  applies, and `Validate` re-checks it independently.
- `Validate` mirrors the query-input walk exactly (`validateQueryInputs`). It
  runs against files other people send — hand-edited, or written by a
  third-party tool — so anything `Capture` redacts, `Validate` must reject. Do
  not widen one without the other: a bundle carrying
  `"params":{"next":"/x?api_key=..."}` passed every check this package had
  because only `Capture` had been widened.
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
  `Validate`'s checks — `TestCapture_RedactionCanary`
  (`redaction_canary_test.go`) for what reaches the serialized bundle, and the
  full-egress canary (`redaction_egress_test.go`) for what reaches ANY package
  output, errors included. A new query-input field or a new error return needs
  its own planted sentinel in the egress canary.
- Keep this package under the 500-line-per-file cap; split before a file
  approaches it.
