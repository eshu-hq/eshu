# Media Transcript Documentation

## Purpose

`collector/document/media` turns reviewed local transcript output for media
documentation artifacts into source-neutral documentation document and section
facts. It exists so audio/video transcript facts can be tested and read back
without enabling a hosted collector path or treating spoken text as operational
truth. Emitted metadata identifies transcript sections with the incident-media
`transcript_chunk` source class so later correlation code can preserve
provenance without promoting transcript text to graph truth.

## Ownership boundary

This package owns the post-preflight transcript fact boundary for local media
inputs. It calls `media` preflight first, invokes only an injected transcript
engine for supported inputs, redacts sensitive-looking transcript segments and
the segments that follow a sensitive keyword (see "Value segments"), and emits
`documentation_document`, timestamped `documentation_section`, and
provenance-only `documentation_entity_mention` envelopes.

It does not discover repositories, add runtime flags, create temp files, call
cloud transcription APIs, persist raw media bytes, read video frames, persist
speaker names, trust subtitles, emit graph edges, infer service or deployment
truth, or create claim candidates. Transcript mentions use the existing
`doctruth` extractor over redacted-safe section content; exact, ambiguous, and
unmatched mentions remain document evidence only. Runtime enablement still
requires sandbox, dependency, telemetry, ACL, and security review.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Engine` is the injected local transcript boundary.
- `Request` describes one media artifact revision and durable scope metadata.
- `Options` forwards media preflight limits and section text bounds.
- `Media`, `EngineResult`, and `Segment` describe bounded transcript input and
  output without source-specific types. `EngineResult.ModelName` and
  `ModelDigest` become `transcript_model` and `transcript_model_digest`
  section metadata when a model-backed engine sets them.
- `ContainsSensitiveKeyword` is the keyword check `Extract` uses to open a value
  window. An engine may use it to keep a segment it would drop; it must not
  redact on the result.
- `ErrNoSpeech` is the sentinel an engine wraps when it ran cleanly but heard
  no speech. `Extract` records it as `transcript_status=no_speech` with the
  `transcript_no_speech` warning instead of failing.
- `Extract` runs preflight, calls the engine when allowed, and returns
  ready-to-persist fact envelopes. Callers may pass known `doctruth.Entity`
  values and per-segment mention hints; this package emits mention facts only
  from non-redacted transcript sections and never passes claim hints.

## Dependencies

The package depends on `internal/collector/preflight/media` for metadata-only
media gating, `internal/doctruth` for conservative mention extraction,
`internal/facts` for documentation payload and envelope contracts, and
`internal/scope` for the documentation collector kind. It uses
`golang.org/x/text/unicode/norm` for keyword folding and only the Go standard
library otherwise.

## Telemetry

None directly. This is a default-off helper with no runtime side effects. A
future runtime integration must add bounded extraction attempt metrics, warning
class counts, elapsed time, source bytes, duration, codec/container outcomes,
segment counts, redaction counts, logs, spans, and status evidence before
enablement.

Collector Performance Evidence: `go test ./internal/collector/document/media
./internal/collector/preflight/media ./internal/doctruth -count=1` proves
transcript fact construction is bounded by media preflight limits, synthetic
transcript segments, section text bounds, and deterministic mention extraction
without adding discovery, queue, graph, or runtime work.

No-Regression Evidence: the value-segment walk (#9786) adds a small cost. It
sorts the segments, capped at 1,000 by the `whispercpp` engine, and makes two
passes over them. Baseline is `d2e483870d`, measured at `9e2cb253b2`. A
throwaway benchmark of `buildSections` on 1,000 benign synthetic segments, 5
runs of 3,000 iterations each on arm64, went from a median of 8.27 ms/op to
8.43 ms/op, about 2% more, with about 1,004 more allocations per call (24,913
to 25,917). There is no backend, queue, or row count: this is in-memory work
with no production caller, and it holds no lock and starts no worker.

No-Regression Evidence: the spoken keyword fold (#9788) adds work per
segment. Baseline is `main` at `82c9ef362e`; after is the #9788 branch with the
ASCII fold fast path, the split-segment join, and the keyword result computed
once per segment and shared with `persistedContent`. The `whispercpp` cap is
4,096 characters per segment (`defaultMaxSegmentChars`) and 1,000 segments, so
a typical 85-character segment and the capped case were both run. A throwaway
benchmark called `buildSections` on 1,000 synthetic benign segments (arm64,
two passes of 6 alternating base and branch rounds, 30 iterations each, built
from `git archive` copies of both trees). The host was shared: its load average
ran from 12 to 36 during the runs, and a few runs on either side came in
2-3 times slower, so the table gives the normal band and the median, and the
outliers are listed after it.

| Segment size | main | branch | Allocations |
| --- | --- | --- | --- |
| 85 characters | 11.9-13.5 ms, median 12.0 | 13.8-14.4 ms, median 14.0 | 26,915 to 28,914 |
| 4,096 characters | 491-562 ms, median 495 | 516-567 ms, median 531 | 26,950 to 28,951 |

That is about 2 ms more per 1,000 segments at 85 characters (about 16%) and
about 35 ms more at the cap (about 7%). A review that alternated the two trees
6 rounds of 30 iterations measured +2.7 ms (+22%) and +28 ms (+5.8%), with
non-overlapping bands, so the cost at a typical size is 15-22% and at the cap
5-7%. Outliers: base 22.7 and 17.1 ms (85 characters), 616 and 610 ms (4,096);
branch 27.6, 42.0 and 44.2 ms (85), 663, 718 and 1,370 ms (4,096). An earlier
revision that joined whole segments and folded each segment twice measured
717-921 ms on the capped case, which is why the fast path, the 24-byte join
edge, and the shared result exist. At a typical segment size the absolute cost
is a few milliseconds per transcript, and about 30 ms at the capped size, in
memory. There is no backend, queue, or row count: no production
caller exists, and the work holds no lock and starts no worker.

Collector Observability Evidence: this package emits no facts outside the
returned envelopes and has no runtime side effects. Future runtime wiring must
add collector signals for attempted media preflight and transcription,
warning classes, elapsed time, bytes inspected, media duration, codec/container
outcomes, transcript segment counts, redaction counts, and resource outcomes
before enabling media ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service,
ServiceMonitor, collector binary, runtime flag, environment-variable path, or
hosted collector route changes in this slice. Media transcripts remain callable
only through an injected engine in tests or future reviewed code.

No-Observability-Change: this package adds no worker, queue consumer, database
query, graph write, HTTP handler, MCP tool, runtime flag, hosted collector path,
metric, span, log, or status row. Existing collector, fact-store, HTTP, and MCP
readback surfaces already carry the emitted documentation facts.

No-Observability-Change: the value-segment rule (#9786) and the spoken keyword
forms (#9788) add none of these and no error text. A value section carries the existing `redacted` and
`redaction_class` metadata, which is what a future caller counts.

## Value segments

A speech engine can put a secret's value in a later segment than the word that
introduces it, so `Extract` does not judge segments only one at a time (#9786).

- A segment with a sensitive keyword (see "Keyword forms" below) opens a window
  of `valueWindowSegments` (2) segments.
  Those segments persist as `[redacted]`, with the same `redacted`,
  `redaction_class`, and warning metadata as a marker segment, and emit no
  mention facts.
- Emails, URLs, tickets, and local paths with no keyword in their text redact
  their own segment and open no window. They are values, not words that
  introduce one. A segment that has both a keyword and one of these patterns,
  such as "password alice@example.com", still opens a window, because the
  keyword alone decides that.
- The walk runs twice, and every keyword segment opens a window in both: once
  in time order, by start then end, because the `Engine` contract does not
  promise sorted output, and once in slice order, because timestamps cannot be
  trusted to place a keyword. A wrong time can still look valid and sort away
  from the value, and a duration bound does not help: the real engine returned
  an end of 6500 ms on the 5783 ms split fixture. The passes agree on
  time-sorted input. On out-of-order input the second pass only adds redaction,
  and that over-redaction is accepted policy. Section order and fallback
  section IDs stay in engine order.
- Only a segment with a letter or digit uses up the window. Blank,
  punctuation-only, and zero-width segments stay redacted inside it but leave
  it open, so "The password is", ".", "...", "swordfish." still covers the
  value. A long silence does not close it either: the window counts segments,
  not milliseconds.
- A segment with invalid timing never reaches a section and uses up no window,
  but a keyword in it still opens one: its text was spoken.
- Each keyword segment adds a full window, so stacked markers each cover their
  own value.
- A multi-word keyword that the engine splits at a space across two
  consecutive segments ("The API", then "key is"; "The pass", then "word is";
  "The access", then "key is") opens a window at the second one, and that
  segment is withheld too. The pair is the previous segment with a letter
  or digit and the current one, in each walk's own order, and counts only when
  neither holds a keyword alone, so one keyword never opens two windows. Only
  the last 24 bytes of the first segment and the first 24 of the second are
  joined, so the work is constant per pair; a cut inside a word leaves a
  placeholder letter, so a long word beside the join is kept and a cut cannot
  fake a word boundary. The join is a space, so only a rule with a space in it
  can match across the join. A split inside a word ("to", then "ken"; "cred",
  then "ential"; "pass", then "wd") opens no window, and joining without a
  space is not done because it would match ordinary English ("up" then "in").
  Splitting a keyword into three or more segments is not caught.
- The window is 2 so it covers an immediate value, plus a value the engine
  broke in two or a filler spoken before it. It stays small because every
  segment in the window loses its text.
- Any keyword opens the window, not only one that ends its segment. This is a
  deliberate deviation from a "marker-ending" rule. Through the real engine
  path (16 kHz stdin), `base.en` hears the fixture's "The password is as
  follows." as "The password in a column". Direct `whisper-cli` runs on the
  22.05 kHz file heard "as follows" as "El Falon", "Alpha alone", and "El
  Paolo". A heuristic that reads the words after a keyword fails open.
- The cost is lost speech. One keyword segment can redact two following
  segments. On valid, time-sorted input, n consecutive keyword segments can
  redact n+1 segments after the last one. With invalid or out-of-order timing
  the two passes can withhold different segments: each pass withholds at most
  2n segments that have a letter or digit for n keyword segments (every keyword
  adds two slots and each such segment uses one), so the union of the passes
  can reach 4n. So "The password was rotated." costs ordinary speech. That loss
  shows in section metadata; a persisted secret cannot be taken back.

### Keyword forms

The keyword check (#9788) folds the segment text before it matches, so the
forms a speech engine or an evasion produces fold to the same word:

- Case, Unicode compatibility forms (fullwidth letters, ligatures, no-break and
  ideographic spaces), and combining accents fold away ("pássword").
- Format characters are dropped: soft hyphen, zero-width space and joiners,
  word joiner, byte order mark, and bidirectional marks.
- A bounded set of Cyrillic, Greek, and Latin look-alikes of the letters the
  keywords use (a c d e h i k l m n o p r s t v w y) folds to Latin
  (`homoglyphs` in `spoken_keyword.go`). A capital folds through its lower-case
  form, except the ones in `capitalHomoglyphs`, whose lower-case form looks like
  a different letter: Greek capital nu (N), upsilon (Y), eta (H), and mu (M).
  Cyrillic capital en folds through its small form, which also looks like H. It
  is not the full Unicode confusables table. A sweep of every code point
  against the first #9788 revision found that each rune that folded to a keyword
  letter there still does, except Greek capital nu, which now folds to n, not
  v.
- A run of white space, dash, `_`, `.`, or `,` becomes one space, so "pass
  word", "pass-word", "pass_word", "pass.word", "API key", "P.I.N.", and "P, I,
  N" fold like their joined or spaced forms.

The folded text is matched against `spokenKeywordRules`. The boundary of each
rule is deliberate:

| Rule | Words | Boundary | Why |
| --- | --- | --- | --- |
| Substring | `password`, `passphrase`, `passcode`, `passwd`, `pwd`, `pincode`, `credential`, `api key`, `apikey`, `private key`, `privatekey`, `accesskey`, and the `*_marker` keywords | none | No ordinary word contains them; "passwords" and "credentials" are the same vocabulary. This is the original behavior. |
| Start-bounded | `pass word`, `pass phrase`, `pass code`, `a p i key` | no letter before | "compass word" and "bypass wording" stay clear. |
| Whole word | `secret`, `secrets`, `token`, `tokens`, `pin`, `p i n`, `p w d` | no letter on either side | "tokenize", "tokenizer", "pinned", "spinning", "mapping", "shipping", "betoken", "spin", "secretary", and "secretive" stay clear. |
| End-bounded | `access key`, `access keys` | no letter after | "access keyboard" stays clear. |

`secret` fails closed on purpose. "A secret garden" and "the secret of good
bread" withhold the next two segments, which shows in section metadata, while a
client secret that stays in clear text cannot be taken back.

OCR in this tree keeps the six original markers. #9793 tracks a wider OCR list.

Source names, document IDs, and mention hints keep the original substring list,
because they are labels such as `token-service` and `pin-assignments.wav`, not
speech. Only a transcript segment gets the folded rules.

Measured cost, on 206,995 sentences of four or more words from this
repository's `docs/public` and `docs/internal` Markdown, split with the regular
expression `[.!?]\s+` after dropping code fences, table rows, headings, and
HTML lines: the original list hit 290 sentences (0.14%); the folded rules hit
4,181 (2.02%). The added hits came from `token` 1,214, `credential` 1,015,
`pin` 582, `secret` 417, `tokens` 293, `secrets` 273, `pwd` 30, `api key` 30,
`private key` 17, `access key` 7, `access keys` 5, `apikey` 5, `passwd` 1,
`privatekey` 1, and `accesskey` 1 (each sentence counted once, under the first
rule in list order that hit it). The counts come from a throwaway test, not a
committed script, so the method is not pinned. A review that counted the same
files its own way got 200,218 sentences, 0.11% before and 2.00% after, and `pwd`
3 hits instead of 30; the gap in sentence count is about 3%, and the difference in
the `pwd` count is not explained by the corpus changing between runs. An
earlier review with a different sentence splitter found 244,300 sentences, 0.17%
before and 1.76% after. Treat every figure here as approximate. This corpus is
the project's own security-heavy documentation, so it is a rough guide, not a
bound for any other speech. Each hit also withholds the next two segments with
a letter or digit. The sandbox fixture sentences ("Please restart the payments
worker before noon.", "The password is swordfish.", "The password is as
follows.", "Swordfish.") match as before.

Against the real engine (`base.en`, v1.9.5, 16 kHz stdin path),
`scripts/verify-media-transcript-sandbox.sh` heard the spoken forms as "The API
key in swordfish.", "The API key is..." then "Swordfish", and "The P.I.N. in
swordfish." Every segment was withheld and no segment text reached a fact. It
also showed what the rules do not catch: "The pass <2 s silence> word is" came
back as "The past." then "Where did sword fish?", with no keyword at all.

The whisper.cpp engine drops a segment that is only a bracketed, parenthesized,
starred, or music-note annotation, such as "[BLANK_AUDIO]". A model can also put
speech inside brackets, so a "(The password is)" segment would vanish before
`Extract` could open a window for it. That was not observed from `base.en` on
synthetic speech: espeak speech at six volume levels, with and without the
espeak whisper voice, came back as plain text, or as `[BLANK_AUDIO]` when
silent. The guard stays because the drop is a fail-open path and costs one
function call. The engine keeps an annotation that holds a keyword, using the
exported `ContainsSensitiveKeyword`, and still redacts nothing; `Extract`
withholds the text and opens the window. Other annotations are still dropped,
and a dropped annotation that held a value is lost, not persisted.

Known limits:

- A value spoken before its keyword ("swordfish is the password") is not
  redacted, because the window only looks forward (#9792).
- A value spoken past the window keeps its text.
- Two self-redacting segments in a row, such as an email and a ticket, use up
  the window, so a value behind them keeps its text.
- Only the forms above are matched. Leetspeak ("p4ssword"), a homoglyph outside
  the bounded set, a misheard keyword, and unlisted secret words ("passkey",
  "login") open no window and are not redacted.
- `tokenized`, `tokenizer`, and `pins` keep their text by design.
- Characters not folded: invisible fillers such as U+3164, U+2800, and U+115F,
  the combining enclosing circle U+20DD, the middle dot, the minus sign U+2212,
  and `/` and `:` as separators. A CJK character next to `token` or `pin`
  counts as a letter, so the whole-word rule does not fire there.
- Spelling a keyword letter by letter ("p a s s w o r d") is not matched,
  except `p i n`, `p w d`, and `a p i key`.
- A keyword split across three or more segments is not caught, and a split
  inside a word ("to", then "ken") opens no window.
- A half that follows a segment with a keyword of its own is not joined, so a
  split keyword there opens no second window. "The password is the API", then
  "key is swordfish." is covered only by the first window.
- A bracketed half such as "(The API)" is dropped by the engine as an annotation
  unless it holds a keyword alone, so "(The API)", then "key is swordfish." opens
  no window.

## Gotchas / invariants

- Non-WAV media stays unsupported until a codec/container dependency review
  lands.
- Source identity fields are redacted when they contain scheme-bearing URIs,
  private URLs, hostnames, or embedded local paths. Keep raw locations out of
  document IDs, external IDs, canonical URIs, source URIs, and source record
  IDs.
- Speaker labels are never persisted as raw names. Sections may record only a
  presence marker and stable hash.
- Sensitive-looking transcript text is not persisted as content. The section
  keeps redaction metadata instead. Its `text_hash` and `excerpt_hash` are
  hashes of the `[redacted]` placeholder, because an unsalted sha256 of a short
  spoken value would confirm a guess at it. Redacted sections do not emit
  mention facts, even when the caller supplied mention hints.
- Sensitive-looking mention hints are dropped before doctruth extraction; a
  hint must not be used to bypass transcript redaction.
- Transcript text and transcript mentions remain document evidence. This
  package must not emit claim candidates, graph relationships, ownership truth,
  deployment truth, incident truth, or service truth.
- A caller must provide a transcript engine; there is no default engine or
  hosted provider fallback. In-tree backends are the out-of-process
  whisper.cpp speech-to-text engine in `whispercpp/` (#9729) and the
  deterministic DTMF decoder in `dtmf/`, kept as a fixture backend. Both are
  for explicit QA-tier injection only.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/incident-media-evidence.md`
- `go/internal/collector/preflight/media/README.md`
- `docs/public/reference/local-testing.md`
