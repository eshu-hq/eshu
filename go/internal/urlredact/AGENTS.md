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

- **One definition of the DEPTH, too.** `escape.go` owns reading a separator
  through percent-encoding, and every walk goes through it — `Query`,
  `redactFreeText`, and `reportbundle.embeddedSensitiveKey`. Sharing the
  separator BYTES was not enough on its own: an HTTP client writes the structure
  encoded, so `?redirect_uri=%2Fcb%3Faccess_token%3D…` carries no bare `?` and no
  bare `=`, and it went past both walks while the literal spelling of the same
  URL was pinned by a corpus row.

  **EXACTLY ONE layer, never a loop.** `%253F` asks for the text `%3F`, so a
  second layer describes a request no server received. The harder reason is that
  `redactFreeText` emits what it scanned and `Capture` runs `Validate` over that
  output: a deeper unwrap hands back a string one layer shallower than it
  arrived, the next pass finds something new, and `Capture` rejects its own
  bundle — the reporter then gets nothing. `Decode` is `url.PathUnescape`, not
  `url.QueryUnescape`, because a `+` is a plus sign in prose and turning it into
  a space can only lose matches. It is also the **only** decoder here: a second
  one is the same defect as a second separator constant, and `redactPair` had
  `url.QueryUnescape` sitting thirty lines from this rule.

- **An escape is a separator only at its own pair's depth.** Reading every
  escape as the byte it stands for is too much, and it introduced a partial leak
  worse than the whole one it fixed. `?token=AAAA%26BBBB` joins name to value
  with a LITERAL `=`, so the reporter typed it at the surface and its `%26` is
  two characters of a value that reads `AAAA&BBBB`; splitting there shipped
  `BBBB`. `?a=1%26token%3D…%26repo%3Ddemo` joins with `%3D`, so its structure is
  one layer down and `%26` is the separator there.

  `BoundaryDepth` / `IndexBoundary` carry this, and each walk picks the depth
  from the width its own `=` was found at. At the LITERAL depth do not add a
  per-separator exception: `%26`, `%3B`, `%3F`, `%20`, `%22` and `%27` all broke
  identically and one rule covers every one of them.

  **One layer down it is not one rule, and claiming it was is what leaked
  next.** Only `PairSeparators` is structure there. `reportbundle` also ends a
  value at whitespace, a quote or a backtick, and an encoder writes `%20`
  precisely because the space is INSIDE a value — the escaped spelling is
  evidence of content, not of a boundary. Counting the prose half a layer down
  cut the credential out of `?redirect_uri=%2Fx%3Faccess_token%3D…%20TAIL` and
  shipped `TAIL`. `IndexBoundaryBySpelling(s, literal, escaped)` is where a walk
  says which set it means in which spelling; `IndexBoundary` is the
  same-set-both-ways shorthand for a caller that has no prose in its set.

  The accepted cost is over-removal — `?token=x%26repo=demo` loses `repo=demo` —
  and the residue that cannot be fixed from inside a string is a value genuinely
  containing `&` inside an already-encoded pair, where `%26` spells both.

- **Depth is one axis; POSITION is another.** The depth question asks whether
  the name is credential-shaped and whether its `=` was literal. It must not
  also ask whether the pair carries a value: that is `redactPair`'s question,
  it lives in `sensitivePairSeparator`, and merging the two shipped a whole
  credential. When the escape is the value's FIRST byte the pending text is a
  bare `token=`, the merged function answered "no pair here", the escape was
  honoured as a separator, and `?token=%26<credential>` came back untouched —
  on every separator and every credential-shaped name. Keep
  `sensitiveNamedPairSeparator` (depth) and `sensitivePairSeparator` (has a
  value) apart.

- **Two walks deciding the same thing independently is how this drifts.**
  `DifferentialCases` compares the walks to EACH OTHER over a generated
  cross-product, because a corpus row is written by somebody who already knows
  the case exists. Both walks passed every corpus row while disagreeing on 72 of
  594 inputs. When you add an axis to the walk, add it to the cross-product —
  not only a row to the corpus.

  **A differential is silent when both walks are wrong the same way.** That is
  why `CheckRemoval` exists beside `CheckAgreement`: 378 of the 594 rows declare
  a fragment inside a credential value and BOTH walks must remove it. Break the
  shared name predicate and every row still agrees — only the removal half goes
  red. A fragment the depth model puts outside the value goes in `Outside`, not
  `Removable` and never in `WalksDisagree`: both walks keeping it is agreement,
  so recording it as a disagreement trips the stale-exemption branch.

  Keep the oracle hand-written. `credentialShaped`, `encoded` and
  `pairSeparator` are literals in the axis tables on purpose — an oracle that
  called `collector.IsSensitiveKeyName` or read `PairSeparators` would
  reclassify every row the moment the code it checks moves, and the table would
  go green asserting nothing.

  **Seven totals are pinned, answering four questions.** How many rows: `594`,
  and `378` carrying a removable fragment. How many fragments: `492` removable,
  `300` outside. Which fragment: `TailSentinel` declared removable on `114` rows
  and outside on `84`. How much room the table has: `198` inputs hold
  `TailSentinel`, which with the vocabulary check fixes declaring capacity at
  `594 + 198`.

  Each level exists because a weakening walked past the one above it, and both
  were measured, not imagined. 114 rows carry two removable fragments, so
  demoting the second to `Outside` holds both ROW totals exactly while the
  removal assertions drop 492 → 378. Declaring `Sentinel` twice in the "inside
  the value" position instead of `Sentinel` plus `TailSentinel` holds all four
  COUNTS exactly, and `TailSentinel` is then declared by nothing. Both
  weakenings leave the whole package green except for the pin that catches them,
  and both drop the both-walks-wrong mutation from 36 red subtests to 18 — the
  half `TailSentinel` exists to catch, since it is the only fragment sitting
  after the escape.

  Change an axis and all seven move; the self-consistency test names the new
  numbers, and every place citing them has to move too.

- **What is pinned, what is assumed, and what a further level would need.**
  Five rounds of review each found the same hole one level up — guards, then row
  counts, then fragment counts, then fragment identity, then identity spread
  across rows — and the coverage lost was the same half every time, the
  partial-leak case. So do not read the list below as "this is closed". Read it
  as the current boundary.

  **Pinned.** Seven literals. The two identity counters count **rows**. No row
  may declare a fragment twice. A fragment must BE `Sentinel` or `TailSentinel`
  — containment is `strings.Contains`, which admits any substring, so without
  the vocabulary check a row buys capacity with a proper prefix of a sentinel.
  That was the fifth level and it was measured: drop the head-of-value
  `Sentinel` from all 114 credential inside rows, pay the count back with a
  prefix on the 114 credential opening rows, and every literal holds while 114
  assertions move off the rows that carried them.

  With those, the budget is arithmetic. Every input holds `Sentinel` and 198
  hold `TailSentinel`, so declaring capacity is `594 + 198 = 792` and the table
  declares exactly `492 + 300 = 792`. Saturated, so no row can carry a fragment
  another row gave up. The `198` is pinned as well, because capacity is `594`
  plus that number and anything above it is slack.

  **Free, and deliberately so.** Which rows take which classification. That is
  not the ledger's job: a reclassification puts the model at odds with what the
  walks do, and the walks are what the driver runs. Measured — declaring an
  outside fragment removable turns 36 differential subtests red.

  **Assumed, and not pinned on purpose.** The argument needs every `Outside`
  fragment to be one the walks genuinely keep, so there is nothing to borrow
  against. That holds today and stays unpinned: asserting that an `Outside`
  fragment survives would freeze the over-removal trade this package leaves
  free, which is the one behaviour here nobody wants nailed down. If
  over-removal ever widens far enough that both walks remove a fragment declared
  `Outside`, that fragment becomes borrowable.

  **What a further level would need.** Slack in the declaring budget. Capacity
  is `594 + `(rows whose input holds `TailSentinel`), and the table saturates it
  only while that second number stays `198`. Adding an axis, a third sentinel,
  or a fourth position moves it — the pins will say so, and the arithmetic has
  to be redone rather than assumed to carry over. Anyone widening the generator
  should check saturation before trusting the numbers below it.

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

- **Add a separator** → edit `PairSeparators`, then add corpus rows on BOTH
  axes, because each one hid a leak the other looked like it covered.

  Depth, three rows: one spelling the separator literally, one spelling it `%XX`
  as the pair boundary, and one putting `%XX` inside the value of a pair written
  with a literal `=`, where it must NOT bound anything.

  Position, three more: the `%XX` at the **start**, the **middle** and the
  **end** of that literal value. "Middle" is the one everybody writes, and for
  two rounds it was the only one — every row in this corpus put at least one
  value byte in front of the escape, so `?token=%26<credential>` had no row at
  all and shipped whole. `TestBoundaryCasesOpenAValueWithAnEscape` covers the
  start position; nothing covers the end but the row you write.

  Both walks are driven through the corpus, so the rows tell you immediately
  whether the two now agree. `TestBoundaryCasesCoverTheEncodedSpelling` fails
  without the encoded-boundary row, and nothing fails without the value-content
  row — which is exactly how the truncation bug reached review. Do not add the
  character to one walk only.
- **Add a boundary that is not a separator** (a new prose delimiter for the
  free-text walk) → it belongs in `freeTextValueTerminators`, NOT in
  `PairSeparators`, and it stays literal-only at both depths. Pass it as the
  `literal` argument of `IndexBoundaryBySpelling` and leave `escaped` to the
  separators. A corpus row cannot reach it — it is not a pair boundary — so it
  needs a row in `reportbundle/redaction_boundary_test.go` at BOTH depths.
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
- **A coverage guard no corpus edit can redden.** Both meta-guards here shipped
  in that state. One accepted a row carrying no credential; the other used an
  unanchored escape pattern that matched ordinary text after an uppercase
  escape. Before trusting a new guard, delete the row it exists to require and
  watch it go red. Ask `BoundaryCase.ProvesRemoval` rather than `Secret != ""`.
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
