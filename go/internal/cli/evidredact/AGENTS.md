# AGENTS.md — go/internal/cli/evidredact guidance for LLM assistants

## Read first

1. `go/internal/cli/evidredact/README.md` — the four carriers, why the span split
   exists, and the cost this package deliberately accepts.
2. `go/internal/cli/evidredact/doc.go` — the godoc contract.
3. `go/internal/urlredact/AGENTS.md` — the shared boundary, the percent-escape
   depth rule, and the differential corpus. Every rule there binds this package.
4. `go/cmd/eshu/first_run_evidence.go` — the consumer. It keeps thin wrappers
   (`redactEndpoint`, `scrubEvidenceText`) because `hosted_onboard.go` also
   redacts an endpoint and because the shared corpus is driven through
   `redactEndpoint` by that package's differential test.

## Invariants this package enforces

- **The stages run on DISJOINT spans.** Absolute URLs go to `Endpoint`; the text
  between them gets the raw-target substitution and then `urlredact.FreeText`.
  Neither half may be widened to cover the other, and both directions were
  measured:

  Substitution over a URL corrupts it. A repo target of `//` rewrote
  `https://h/z` to `https:.../h/z`, and `/h/z` did the same to the same URL.
  Nothing leaked; the operator just got an endpoint they could not match against
  their own config, and the URL stage no longer recognized it as a URL.

  The free-text walk over a URL destroys it a different way. It ends a value at a
  pair separator and replaces the pair **including its name**, so
  `https://h/x?token=…&repo=demo` would come back `https://h/x?[redacted]&repo=demo`
  instead of the readable `?token=redacted`. `Endpoint` already removes the
  userinfo, every credential-named query value and the whole fragment, so the URL
  span loses nothing by being handled separately.

  `TestTextKeepsAURLReadableWhenItsCredentialIsInAQueryParameter` and
  `TestTextDoesNotCorruptAURLThatContainsARawTarget` pin the two directions.

- **Inside a span, substitution runs BEFORE the pair walk.** Measured on a raw
  target containing a space, which is an ordinary path on a developer machine:
  substituting first turns `token=/Users/bob/my repos/demo` into `token=.../demo`
  and the pair walk removes it whole; scanning first ends the value at the space
  and strands `repos/demo` where the substitution can no longer find it. The
  reverse interference cannot happen — a substitution writes `.../base`, which
  holds no `=` and no `:`.

- **Every free-form field is scanned. There is no safe-field list.** Recovery
  steps and next commands are package literals today, so scanning them is pure
  cost — `export ESHU_API_KEY=<token>` comes out `export [redacted]`. Scan them
  anyway. A provenance list has to be re-decided every time a field changes where
  its bytes come from, and a field whose provenance quietly changed is the exact
  shape of the leak this package closes. If you are about to add an exemption,
  you are adding the next carrier axis.

- **No value heuristics, ever.** Nothing here looks at what a value contains.
  Adding an entropy check or a secret-pattern list would turn a narrow checkable
  claim into "we scan for secrets", which nobody can falsify. The name predicate
  is `collector.IsSensitiveKeyName`, reached through `urlredact`; ask it, never
  restate it.

- **Everything is a fixed point.** `eshu first-run report` re-renders an artifact
  from a saved envelope. A change that makes a second pass find something new is
  a defect, not an improvement. `TestScrubbedTextIsAFixedPoint` drives the whole
  package corpus, and the driver-level re-emit is in the PR evidence.

- **Wrappers in `cmd/eshu` are not dead code.** `go/cmd/eshu` is digest-pinned in
  `scripts/lib/dirgate-grandfather.tsv`, so no new non-test file may be added
  there. The logic lives here; the wrappers stay so the existing corpus and
  differential tests keep driving the real path.

## Common changes and how to scope them

- **Add a URL part to redact** (a new fallback, a new component) → `Endpoint` in
  `endpoint.go`. Keep the query-string half in `urlredact.Query`.
- **Change where a `key=value` pair ends** → that is `urlredact.PairSeparators`
  and the corpus rows beside it, never a local copy here. A second copy of `?&;`
  is the defect `urlredact` was created to remove.
- **Change what counts as a sensitive NAME** → `sensitiveQueryPattern` in
  `sdk/go/collector/validation.go`. Not here, and not by passing a predicate:
  `urlredact.FreeText` ORs a caller's extra names with the shared predicate
  precisely so a caller cannot narrow it.
- **Add a new scrubbed field to the report** → call `Text`, `Texts`, `Truth` or
  `Value` from `buildFirstRunEvidence`. Do not add a rendering-time scrub; text
  is cleaned on the way INTO the report model, so a new renderer cannot reopen a
  carrier.

## Failure modes and how to debug

- Symptom: an artifact shows an endpoint the operator cannot match against their
  config (`https:.../h/z`) → cause: the raw-target substitution reached a URL
  span. Check the span split in `Text`, not `substituteRawTargets`.
- Symptom: `first-run report` run twice on the same envelope gives two different
  artifacts → cause: something in the chain is not a fixed point. Check the
  markers first: `Marker` must carry no separator and `urlredact.FreeTextMarker`
  must carry no `=`, `:` or `%`.
- Symptom: a credential still in the artifact → identify the CARRIER before
  changing anything. URL query, userinfo, fragment, path segment, bare pair, or
  bare secret with no key. Three of those are closed, one is a stated limit, and
  one is the span-split residue. Fixing the wrong one is how this took four
  rounds.

## Anti-patterns specific to this package

- **Redacting at the rendering surface.** The Markdown renderer and the JSON
  renderer would each need their own copy, and a third renderer would ship
  unredacted. Clean on the way in.
- **A fourth copy of the pair walk.** There were three copies of `?&;` in this
  repository and they drifted. If you need the walk, import it.
- **Testing the scrub only through `first-run report`.** That is exactly how the
  bare-pair carrier survived review: no test drove the scrub in isolation, so
  nobody wrote the input that had no URL in it.
