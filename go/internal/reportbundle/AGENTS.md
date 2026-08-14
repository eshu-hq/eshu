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
- Redaction is scoped by PROVENANCE DOMAIN, not by arrival path. Three domains:
  - **Reporter-typed query input** — `Query.Target`, all of `Query.Params`,
    and `Response.Error.Details` (which echoes the caller's own selector back).
    These get the key-name walk PLUS the `embeddedSensitiveKey` structural
    re-parse, at any depth, via `redactQueryInput`.
  - **Free text** — `ReporterNote`, `Response.Error.Message`, and
    `Response.Error.CorrelationID`, via `redactFreeText`. Scanned line by line
    for a sensitive-named key beside `=` or `:`. It is a separate function from
    `embeddedSensitiveKey` on purpose: that one splits on query separators and
    skips any candidate key containing whitespace, which is right for a parsed
    parameter and wrong for prose. A server-COMPOSED string is in this domain,
    not out of it: `Message` is built by interpolating the caller's own selector
    (`query/service_workload_resolution.go:39`), and `CorrelationID` is the
    caller's own `X-Correlation-ID` header when one was sent
    (`query/documentation.go:470`).
  - **Server-produced evidence** — `Response.Data`, `Response.Truth`. Key-name
    walk only, via `redactValue`.
- The note scan covers BOTH the `key=value` and the `key: value` header form. Do
  not drop the header half as redundant: a pasted `curl` carries the same token
  twice, once in `-H 'Authorization: …'` and once in `?api_key=…`, so covering
  only one strips a copy and ships a copy while `redaction.rules` claims a
  redaction happened. The header removal runs to END OF LINE because an HTTP
  header value may contain spaces; over-removal is the side to err on.
- The note scan REPLACES the matched span and keeps the rest of the note. Do not
  "simplify" it to dropping the whole field: the note is the reporter's own
  description and a repro with the secret cut out is still a repro. Do not go the
  other way either and mask in place — the marker must contain no `=` and no `:`,
  or the cleaned note is re-found on the next pass and `Capture`, which runs
  `Validate` over its own output, refuses to emit any bundle it ever redacted.
  `TestRedactReporterNote` asserts that fixed point per case.
- `reporter_note` was NOT an overlooked field. The plan
  (`docs/internal/design/4595-wrong-answer-report-capture-plan.md:203`)
  annotates it "key-name walked like everything else", which a bare string
  cannot be — it has no key names, and `reporter_note` matches nothing in the
  sensitive pattern. Treat any "covered by the general rule" annotation on a
  scalar field as unproven until a test plants a sentinel in it.
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
- `embeddedSensitiveKey` scans a value twice: as it arrived, and once
  percent-decoded. `url.ParseQuery` decodes the target's query string upstream
  and nothing decodes `--params`, so without the second pass the same bytes got
  two verdicts depending on which flag carried them — the exact asymmetry this
  package exists to remove. The decode runs EXACTLY ONE layer and never feeds
  its output back in; do not turn it into a loop, and do not move it to an
  arrival point.
- The free-text walk reads the SAME one layer, through
  `urlredact.DecodedByteAt` / `DecodedEscapeBefore` / `Decode`, so `%3D` is an
  `=`, `%3A` is a `:` and `api%5Fkey` is `api_key`. It had none of that while
  `embeddedSensitiveKey` did, which is how
  the encoded spelling of an already-fixed credential
  (`?redirect_uri=%2Fcb%3Faccess_token%3D…`) stayed green in `query.target` and
  leaked from `reporter_note` and `response.error.message`. Use the shared
  reader; do not add a second decoder. The one-layer rule is even stricter here
  than for the detector: this walk EMITS what it scanned and `Validate` scans
  the output again, so a deeper unwrap makes `Capture` reject its own bundle.
- Whether `%26`/`%3F`/`%3B` END a value depends on the DEPTH of the pair, and
  `noteTerminatorDepth` is where that is decided. A pair joined by a literal `=`
  was typed at the surface, so an escape inside its value is part of the
  credential: `?token=aa%26bb` is a token of `aa&bb`, and reading the `%26` as a
  terminator cut it and left `bb` in the note. Only a pair whose own `=` arrived
  encoded ends at an encoded terminator. Do not special-case one separator here
  — `%26`, `%3B`, `%3F`, `%20`, `%22` and `%27` all truncated the same way, and
  the escaped members of `freeTextValueTerminators` that are NOT in
  `urlredact.PairSeparators` cannot be covered by a corpus row, so they need a
  test in `redaction_boundary_test.go` of their own.
- Any test constant carrying a credential must exist in more than one spelling.
  `symmetryBytes` was a single unencoded string driving all three placements, so
  one fix closed the axis for every placement at once and a placement the fix
  missed still looked covered. `symmetrySpellings` is a grid now; keep it one.
- No user-supplied string is interpolated into an error this package returns.
  Name the field (`query.target`, `query.params.next`), never the value. A
  parameter NAME is reporter-typed too and can itself be a `key=value` pair, so
  every dynamic path segment goes through `safePathSegment` before it reaches a
  message. A merely sensitive-NAMED key (`api_key`) is still named — that is a
  field name, and naming it is what makes the rejection actionable. These
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
  output, errors included. A new reporter-typed field or a new error return needs
  its own planted sentinel in the egress canary, and
  `TestReporterInputPlacementSymmetry` needs a placement row: the same bytes in
  two reporter-typed fields must get the same verdict, which is exactly what
  `reporter_note` failed before this scan existed.
- Every canary sentinel in the egress TABLE is planted in a VALUE. A credential
  can also arrive as a KEY — `url.ParseQuery` percent-decodes names, so
  `?api_key%3Dsk-live-X` yields a parameter named `api_key=sk-live-X`.
  `Validate`'s own messages no longer repeat such a key
  (`TestValidateDoesNotEchoAUserSuppliedKeyName`), but two routes are still
  open: `Capture` copies the raw name into `Redaction.Rules`, and the share-safe
  gate in `sdk/go/collector` quotes the key it rejected. Measured and reproduced
  in the header comment of `redaction_egress_test.go`. Plant a key sentinel in
  the table as part of the change that closes those; adding one before it only
  makes the suite red.
- Keep this package under the 500-line-per-file cap; split before a file
  approaches it.
