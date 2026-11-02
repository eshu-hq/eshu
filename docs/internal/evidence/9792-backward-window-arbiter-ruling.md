# Arbiter ruling, #9792: backward value window size (arbiter-eshu, Fable, 2026-11-02)

RULING: N=2. `valueWindowBackSegments = 2`, mirroring the forward window and
its rationale; whole-segment, fail-closed, both walks, as the lane built it.
N=1 is rejected because base.en really does put a spoken value two lexical
segments before its keyword. N=3 is rejected because every finite N has a
leaking phrasing one segment further out, and the third step buys the least
cover for the same cost as the second.

Ruled on: worktree `drive/9792-stt-value-before` at `0e4aafe77e` (origin/main)
with the lane's uncommitted diff (+63/-11 in `sanitize.go`,
`extract_value_segment_test.go`, `sanitize_keyword_test.go`,
`whispercpp/extract_redaction_test.go`, new `extract_value_before_test.go`).
Issues read: #9792, #9786, #9788. Precedent read: 9793-ruling.md,
ruling-9729-split.md, ruling-9728-pinning.md, ruling-9728-F3.md,
verdict9786b.md.

## Verified facts

1. Forward window rationale, from source (`sanitize.go:16-20` on origin/main):
   "One covers the immediate value. The second covers a value the engine broke
   in two or a filler spoken before it." The deeper reason (`sanitize.go:92-98`,
   README "Value segments"): the words around a keyword are the least reliable
   part of a transcript (base.en hears "The password is as follows." as "The
   password in a column"), so no reading of the segment tells a value still to
   come from one already spoken. That argument is direction-blind: if the rule
   cannot read "is" after "password", it cannot read "is the" before it either.
   A backward window smaller than the forward one would need a measured
   speech prior that values follow their label; #9792's "usually follow" is an
   unmeasured prior, and the lane and I have no corpus of spoken secrets.
2. Lane's fix shape verified in the diff: `backWindow` (`sanitize.go` new
   `:160-193`) keeps the last `valueWindowBackSegments` segments with a letter
   or digit plus the blank/punctuation segments among them, in walk order;
   invalid-timing segments are never added (they never reach a section);
   `mark` fires on `opens || straddles` before `add(i)`, so a straddle's first
   half (previously left in clear) is now withheld too. The union over the
   time-order and slice-order walks only adds redaction, as the forward rule
   already accepts. `go test -count=1 ./internal/collector/document/media/`
   at the lane head: 237 passed, exit 0; `go vet` exit 0.
3. Complexity: the window is not a fixed ring of N slots; blanks between
   lexical segments stay until N lexical segments after them are added. Work is
   still linear: a segment is marked at most N+1 times per walk (it leaves the
   window when the (N+1)-th lexical segment after it arrives, and only lexical
   keyword/straddle segments mark). Slots are bounded by the engine's
   `MaxSegments` (1,000). The lane's brief said "ring of <=N segments"; the
   README must state the real bound (decision 4).
4. Corpus cost re-measured independently (throwaway test in a `git archive`
   copy of the lane tree, no tree edits; corpus = `git ls-files
   'docs/public/*.md' 'docs/internal/*.md'`, 1,740 files; fences, table rows,
   headings, HTML dropped; split on `[.!?]\s+` per line; four or more words;
   each file's sentences fed to `valueSegments` as sequential segments):
   201,226 sentences, 4,051 keyword segments (2.01%).
   withheld: N=0 10,950 (5.44%); N=1 13,686 (6.80%); N=2 16,225 (8.06%);
   N=3 18,616 (9.25%). Walk time 0.79-0.84 s per pass on this host for every N.
   The lane's three figures reproduce exactly. Each step of N costs about
   +1.2 to +1.4 points on this security-dense corpus; on ordinary speech the
   cost scales with the keyword rate, which is far below 2%.
5. Real engine probe (bounded, 15 min budget, met): espeak-ng 1.51.1-r1 in a
   throwaway `alpine:3.21@sha256:48b0309c…` container synthesized seven
   value-before fixtures; the pinned `whisper-cli` from the local
   `eshu-media-transcript-sandbox:local` image (`ggml-base.en.bin`) ran them
   with the engine's exact argv from `whispercpp/engine.go:173-187`
   (`-of - -ojf -np -ng -l en -t 4 -bs 1 -bo 1 -tp 0 -nf`), 22.05 kHz file
   input (the engine path resamples to 16 kHz over stdin; boundaries may
   differ there, see NOT_CHECKED). Segments (ms from-to, text):
   - a `Swordfish. <break 3s/> That is the password.`:
     0-1000 "swordfish" | 3700-4900 "That is the password". Value 1 back.
   - b `Swordfish. <break 2s/> Write it down. <break 2s/> That is the password.`:
     "swordfish" | "right it out" | "that is the password". Value 2 back.
     N=1 leaks the canary; N=2 covers it.
   - e `Sword <break 1.5s/> fish. <break 2s/> That is the password.`:
     "sword" | "fish" | "that is the password". Value broken in two, 2 back.
     N=1 leaks "sword"; N=2 covers both halves. This is the backward twin of
     the forward rationale's "value the engine broke in two".
   - g `The value is X 7 Q 9. Please write it down. Keep it safe. That is my API key.`
     (no SSML breaks): four segments at the sentence boundaries, value 3 back.
     N=2 leaks "The value is x7Q9."; N=3 would cover it. base.en splits long
     utterances at sentence boundaries without any pause.
   - c (three sentences, 5.84 s, no breaks), d (`4-7-1-9. That is my pin.`),
     f (`swordfish, that is the password.`): one segment each, same-segment
     rule already redacts.
   The probe's threshold (one fixture with the value at least two lexical
   segments before its keyword) is met twice, so the 2-back case is a real
   output shape, not a fake-engine construction.
6. Sandbox-fixture cost (the issue's acceptance wording): the only benign
   segment in the committed fixtures is `speech.wav` segment 0 ("Please restart
   the payments worker before noon."); every N>=1 withholds it, so the fixture
   cost is 1 of 1 benign segment and does not separate N=1 from N=2. The lane
   already changed `whispercpp/extract_redaction_test.go:39-46` to expect it.
   The whispercpp README table row "Redaction via `media.Extract` | 2 sections,
   1 redacted" (`whispercpp/README.md:248`) becomes wrong at the final head.

## Decisions

1. N=2. Grounds, in priority order: (a) accuracy/fail-closed: facts 5b and 5e
   show values two back in real base.en output, and the forward rationale's two
   cases (value broken in two; one filler) occur backward as well; (b) the
   rule's own premise (fact 1) gives no principled reason for the two
   directions to differ; (c) cost: +2.62 points over forward-only on a corpus
   with a 2.01% keyword rate, with no runtime change (fact 4); every hit is
   visible in section metadata, a persisted secret is not recoverable.
2. N=3 and larger are rejected for merge. Fact 5g shows a 3-back shape exists,
   and so will a 4-back one; the window is a bound, not a guarantee. The third
   step costs the same +1.2 points as the second but covers neither of the
   two named failure modes. The 3-back case is recorded as a documented limit
   (decision 4) and on the #9382 gate-2 note, not fixed. Revisit only with a
   measured corpus of spoken secrets, under Prove-The-Theory-First.
3. No serialization, worker, or engine-package change is involved; none is
   accepted. The fix stays in `media.Extract`/`sanitize.go` per #9786 and
   ruling-9729-split.md.
4. Conditions the lane must meet before review receipt:
   - Tests (keep; verify RED first): `TestExtractRedactsValueSegmentsBeforeKeyword`
     must fail on origin/main for the "value, then a clause that ends in the
     keyword" case (`swordfish.`, `is the password`), the issue's named
     regression. Record the RED command and exit code in the PR body. Keep the
     "value three segments back keeps its text" case as the N pin, and add one
     case for a value broken in two before the keyword (`sword`, `fish`,
     `that is the password` -> all withheld), the fact-5e shape.
   - Linearity: either extend an existing linear-time test to `valueSegments`
     with a 1,000-segment adversarial input (long blank runs between keyword
     segments) or state the N+1 bound in a `backWindow` doc comment and in the
     README; the comment "keeps no more than it needs" is not a bound.
   - Benchmark: rerun the README's `buildSections` 1,000-segment benchmark at
     the final head and replace the "No-Regression Evidence" figures
     (`README.md:75-82`) with before/after at the new base; same method, same
     host, median of 5 runs.
   - README "Value segments": one bullet for the backward window: N=2 lexical
     segments before every keyword or straddle segment, both walks, blanks do
     not use it up, invalid timing never added, the straddle's first half now
     withheld, the N+1 marks-per-segment bound, and the cost line: n keyword
     segments can now withhold up to 2n before plus 2n after per walk. Update
     the forward-window cost bullet accordingly. Replace the "Known limits"
     bullet `README.md:287-288` with: a value more than two lexical segments
     before its keyword keeps its text (fact 5g wording, cite this ruling).
     Record the corpus figures (fact 4) next to the existing ones, with the
     same "approximate, security-dense corpus" caveat.
   - whispercpp README: fix `:248` to "2 sections, 2 redacted" at the final
     head, and add the new fixture row.
   - Sandbox proof at the final head (required by this ruling, in the spirit of
     #9786's third criterion): add one fixture to
     `scripts/lib/media-transcript-sandbox/Dockerfile`, the fact-5b text
     `'Swordfish. <break time="2s"/> Write it down. <break time="2s"/> That is the password.'`
     (`speech-before-split.wav`), copy it into the proof stage, export it in
     `scripts/verify-media-transcript-sandbox.sh`, and add
     `TestSandboxValueBeforeKeywordIsRedacted` modeled on
     `TestSandboxCanaryInOwnSegmentIsRedacted`: read the one transcript Extract
     consumed, locate the canary segment and the keyword segment, require the
     canary index to be lower than the keyword index with at least one lexical
     segment between them (log the distance), require all sections redacted and
     the canary absent from every envelope, fail with counts only. If the
     engine path (16 kHz stdin) merges the segments differently, tune the break
     lengths until the 2-back shape appears, and record the final transcript
     shape as counts. Run `scripts/verify-media-transcript-sandbox.sh` at the
     final head; paste the EVIDENCE lines and exit code in the PR body.
   - CHANGELOG entry under Unreleased, `#9792` without a closing keyword unless
     the issue is meant to close on merge (it is: all three acceptance boxes
     are met by this ruling plus the lane's change, so `Closes #9792` is
     allowed once the ruling file is committed).
   - Commit this ruling at
     `docs/internal/evidence/9792-backward-window-arbiter-ruling.md` and cite
     it from the README limit bullet and the PR body.
   - Then: fresh full `eshu-code-review`, `make pre-push`, receipt verify, push.
5. Gate-2 note on #9382 for `document/media`: the window is two segments each
   way; a value spoken more than two lexical segments before its keyword, or
   past two after it, keeps its text (facts 5g and the existing forward limit).

## What I will not do and why

- I will not choose N=1 on the "usually follows" prior. It is unmeasured, the
  rule's own premise says the surrounding words cannot be read, and the real
  engine produced the 2-back shape on the first attempt.
- I will not accept N=3 to cover fact 5g. Each step buys one more phrasing at
  the same cost; without a spoken-secret corpus there is no stopping rule but
  the forward window's, and that rule says two.
- I will not accept the "ring of <=N segments" wording. The structure is
  linear by amortization, not by a fixed ring; the README must say what is true.
- I will not accept the sandbox-fixture cost alone as the measurement; it
  cannot separate the candidates (fact 6). The corpus figures are the record.

## NOT_CHECKED

- Segment boundaries through the engine path (16 kHz stdin resample); my probe
  used the engine argv on the 22.05 kHz file. The lane's sandbox run settles it.
- The first `-nt` run of fixture `a` printed only "swordfish."; the JSON run
  of the same file gave two segments. Not investigated; the sandbox test
  compares against the transcript Extract consumed, so it cannot pass on a
  dropped keyword.
- The lane's 0.65 s walk figure (mine 0.79-0.84 s, other load on this host).
- `make pre-push`, `eshu-code-review`, CI, and the benchmark rerun at the
  final head.
