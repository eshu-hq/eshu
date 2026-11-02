# Arbiter ruling: #9799, OCR redaction of a secret that spans more lines than the window

RULING: the secret-run rule in `secret_runs.go` is accepted as designed, with
its four thresholds (16, one stray space per 16, two tokens before the tail,
40 characters with three classes and no space for a seed) and the unanchored
seed. No threshold changes. The remaining leak classes are documented limits,
not merge blockers, because nothing calls OCR in production and the measured
cost of closing each one is larger than its reach. #9799 closes on the merge of
this branch once the conditions in section 5 are met, with the table-column
rule staying on #9382.

Arbiter: arbiter-eshu (Fable), 2026-11-02. Head ruled on: worktree
`drive/9799-ocr-multiline` at fe1c13a077 (12 commits on base 0e4aafe77e;
`origin/main` is at 7f6501b7e3, so a rebase is pending). Read: #9799 (no
comments), #9782, #9793, #9382, the #9793 ruling (fold not edit distance,
fail-closed, linear time, measured false-positive cost), the lane's evidence
note `docs/internal/evidence/9799-ocr-multiline-secret-runs.md`, the README
Limits, and the diff against base (13 files, +996 -5). Every figure below was
re-measured by the arbiter on `git archive` copies under `~/.cache/arb-9799`
(`GOTMPDIR` inside it); the worktree was not written.

## 1. Verified facts

1. RED on base. `extract_multiline_test.go` copied onto a copy of 0e4aafe77e
   and run with `go test -count=1 -run TestExtractWithholdsASecretThatSpansManyLines`:
   0 passed, 10 failed. The leak counts match the evidence table line for
   line: RSA 24 of 26, OpenSSH 10 of 12, encrypted 18 of 18, token under label
   1 of 3, token with caption 5 of 6, bearer 3 of 5, armorless 12 of 12,
   column 3 of 5, sandbox body 9 of 9.
2. GREEN at head. `go test -count=1 -race ./internal/collector/document/ocr/`:
   518 passed; `go vet` exit 0; `gofmt -l` empty. The 9 layout subtests and
   `TestExtendSecretRunsIsLinear` pass.
3. Rule shape (`secret_runs.go:64-101`). One pass in engine order after the
   window and row passes (`sanitize.go:120`), values only move from false to
   true (`:90`), four clauses: chain (`:82`), tail (`:84`), symbol (`:72-76`),
   seed with a backward walk over run tokens (`:86-88`, `:183-194`). The walk
   stops at any withheld region, so each region is visited a bounded number
   of times. No edit distance, no regex, no sort.
4. Mutants. 16 planted on a copy of head, each run against the package tests:
   `minRunTokenLen` 15 and 17, `minSeedTokenLen` 39 and 41,
   `minTokensBeforeTail` 1 and 3, `denseSpaceShare` 15 and 17, seed without
   the upper, lower, or digit class, seed charset with a space, run token
   without the digit/`+/=` requirement, spaced token chaining after any
   withheld region, tail with no one-region limit: all 15 KILLED.
   `values[i] = withheld` in place of the or-merge SURVIVED and is equivalent:
   `withheld` starts as `values[i]` (`:78`) and only rises. So the lane's
   "one equivalent survivor" is two, both harmless.
5. False-positive cost, re-measured at Extract level (window, run, marker, and
   pattern lines) over `docs/public` and `docs/internal` `*.md` of the base
   tree, each file one page, each non-blank line one region: 1,740 files,
   263,378 lines; base 19,803 withheld, head 19,983, added 180, removed 0.
   Exactly the lane's figures. Seeds in the corpus: 1
   (`ESHU_DR_COMPOSE_PROJECT=eshu-pr6634-final-rebuild`). The 180 split as 39
   no-space lines (paths with line numbers, file names in backticks), 102 lines
   with a stray space that chain or fall in the tail slot (`ENV=value \`
   continuation lines, `-> path` rename tables, `ok  pkg 28.907s` results), and
   39 symbol-only lines (36 are ```` ``` ```` fences). The evidence note's
   sentence "The 180 are lines of 16 or more characters with a digit and no
   space" is wrong on the split and must be corrected (condition 5.3).
6. Rejected variant, measured by the arbiter: a seed that tolerates one stray
   space (dense by `denseLen`, 40 non-space characters, same classes and
   charset) makes 60 corpus lines seeds and withholds 20,097 (+114 over head).
   The 60 are `go test ... -run TestX -count=1` commands, `export ESHU_...=...`
   lines, `POST /api/v0/...` routes, and `CREATE INDEX ...` statements. This
   confirms the lane's "allowing a space in a seed made 60 shell-command lines
   seeds" and is the reason the seed stays space-free (decision 2.4).
7. Rejected variant, bounded count: a lowercase-only seed (to catch a cropped
   hex token) would hit 30 bare 40+ hex lines and chain into the 676 corpus
   lines that carry a 64-hex digest. Rejected (decision 2.4).
8. New layouts probed at head (none in the lane's table):
   - PGP armored message (no marker word in the armor): 3 body lines and the
     `=3Kp9` CRC line withheld by seed, chain, tail; both armor lines and the
     prose after kept.
   - PGP private key block: body and CRC withheld; `-----END PGP PRIVATE KEY
     BLOCK-----` is a marker and hides the 2 lines after it (the #9819 window
     cost, not new).
   - AWS pair with labels on separate lines: all withheld (windows).
   - AWS pair with no marker word anywhere (`Deploy user keys`, `AKIA...` 20
     characters, 40-character secret): the secret is a seed and the walk back
     takes the `AKIA` line; `Region us-east-1` kept.
   - JWT over 3 lines with no label: seed, chain, tail (`_adQssw5c`) withheld;
     prose before and after kept. Same under `Authorization: Bearer`.
   - Public certificate PEM: 5 body lines withheld by seed and chain,
     `-----END CERTIFICATE-----` and the prose after kept. Over-redaction of a
     public body is accepted: a certificate body has no documentation value and
     a reader cannot tell a public from a private body by shape.
   - Lowercase hex digests, UUIDs, git SHA lines, Kubernetes pod names, image
     tags: kept (no capital, or too short). URL list: withheld by the existing
     scheme pattern, not by this rule.
   - Base64 PNG data line under a caption: withheld as a seed, and the one
     short line after it (`Done`) falls in the tail slot.
   - Labelled 64-column body with k stray spaces in every line, random base64
     (no marker word in any line): 0 of 5 leak for k = 1..4; 3 of 5 leak at
     k = 5. One space per 16 non-space characters is exactly the boundary.
   - Cropped (label-less) body with one stray space in every line: 5 of 5 leak.
     With the third line clean, the seed walks back and 0 leak.
   - Wrapped token of three 12-character lines under `Token:`: third line
     leaks (under 16).
   - Lowercase 64-hex secret after an exhausted window: withheld by chain.
     Cropped with no label: leaks (no capital, so no seed).
   - `TestExtractWithholdsASecretThatSpansManyLines2` and
     `ESHU_DR_COMPOSE_PROJECT=eshu-pr6634-final-rebuild` on their own line:
     each is a seed and hides the one short line after it.
9. Performance. One run each at `-benchtime=300x` on a host at load 17-28:
   base LabelHeavy 2.92 ms, NoLabels 4.44 ms; head 3.19 ms and 4.58 ms; allocs
   identical (12,665 and 12,907). Within noise on this host; the lane's
   seven-run medians (5.55 to 5.52 ms, 3.68 to 3.79 ms) are the figures of
   record. `TestExtendSecretRunsIsLinear` passes at 200,000 lines.
10. Real tesseract. `bash scripts/prove-ocr-sandbox.sh --platform linux/arm64`
    from the scratch copy of fe1c13a077: exit 0, `PASS - OCR sandbox proof on
    linux/arm64`, `key-body.png` sections 14, redacted 12, `body_regions=9
    body_regions_in_clear=0`, `canary_leak` false for every fixture,
    `form-columns.png` redacted 4. Matches the lane.
11. Drift and rebase. `secret_runs.go` and `sanitize.go` have no commit after
    aec48436cc, the commit the measurements and the sandbox run cite, so the
    numbers describe the code at head. `origin/main` moved on
    `ocr/AGENTS.md`, `ocr/README.md`, `ocr/extract_test.go`,
    `tesseract/sandbox_proof_test.go` (#9780 made WebP `completed`), and
    `tesseract/execwrite_test.go`. The lane touches the first two and the
    fourth, so conflicts are likely and the rebase is row 4b.
12. Line caps. `ocr/README.md` is 490 lines at head and main adds 3; the
    conditions below add bullets. The 500-line gate will trip without a split.
    Other new files: 194, 281, 162, 82 lines. The `ocr` directory holds 23 Go
    files.
13. The new fixture body is nine synthetic base64 lines of a size no real RSA
    or EC key has; `liveKeyBody` compares what tesseract read, not the drawn
    text, and refuses a fixture that yields fewer than 6 body regions.

## 2. Decisions

1. `minRunTokenLen = 16`. Accepted. A PEM line is 64 or 70 characters, an
   OpenSSH line 70, a wrapped token line 30 or more; an ordinary word of 16
   characters with a digit and no space is rare, and the chain clause still
   needs a withheld region before it. Mutants at 15 and 17 are killed, and the
   cost of the chain on the corpus is bounded by fact 5. A shorter floor would
   admit words like `v1.2.3-rc.4-arm64` after any label; a longer one would
   drop base64url lines of 32 (the six-line layout). Lines under 16 stay a
   documented limit.
2. `denseSpaceShare = 16` (one stray space per 16 non-space characters), only
   inside a run. Accepted. Tesseract put one space in 2 of 9 real lines; the
   rule tolerates up to 4 per 64-column line (fact 8) and prose at one space
   per 5 or 6 characters never passes. Allowing a spaced token after a
   non-token region made the `- \`ESHU_CHECK=false\`` bullet chain (the lane's
   +577 variant); the final rule's +180 is 0.068 percent and over half of it
   is fences and continuation lines inside already-withheld blocks.
3. `minTokensBeforeTail = 2`, one region. Accepted. At 1 the real
   `form-columns.png` fixture lost `platform` under a withheld value (the
   lane's sandbox run); at 3 a two-line body loses its tail. The cost is one
   short line after a run or a seed, and it is named in Limits.
4. Seed: 40 or more characters of `A-Za-z0-9+/=_.-`, a capital, a lowercase
   letter, a digit, no whitespace; counts as two tokens; walks back over run
   tokens. Accepted, including the unanchored design. Reasoning:
   - It is the only clause that acts with nothing before it, and it is what
     makes the real fixture pass: tesseract dropped the `BEGIN` line, so no
     label opened a window. Without it a cropped or armor-less body leaks whole
     (12 of 12 in the RED run).
   - Its reach is measured: 1 line in 263,378 on this corpus, which is dense
     with commands and identifiers. Random base64 of 40 characters misses a
     digit with probability about (54/64)^40, near 0.1 percent, and misses a
     case class far less often, so the three-class test costs almost no recall
     on real bodies while it excludes paths, hex digests, upper-case variable
     names, and prose.
   - The two loosenings that would close the known residuals are rejected on
     measurement: a one-space seed costs 60 command lines (fact 6); a
     lowercase seed costs every bare digest and chains into 676 digest lines
     (fact 7). Both trade a common documentation shape for a rare layout, and
     the #9793 ruling's standard applies: a hit must be explainable by one
     clause and one threshold, and the cost must be measured before it lands.
   - Fail-closed assessment: the rule never un-withholds (fact 3), so it can
     only move the system toward withholding. It is not fully fail-closed on
     recall; the gaps in decision 3 below are the honest list, and all of them
     need a label-less secret plus a second adverse condition (every line
     misread with a space, letters-only or lowercase-only charset, or lines
     under 40).
5. Residuals. None must be fixed before merge. All must be documented
   precisely (section 5). The accepted list:
   - A label-less body whose every line holds a stray space leaks whole
     (fact 8). With a label it is covered up to 4 spaces per 64 characters.
   - A label-less token or body of lines under 40 characters, or of letters
     only, or of lowercase only (hex), opens no run.
   - A wrapped line under 16 characters, or with no digit and none of `+/=`,
     ends a run; the rest leaks until a seed.
   - A table column (#9382, unchanged from #9793).
   - Cost: a 40+ character identifier or assignment with a capital, a
     lowercase letter, and a digit (`TestExtract...Lines2`, `ESHU_X=...`) is
     withheld with the one short line after it; a long path or `ENV=value`
     line after a withheld region joins the run; a fence or `|` inside a run
     is withheld. Measured at +180 lines, 0.068 percent, on the docs corpus.
6. Serialization, worker counts, and queue behavior are not involved; none is
   accepted. The STT twin is unchanged and needs no change: a transcript has
   no lines.

## 3. What #9799 needs to close, and what stays open

- "Failing test row for a PEM block and for a token wrapped across three
  lines": met (fact 1, RED 9 of 9 on base; fact 2, GREEN at head).
- "Decide the rule with an arbiter ruling file that records the measured
  false-positive rate": this file, committed per condition 5.4. Rate: +180 of
  263,378 lines (0.068 percent), 1 seed; rejected variants +577, +114, and
  the hex-seed count in fact 7.
- "Rerun `scripts/prove-ocr-sandbox.sh` with a multi-line fixture and show no
  canary in facts": met at fe1c13a077 on arm64 (fact 10). Must be rerun at the
  final rebased head because `sandbox_proof_test.go` changed on main
  (condition 5.1).
- "Update the OCR README Limits": partly met; conditions 5.2 fill the gaps.
- Stays open elsewhere: the table-column rule on #9382 (gate-2 record, as the
  #9793 ruling already placed it); the residual list above goes on #9382 as the
  issue's own text requires ("documented and accepted on #9382 with the
  measured cost"). Nothing else stays open on #9799, so the PR may close it.

## 4. What I will not do and why

- I will not loosen the seed to accept a stray space or a lowercase-only
  charset. Both were measured (facts 6 and 7) and both hide common
  documentation lines to cover a layout that needs two adverse conditions at
  once.
- I will not require the amd64 sandbox for merge. The rule is pure Go over
  region text and does not depend on the architecture; the amd64 tesseract
  pins are unchanged by this branch; `liveKeyBody` compares what tesseract
  read, so a different misread on amd64 cannot hide a leak, and the
  `minKeyBodyRegions` floor refuses a fixture that collapses. Record it as
  NOT_RUN in the PR and run it when the #9382 gate-2 evidence is assembled.
- I will not accept the evidence note's description of the 180 as written
  (fact 5); a derivable figure that is wrong is a P2 in the same edit.
- I will not treat the surviving mutant as a gap; it is equivalent (fact 4).

## 5. Conditions for the lane and the PR

1. Rebase onto current `origin/main` (7f6501b7e3 or later). Expect conflicts in
   `ocr/README.md`, `ocr/AGENTS.md`, and `tesseract/sandbox_proof_test.go`
   (keep main's WebP-`completed` assertion and the `liveKeyBody` call). This
   is row 4b: scoped re-review at least, and the sandbox proof rerun on arm64
   at the final head with the exit code and the `key-body.png` evidence line
   quoted in the PR.
2. README Limits, before the review: (a) a label-less body whose every line
   holds a stray space leaks whole, and with a label the rule tolerates up to
   4 stray spaces per 64-character line; (b) a lowercase-only or letters-only
   token with no label opens no run (a 64-hex secret after an exhausted window
   is still chained); (c) the seed cost class: a 40+ character identifier or
   assignment with a capital, a lowercase letter, and a digit, and the one
   short line after it; (d) a fence or symbol line inside a run is withheld.
   Keep the README under 500 lines after the rebase and these bullets; it is
   at 490 now, so split before it approaches the limit (for example, move the
   redaction rule sections to a sibling Markdown file the README links).
3. Evidence note: replace the "180 are ... no space" sentence with the split
   in fact 5 (39 no-space, 102 with a stray space, 39 symbol-only); add the
   one-space seed variant (+60 seeds, 20,097 withheld, +114) and the hex-seed
   count as rejected variants; change the STATUS line to point at the ruling
   file.
4. Commit this ruling at
   `docs/internal/evidence/9799-multiline-arbiter-ruling.md` and link it from
   the evidence note and the README paragraph on #9799. Run
   `mkdocs build --strict --config-file docs/mkdocs.yml` and
   `git diff --check`.
5. Tests: add Extract-level rows for the bare AWS pair (20-character token
   before a 40-character seed, no marker word anywhere) and for a JWT over
   three lines with no label, both of which pass at head; add
   `ESHU_DR_COMPOSE_PROJECT=eshu-pr6634-final-rebuild` to
   `TestSecretTokenShapes` as `seed: true` so the accepted cost class is
   pinned. No threshold changes, so no new RED is expected; say so in the PR.
6. CHANGELOG: the entry at head is accurate; add the ruling file to it.
7. Record the residual list of decision 5 and the measured cost on #9382 in a
   comment, and reference this ruling. The PR may use `Closes #9799`.
8. Promotion: fresh full `eshu-code-review` at the final head, receipt,
   `make pre-push` once by the orchestrator, verify, push. No edits between the
   verified receipt and the push.

## NOT_CHECKED

- amd64 sandbox run (ruled not required for merge).
- `make pre-push`, the registry static gates, and CI on the eventual PR.
- The lane's full mutant set beyond the 16 I planted.
- The seven-run benchmark medians (one noisy run each here, allocs equal).
- Whether the table-column residual is already recorded on #9382.
