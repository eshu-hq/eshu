# Arbiter ruling: #9793 and #9788, fold versus edit distance for secret labels

RULING: fold-based matching stays in `document/ocr` and `document/media`; edit
distance and fuzzy matching are rejected for the label rule. The source-identity
list stays the same six entries in both packages. The two packages keep two
tested copies of their label rules. Each README describes only merged code.
PR 9817 (#9788) merges first with its planned-OCR bullets removed; #9793 follows
with this file, a fresh full review, and a sandbox rerun at its final head.

Arbiter: arbiter-eshu (Fable), 2026-11-02. Heads ruled on: OCR branch
`drive/9793-ocr-marker-words` at 3ec277d319 (no PR yet); STT branch
`drive/9788-stt-keyword-variants` at 245264177f (PR 9817, open). Issues read:
#9793, #9788. Independent verdicts read: the OCR verdict at 407388ec38 (blocked
on F7) and the STT verdict at 245264177f (clean). Every figure below was
re-measured by the arbiter.

## Verified facts

1. The OCR rule folds each rune once (`label_markers.go:85-99`): `0` to o, `3`
   to e, `4` and `@` to a, `5` and `$` to s, `1` and `l` to i, after width-fold
   and lowercase (`:63-73`). Four families set the boundary per word
   (`:34-49`): contains, endsWith, exact, phrases. `containsMarkerWord`
   (`:139-218`) first runs the old six-entry substring check
   (`containsSourceMarker`, `:223-246`), so it is a superset of the rule it
   replaced.
2. The STT rule folds NFKD, drops combining and format characters, maps a
   bounded homoglyph table, and collapses separators to one space
   (`spoken_keyword.go:179-263`), then matches `spokenKeywordRules`
   (`:50-78`) with per-rule before/after letter bounds (`:268-282`).
3. Source identities use a separate six-entry list in both packages:
   `identityMarkers` (`ocr/sanitize.go:34-36`) and `legacyKeywords`
   (`media/sanitize.go:26-28`). The lists are identical. They feed
   `safeIdentity` and `isSafeRelativeSource`, which hash or drop the document
   ID, external ID, source URI, and record ID (`ocr/extract.go:113-114,259`;
   `media/extract.go:127-128,364`) and mention hints (`media/extract.go:252`).
4. Tests at both heads pass: `go test -count=1 ./internal/collector/document/ocr/`
   239 passed, exit 0 (includes `TestContainsMarkerWordIsLinear`);
   `go test -count=1 ./internal/collector/document/media/` 219 passed, exit 0.
5. False-positive cost, re-measured on `git ls-files 'docs/public/*.md'
   'docs/internal/*.md'` (1,734 files) with a scratch test on a `git archive`
   of each head (no tree edits):
   - OCR, per non-blank line: 262,304 lines; old rule 470 (0.18 percent); new
     rule 6,697 (2.55 percent); lines the old rule flagged and the new one
     missed: 0.
   - STT, per sentence (fences, tables, headings, HTML dropped; split on
     `[.!?]\s+` per line; four or more words): 200,343 sentences; old 228
     (0.11 percent); new 4,013 (2.00 percent); old-only 0. Joining lines before
     splitting gives 89,080 sentences and 4.13 percent, so the sentence method
     moves the ratio; the per-line ratio is the one to cite.
6. Form probes at the OCR head (true = label): `Passw0rd:`, `P@ssword`,
   `S3cret`, `PIN:`, `PINs`, `pln`, `accesskey`, `ACCESSKEY:`, `AccessKey`,
   `API Key: value`, `Pin the base image`. False: `500 PLN`, `spin`, `pinned`,
   `secretary`, `tokenize`, `taken`, `pan`, `Pasword`, `PW:`, `A.P.I. key`,
   Cyrillic `раssword`, `secretkey`, `tokenid`, `aPI Key`, `$ecret`.
7. Form probes at the STT head: true for `The API key is abc123`,
   `private key: abc123`, `The secret is abc123`, `pwd`, `AccessKey`,
   `accesskey`, `P.I.N.`, `A.P.I. key`, Cyrillic `раssword`; false for `pins`,
   `access keyboard`, `passw0rd`, `tokenize`, `spinning`, `compass word`.
   `pin-assignments.wav` and `token-service` are true as spoken text and false
   as identities, which is the intended split.
8. The media README (`media/README.md:238-254`) describes "the planned" OCR
   list and says at `:251-254` that it "reads an all-lower `accesskey` as one
   word and does not match it". At OCR head 3ec277d319 `accesskey` is in the
   contains family (`label_markers.go:40`) and matches. The OCR README
   (`ocr/README.md:150-153`) now says the STT rule is not merged and does not
   describe it.
9. PR 9817 body line 28 says the README records "the planned differences from
   the wider OCR list tracked by #9793".

## Decisions

1. Fold, not edit distance. This is a privacy control, so the question is where
   each approach fails open and what each costs when it fires wrongly.
   - Short-word collisions make edit distance unusable at distance one for
     `pin`, `pwd`, `token`, and `key`: `pan`, `pen`, `pit`, `pie`, `spin` are
     one edit from `pin`; `taken` from `token`; `pod`, `pad`, `pwn` from `pwd`.
     A per-word threshold that excludes them also excludes every misread of
     them, so distance adds nothing on the words where it is cheapest and adds
     English collisions on the words where it is widest.
   - The fold can only create a match when the folded word spells a marker.
     Digits and symbols fold only to {o, e, a, s, i}, and no marker word is
     spelled from that set alone, so a digit string never becomes a label.
     Measured, the digit and symbol folds added 5 lines in 262,000; the rise to
     2.55 percent comes from the new words `token`, `pin`, `secret`,
     `credential`, not from folding.
   - Determinism and explainability: every hit traces to one family and one
     word, and a reviewer can reproduce it by reading one table. A distance hit
     needs the metric, the threshold, and the candidate list to explain, and the
     threshold is a second list to maintain.
   - Linear time: one pass, one table lookup per rune, fixed-size stack buffers,
     and the symbol-run early exit (`label_markers.go:275-294`).
     `TestContainsMarkerWordIsLinear` passed at head. Edit distance against k
     markers is O(n times k times word length) with a per-word DP and is harder
     to bound against adversarial input.
   - The residual misses are not an argument for distance. `Passvvord` is two
     edits from `password`; `A.P.I. key` is three or more from `api key`;
     homoglyphs are a code-point problem. Only `Pasword` is one edit away, and
     the probe of degraded renders found the label came back verbatim or was
     lost whole.
   Decision: no edit-distance, n-gram, or fuzzy matcher in either package. If a
   letter-for-letter misread class ever needs cover, add it as an enumerated
   confusion-pair fold (`rn` to m, `vv` to w) measured on the same corpus before
   it lands, under the Prove-The-Theory-First rule.
2. The source-identity list stays narrow and identical. Document IDs, source
   URIs, and record IDs are names chosen by a person, not text read by an
   engine, so they carry no misreads to fold. Widening them to the label rule
   would hash the identity of `token-rotation.png` and `pin-assignments.wav` and
   lose provenance for ordinary documents while gaining no secret cover: a
   secret inside a name is a path, URL, or email, which `matchesSensitivePattern`
   and `isUnsafeSourcePath` already catch. Condition: a change to one list lands
   in both packages in the same PR, and `TestSourceIdentityKeepsTheNarrowMarkerList`
   (OCR) keeps a twin in media.
3. Two tested copies, not a shared package. The folds differ by input physics:
   OCR needs digit and symbol folds and camel splitting; speech needs NFKD,
   homoglyph, and spelled-letter folds, and the engine never writes digits for
   letters. The boundaries differ on purpose (`pwd` substring in STT, whole word
   in OCR; `pins` matched in OCR, clear in STT). A union rule would carry each
   package's false positives into the other for no gain, and `document/AGENTS.md`
   keeps the namespace documentation-only with implementation in leaves.
   Revisit only if a third consumer appears; record that on #8879. Condition:
   the merged-code truth rule applies to both READMEs. The media README violates
   it today (fact 8). It must reduce `:238-254` to one statement that OCR in the
   merged tree keeps the six markers and that #9793 tracks a wider list, with no
   per-form bullets. The PR that merges second owns the differences table,
   written against both merged trees and verified with a probe like the one in
   facts 6 and 7.
4. Residual misses are accepted as documented limits, with two notes. `Pass:` is
   rejected as a marker (`pass` is an ordinary word). `PW:` is not required for
   merge; it may be added later as an exact-family word only after a corpus
   measurement (it collides with abbreviations such as "Public Works").
   `secretkey` and `tokenid` are identifier forms whose underscore and camel
   spellings already match; a fused all-lowercase label on a printed form is
   rare, and both are named in Limits. `$ecret` stays a separator case by
   design. OCR homoglyphs stay a limit: a Latin-trained tesseract emits Latin.
   The table-column residual goes on #9382 as the issue requires; it is not a
   merge blocker. The prose cost ("Pin the base image" hides two regions) is
   accepted: each hit costs a bounded window of 2.
5. Conditions before merge.
   - Order: PR 9817 (#9788) first. Its trees do not overlap with #9793 except
     `CHANGELOG.md` and `docs/public/observability/telemetry-coverage.md`
     (row 4a conflicts).
   - PR 9817: apply decision 3 to `media/README.md:238-254` and edit PR body
     line 28 to match. That changes PR claims, so the review receipt is void;
     re-capture per the promotion order (scoped re-review at minimum, since the
     edit is README and body text only). Then `make pre-push`, verify the
     receipt, push.
   - #9793: commit this file at
     `docs/internal/evidence/9793-fold-vs-edit-distance-arbiter-ruling.md`;
     after 9817 merges, replace `ocr/README.md:150-153` with the differences
     table against merged STT code; rerun `scripts/prove-ocr-sandbox.sh` at the
     final head and record its exit code; record the table-column residual on
     #9382; run a fresh full `eshu-code-review` at the final head; then the
     promotion order.
   - Both: no serialization or worker change is involved; none is accepted.
     `identityMarkers` and `legacyKeywords` stay equal. No edit-distance code.

## What I will not do and why

- I will not accept edit distance as a "safer" fallback. It widens the
  fail-open surface on short words and makes the hit unexplainable.
- I will not accept the media README's planned-OCR bullets on the ground that
  they say "planned": fact 8 shows they are already wrong against the branch
  they describe. Docs describe merged code.
- I will not require a shared package to satisfy "one shared rule or two tested
  copies that agree" in #9788; the acceptance text allows two copies, and the
  agreement is on the source-identity list (decision 2) and the documented
  differences (decision 3).

## NOT_CHECKED

- `scripts/prove-ocr-sandbox.sh` and `scripts/verify-media-transcript-sandbox.sh`
  at the current heads (the STT verdict reports exit 0 and canary_in_facts=false;
  the lane reports exit 0 for OCR at 3ec277d319).
- The benchmarks in either README.
- The 46 OCR mutants and the sandbox "15 distinct texts, 2 flagged" count.
- `make pre-push`, the live gate, and CI on PR 9817.
- Whether the table-column residual is already recorded on #9382.
