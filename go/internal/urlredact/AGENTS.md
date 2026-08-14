# AGENTS.md — go/internal/urlredact guidance for LLM assistants

## Read first

1. `go/internal/urlredact/README.md` — the drift this package ends, why the walk
   is an index walk, and why the shared corpus lives in a production package.
2. `go/internal/urlredact/doc.go` — the godoc contract.
3. `go/cmd/eshu/first_run_evidence.go` — `redactEndpoint`, the first consumer.
   It calls `Query` and adds the userinfo and fragment rules around it.
4. `go/internal/reportbundle/redact_free_text.go` — `redactFreeText`, the second
   consumer, and the one that is easy to forget because it reads only
   `PairSeparators`, not `Query`.

## Invariants this package enforces

- **One definition of the boundary.** `PairSeparators` is the only place `"?&;"`
  appears. A second copy anywhere is the exact defect this package was created
  to remove: `reportbundle` had all three characters, `cmd/eshu` had only `&`,
  and three credentials shipped verbatim through the difference. If a walk needs
  a different boundary, that is a change to this constant plus a corpus row
  proving what the difference does — not a local copy.

  There were **three** copies, not two. `reportbundle` defines both
  `queryPairSeparators` and `freeTextValueTerminators`, and the second is the
  one `redactFreeText` reads. Wiring only the first left `reportbundle`
  completely green when `PairSeparators` was broken. Before claiming a
  consumer is wired, change `PairSeparators` to `"&"` and check that every
  consumer's tests go red — a green consumer under that probe is an unwired
  consumer, not a passing one.

- **No value heuristics.** This package looks at the left half of a pair and
  nothing else. Adding an entropy check or a secret-pattern list here would make
  the README's narrow, checkable claim into "we scan for secrets", which nobody
  can falsify. The name predicate is `collector.IsSensitiveKeyName`; ask it,
  never restate it.

- **Original bytes survive.** `Query` writes back the separator it read. Do not
  reach for `strings.Split`/`Join`, `url.ParseQuery`, or `url.Values.Encode`:
  the first collapses `;` into `&`, and the second pair sorts parameters and
  re-encodes the ones it kept. Both rewrite an endpoint an operator has to match
  against their own config.

- **Never invent a value.** `?token` and `?token=` come back untouched. Writing
  `token=redacted` reports a credential that was never in the URL, and the
  hand-rolled split exists precisely so the output describes what arrived.

- **`Query` is idempotent.** An evidence report can be re-rendered from a saved
  envelope, so the redacted form has to be a fixed point. Both corpus tests
  assert a second pass changes nothing. A marker that is empty, or that stops
  matching the name predicate as a value, breaks this.

## Common changes and how to scope them

- **Add a separator** → edit `PairSeparators`, then add a corpus row using it.
  Both walks are driven through the corpus, so the row tells you immediately
  whether the two now agree. Do not add the character to one walk only.
- **Change what counts as a sensitive name** → that is `sensitiveQueryPattern`
  in `sdk/go/collector/validation.go`, not here. Every walk in the repo reads
  it through `collector.IsSensitiveKeyName` for that reason.
- **Add a redaction rule to the endpoint URL** (a new URL part, a new fallback)
  → that belongs in `redactEndpoint` in `go/cmd/eshu/first_run_evidence.go`,
  which owns URL assembly. Keep this package to the query string. Watch that
  file's line count: it sits close to the repo's 500-line cap, and
  `go/cmd/eshu` has a digest-pinned file-count ledger entry
  (`scripts/lib/dirgate-grandfather.tsv`), so a new file there fails the gate.
  Extract into this package or another importable one instead.
- **Record a row one walk cannot handle** → set `EndpointKeepsSecret` or
  `FreeTextKeepsSecret` with the reason. Do not delete the row. The check
  asserts the reason in both directions, so an exemption that stops being true
  goes red.

## Failure modes and how to debug

- Symptom: a corpus row fails in `reportbundle` but passes in `cmd/eshu` (or the
  reverse) → cause: the two walks disagree on that row. That is the defect this
  package exists to surface; fix the walk, or record the divergence with its
  reason if it is genuinely unfixable.
- Symptom: "now removes the credential, so the recorded exemption is stale" →
  cause: a walk got better and a `*KeepsSecret` reason is now false. Clear the
  reason and update `WantFreeText`/`WantEndpoint` for that row.
- Symptom: an idempotency failure → cause: the marker passed to `Query` is empty
  or the walk rewrote a separator. Check the marker first; `cmd/eshu` passes
  `evidenceRedactedMarker`, deliberately a bare word with no separator in it.

## Anti-patterns specific to this package

- **Copying `PairSeparators` "for readability".** That is the bug.
- **Deleting a corpus row because it is awkward.** The awkward rows are the ones
  that were never varied before, which is how the drift survived review.
- **Importing a CLI or service package from here.** This package's only
  dependency beyond the standard library is `sdk/go/collector`, for the name
  predicate. `redactEndpoint`'s `mcpsetup.RedactToken` fallback stays on the
  `cmd/eshu` side for that reason.

## What NOT to change without saying so out loud

- The `?token` / `?token=` pass-through. It is a deliberate honesty rule about
  what the output claims, not an oversight, and a reviewer who has not read the
  README will read it as a missed case.
- The exact `WantEndpoint` / `WantFreeText` strings. They are what two walks
  currently produce; changing one without changing the walk means the corpus
  stopped describing the code.
