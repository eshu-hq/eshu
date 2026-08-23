# AGENTS — internal/cigates

Scoped rules for editing the CI gate registry core. Load `golang-engineering`
and `eshu-diagnostic-rigor`.

## Invariants

- **Load is the only entry point for YAML.** Never parse the YAML outside
  `Load`. Add new fields to `registryFile` / `gateFile` and map them in `Load`.
- **Select is a pure function.** It must not touch git, the filesystem, or any
  external service, so `ci-gates select --paths-from` is reproducible from its
  inputs alone. `Validate` is the one exception, and only for trigger
  resolution: it already stats scripts, workflows, and literal triggers, and
  since #6159 it also asks git for the tracked path set that glob triggers
  resolve against (`loadTrackedPaths` in `globtrigger.go` — the only
  `exec.Command` in this package's production code; the test fixtures run git
  too). Do not widen that seam; anything else
  needing git belongs at the CLI boundary in `cmd/ci-gates`.
- **A trigger that matches nothing is an error, never a warning.** A literal
  trigger is stat-checked (#6055); a glob trigger must select at least one
  tracked path, plus the directories tracked files imply (#6159). There is no
  waiver field and no warn-only mode: a trigger matching zero paths can never
  select its gate, so the gate reads as wired for a surface it no longer
  guards. If the tracked path set cannot be read, that is an error too, not a
  skip — an unverifiable trigger must not read as present. Resolve globs
  against the TRACKED set, never a filesystem walk: a walk lets a build
  artifact satisfy a trigger CI can never fire on, and makes the verdict
  depend on the developer's tree.
- **The tracked path universe is enumerated once per `Validate` call.** ~500
  glob triggers against ~22k paths is ~11M pattern comparisons; re-enumerating
  or re-splitting per trigger is the shape `checkTriggerPathsExist` already
  had to have a comment written about. `matchesAny` reuses `matchSegments`
  behind a first-segment index rather than calling `MatchGlob` per path, and
  the two tests guarding that are not interchangeable:
  `TestTrackedPaths_MatchesAnyAgreesWithMatchGlob` keeps the two matchers in
  lockstep on VERDICTS — extend it when either side gains a guard clause — and
  `TestTrackedPaths_MatchesAnyConsultsTheFirstSegmentIndex` keeps the index
  itself. Only the second one notices `matchesAny` collapsing back into the
  per-path scan, because both forms answer identically and differ only in
  cost. It works by handing `matchesAny` a deliberately desynchronised
  universe; keep that, or the design has no guard again.
- **`loadTrackedPaths` runs git under `gitTreeEnv`, never a plain inherit.**
  `git -C <repoRoot>` does not decide which repository git reads: an ambient
  `GIT_DIR` overrides it and the gate then PASSES against another checkout's
  tree. `GIT_INDEX_FILE` is kept on purpose — a pre-commit hook exports it to
  name the pending index the hook should be validating, so scrubbing it makes
  the gate judge the tree on disk instead. Do not "simplify" this back to
  `os.Environ()`, and do not "finish the scrub" by adding `GIT_INDEX_FILE` to
  the map; `TestLoadTrackedPaths_HonoursThePendingIndexAHookNames` reds if you
  do. Note a hook exports GIT_DIR too in a **linked worktree** (measured six
  ways, including through pre-commit 4.6.2 — see `gitTreeEnv`'s comment), so
  dropping it is not a no-op on the hook path; it is safe because git
  rediscovers the same gitdir from `repoRoot`, which was verified against the
  real gate under the full hook pair.
- **Validate accumulates errors.** Never return early from `Validate`; collect
  all integrity errors in a single pass so a single run surfaces every broken
  reference.
- **MatchGlob has no external dependencies.** The doublestar matcher in
  `glob.go` must remain self-contained. Do not import a glob library.
- **Enums are closed sets.** `validCategories`, `tierOrder`, and
  `validRequirements` are the authoritative sets. Adding a new value requires
  updating both the constant and the map, plus a table test in the relevant
  `_test.go`.
- **Files stay under 500 lines.** If any file approaches the cap, split into a
  new file before committing.
- **`pathfilter.go`'s `checkPathFilterCoverage` is registry-vs-CI-filter only,
  called from `DriftCheck` in `drift.go`.** It cross-checks each gate's
  literal (non-glob) triggers against its CI workflow's `dorny/paths-filter`
  glob block. A gate's filter key resolves two ways: via an `append_gate` call
  in a matrix-dispatch workflow (`static-contract-gates.yml`), or via a job
  whose `if:` is gated on a paths-filter output — `needs.<job>.outputs.<key>`
  — which is the shape `test.yml`, `security-scan.yml`, and
  `mcp-schema-drift.yml` use (#5546). The if-gated form binds BOTH halves of
  that reference: the producer job must be the job hosting the dorny step, and
  the comparison must be `== 'true'`. Matching the output key alone wrongly
  resolves a job gated on a different job's output, and reads
  `== 'false'` — which selects the job when paths did NOT change — as positive
  selection (#5546 review). It skips rather than guesses when the mapping is
  ambiguous: an unparseable workflow, a workflow with no dorny step, a `ci.job`
  that resolves neither way, a job whose `if:` names two different filter
  outputs, or a glob-form trigger.
- **Duplicate if-gated job display names are ambiguous too, and are reported.**
  Two jobs sharing a `name:` but resolving to different filter keys write the
  same map entry, and Go randomises job-map iteration, so which key survives
  varies run to run. `ifGatedFilterKeys` returns those identities as ambiguous
  instead, exactly as `appendGateKeysByDisplay` does for a duplicated
  `append_gate` display (#5546 review). Do not extend it to compare a
  glob-form trigger against a glob-form filter pattern — that equivalence is
  out of scope, not merely unimplemented.
- **`dornyFilters` walks jobs in sorted key order.** Every workflow here hosts
  exactly one dorny step, so the order cannot change today's answer — but Go
  randomises map iteration, so a second step would make "the first one" resolve
  differently run to run and silently flake any caller's verdict, including
  `internal/evidencecontinuity`'s trigger self-check. Keep the sort. This is the
  same randomised-iteration hazard `ifGatedFilterKeys` reports as ambiguous
  rather than guessing at.
- **Filter matching mirrors dorny's real semantics, not the intuitive ones.**
  `matchesDornyFilter` compiles each pattern separately and honours the
  `predicate-quantifier`: the default `some` includes a file when ANY pattern
  matches, `every` requires all of them. A leading `!` negates that single
  pattern (picomatch behaviour), so under `some` an exclusion can only ADD
  matches and never subtract one — which is why a list containing a catch-all
  `**` renders its own exclusions inert (#5896). Keep this faithful to what CI
  actually does; do not "fix" it into gitignore precedence.
- **A `ci.job` matching two `append_gate` calls with different filter keys is
  also ambiguous, and is reported, not silently collapsed.**
  `appendGateKeysByDisplay` returns both the unambiguous display->key map and
  a separate display->keys map for any display name two or more
  `append_gate` calls name with different filter keys (#5855 review). A plain
  `map[display]key` assignment would silently keep only the last-seen key, so
  every gate naming that display would be checked against whichever call
  happened to appear last in the workflow file. `checkPathFilterCoverage`
  skips the glob comparison for that gate (same "skip rather than guess"
  convention as the unresolved-`ci.job` case) but appends a drift error
  naming the gate and the conflicting keys, so the ambiguity is fixed instead
  of silently picked one way or the other.

- **Correspondence is only checkable for the sound subset.**
  `checkVerifyScriptWorkflowMatch` asserts that a gate whose
  `scripts/verify-*.sh` is invoked by exactly ONE workflow declares that
  workflow. Do not broaden it to "the declared job must run the gate's local
  command": a gate's local and CI entrypoints are legitimately different
  artifacts (CI runs golangci-lint where local runs `precommit-go.sh`, and
  `generate-contracttest.sh` where local runs `verify-contracttest.sh`). A
  one-time #5748 measurement of that broader, never-implemented rule found it
  flagged a double digit number of gates, nearly all of them legitimately
  wired rather than broken; that rule was never shipped, so those exact counts
  cannot be re-derived from code and are not restated here as a live figure —
  only the qualitative reason (entrypoint divergence, not drift) still holds.
  A script no workflow runs, or several run, carries no signal and is
  skipped. Match the script with a boundary check, never `strings.Contains`
  — a workflow running `myscripts/verify-X.sh` contains `scripts/verify-X.sh`
  as a substring, and counting it as a second host makes the "exactly one
  workflow" precondition fail, silently skipping the gate and turning a real
  mismatch into a pass. The size of the checkable sound subset itself IS
  live-verified: `scriptWorkflowSoundSubsetCount` in `scriptworkflow.go` is
  guarded by `TestScriptWorkflowSoundSubsetCount` against the committed
  registry, so it cannot go stale silently the way the doc comment's
  hard-coded gate count once did.

- **One authoritative list, one shared derivation, every side provably wired
  to it — not values compared.** `checkTrivySkipDirsParity` (`trivyskipdirs.go`)
  used to compare two independently-maintained `--skip-dirs` string literals —
  one bash, one YAML — for set equality. That is the shape #5925 shipped. The
  attempts to also prove each literal GOVERNED its own scan (flag arity,
  invocation arity, comment stripping) were tried and discarded here rather
  than shipped: across three review rounds (#5925) that direction accumulated
  repeated ways to defeat it, and round 3's findings were rounds 1 and 2's
  re-expressed in different bash syntax — proof the "parse bash correctly" bar
  was itself the unsound part of the design, not any one fix.
  The first redesign pass removed the parsing problem by making
  `specs/trivy-skip-dirs.txt` the single authoritative skip-dirs list and
  requiring both consumers to read it directly — but that still left
  `scripts/dev/trivy-fs-local.sh` and `security-scan.yml`'s `trivy-fs` job each
  carrying their OWN copy of the same `grep -v '^#' | grep -v '^$' | paste -sd,
  -` derivation pipeline. Identical today, enforced by nothing: a change to one
  side (e.g. adding inline-comment support) would silently diverge the two
  without failing any check — a smaller-scale replay of the original
  two-string-literals bug. `scripts/lib/trivy-skip-dirs.sh` (#5925 F2) removes
  that too: it is the ONE place the derivation pipeline exists
  (`trivy_skip_dirs_csv`, sourced by both consumers), and both
  `scripts/dev/trivy-fs-local.sh` and `security-scan.yml`'s `trivy-fs` job must
  be provably wired to INVOKE it — not to independently derive or hard-code a
  value that might no longer match it. Do not add back a second,
  separately-maintained derivation or list on either side.

  **This check proves wiring, not value flow — say so, don't overclaim.** It
  confirms the helper reads the specs file, and that the local script and the
  CI producer step each `source` the helper and call the function it defines,
  `trivy_skip_dirs_csv` (`checkSourcedAndCalled`); it does NOT interpret shell
  control or data flow to confirm nothing downstream of that call later
  overwrites the value (e.g. a producer step that correctly sources and calls
  the helper and then appends `echo "dirs=." >> "$GITHUB_OUTPUT"`, or a local
  script that calls the helper and then reassigns `skip_dirs="."` on the next
  line). That is deliberately out of scope: proving it would mean re-deriving
  exactly the kind of shell-semantics parser this redesign exists to avoid. It
  is a downstream mutation of an otherwise correctly-wired invocation, and it
  would be visible in review of a security-workflow diff the same way any
  other suspicious edit to a CI security gate would.

  `checkSourcedAndCalled` (#5927 review) replaced an earlier, weaker shape:
  the producer step and local script used to only need to MENTION the
  helper's path anywhere in whole-line-comment-stripped content
  (`strings.Contains` / bare reference-counting via
  `checkFileReferencesPathOnce`), so a mention inside a string literal or an
  echo — while a hard-coded value governed the actually emitted skip-dirs —
  read as "invokes the helper". Requiring proof of BOTH a
  `source` (or `. `) line AND a call to `trivy_skip_dirs_csv` closes that
  class for the SOURCE half structurally: the source-line matcher anchors on
  the command word at the start of a line, so a bare mention that is not
  itself a `source`/`.` command — including one placed in a trailing comment
  on the SAME line as unrelated code — never matches. The CALL half is a
  plain word-bounded identifier match over the same whole-line-comment-stripped
  content, so it keeps the same narrower, long-standing residual gap the rest
  of this file accepts: a mention of `trivy_skip_dirs_csv` in a trailing
  comment on the SAME line as otherwise unrelated code is not (in principle)
  distinguished from a live call. Closing that would mean reintroducing the
  mid-line bash parsing this redesign exists to avoid; it is left as the same
  kind of review-visible risk as the downstream-mutation gap above, not a
  defect in this check.

  **The SOURCE half's structural closure carries a fail-closed limitation of
  its own (P3-1, #5927 round-5 review).** `trivySourceLineRE` and
  `trivyPipefailRE` anchor on the command word at the start of a line — that
  is what makes a mention inside a trailing comment or a string literal
  unable to match. The same anchor also rejects a `source` or
  `set -o pipefail` that is a real, live command but does not sit at column
  0 of its own line: after a `;`, `&&`, or `||` on a shared line, inside a
  one-line `{ }` or `( )` group, after a line continuation, or inside
  `if …; then source scripts/lib/trivy-skip-dirs.sh; fi`. None of these are
  hypothetical — they are ordinary bash a script could legitimately be
  written in. The failure direction is the same fail-closed shape as the
  rest of this file: a script written this way reads as NOT sourcing the
  helper (or not setting pipefail) and fails the drift gate loudly, a false
  alarm a maintainer can see and fix by moving the command to its own line,
  rather than a script that never sources the helper passing silently.
  Recognizing a command anywhere a shell could start one needs the same
  command-separator tracking this package's design exists to avoid.

  The check makes four assertions, in order: (1) the specs file exists, is
  non-empty, has no duplicate entries, no entry with leading/trailing
  whitespace (the shell and Go derivations could otherwise tokenize
  differently, #5925 F5), no entry containing a comma — the delimiter the
  shared derivation's `paste -sd, -` joins entries with, so an embedded comma
  would smuggle extra entries past every other per-entry check (single-source
  review F1) — no entry containing a `#` — only a WHOLE-LINE comment is
  supported (a line whose first non-whitespace character is `#`), never a
  trailing one, so a mid-entry `#` is rejected rather than silently kept as
  part of the directory trivy actually receives (round-2 review P2-2: proven
  against real trivy — `--skip-dirs 'alpha'` skips alpha, `--skip-dirs 'alpha
  # rationale'` skips nothing, because the shared derivation's
  `grep -v '^[[:space:]]*#'` only drops whole comment lines) — no entry
  containing a glob metacharacter (`*`, `?`, `[`, `]`, `{`, `}`) — trivy's
  `--skip-dirs` wants a literal repo-relative path per entry, not a pattern
  — and no entry that NORMALIZES (`filepath.Clean`, the way trivy's own
  `CleanSkipPaths` does, then a trimmed leading `/`) to `.` — the catch-all
  that would disable the scan's coverage entirely — or to `""`, which is
  rejected for a DIFFERENT reason its own error message states: proven
  against real trivy 0.72.0 on a fixture with 2 planted secrets, an entry
  normalizing to `""` (e.g. `/`, `//`, or `/.`) left both secrets findable,
  unlike `.` which found zero. Read against trivy's own source
  (`pkg/fanal/utils/utils.go`), `CleanSkipPaths` does not drop an
  empty-after-clean entry; `SkipPath`'s `doublestar.Match` against an empty
  pattern simply never matches a real repo-relative path, so the entry
  disables nothing — it is dead weight, the opposite of a catch-all
  (#5925/#5927 round-7 review F1) — or to `..`,
  which is rejected for a narrower, DIFFERENT reason its own error message
  states: proven against real trivy 0.72.0, `--skip-dirs '..'` does not
  disable coverage the way `.` does (both planted secrets in the proof
  fixture stayed findable); it is rejected because it escapes the repository
  root the list is defined relative to, not because it defeats the scan
  (#5925 F6, closed structurally by round-2 review P1-1 after proving the original
  entry-specific literal check — reject only `.`, `*`, or a leading `/` as
  the WHOLE entry — defeated against real trivy 0.72.0 by `**`, `**/*`,
  `./`, `.//`, `./.`, and `?`); and no entry that is itself an absolute path
  (a leading `/`) — checked on the RAW entry, not the normalized value: the
  `filepath.Clean` + trimmed-leading-`/` normalization the catch-all and
  escape-root checks above use would otherwise strip a leading `/` before
  either of those questions is asked, letting `/etc` read as the bare
  relative entry `etc` (round-4 review: this is a separate rule, not a
  special case of the catch-all or escape-root normalization above it);
  (2) `scripts/lib/trivy-skip-dirs.sh` references the specs file's path exactly
  once, counted over whole-line-comment-stripped content — never truncating
  mid-line at `#`, so a misclassified line can only lower the count, never
  raise it; for the 0/1 boundary this check gates on, lowering can only fail
  red (a drop from 2 to 1 would silently pass instead, but that needs a live
  reference to sit on a line a plain bash script would never format as a
  whole-line comment, so no known input reaches it) — this is what mid-line
  `#` parsing kept failing at across all three review rounds; (3) the
  local script both `source`s the HELPER exactly once and calls
  `trivy_skip_dirs_csv` exactly once, same comment-stripping rule — not
  merely a mention of the helper's path once, and not zero or more than one of
  either half — and `set`s pipefail (`set -euo pipefail` or `set -o
  pipefail`), the local-script half of the failure-mode-parity requirement
  (4) enforces CI-side via `shell: bash` (round-2 review P2-3); (4) the CI
  workflow's `trivy-fs` job's `aquasecurity/trivy-action` step's `skip-dirs`
  input is EXACTLY a `${{ steps.<id>.outputs.<name> }}` expression —
  full-string match, not a value that merely contains one — and the step
  with that id both `source`s the HELPER and calls `trivy_skip_dirs_csv` in
  its `run:` block, the same source-and-call proof as (3), also over
  whole-line-comment-stripped content (#5925 F3 stripped whole comment lines
  before this question was asked at all; #5927 replaced the mention-counting
  question itself with the source-and-call one). Exact string equality on
  the workflow input kills the append-a-directory-at-the-call-site bypass
  class for free: there is no partial-match branch left to defeat by
  appending `,node_modules` after the expression.

  The CI-side read stays job-scoped via YAML, never regexed file-wide, for the
  same reason it always was: `security-scan.yml` has multiple jobs, and a
  whole-file scan cannot tell a `skip-dirs` input or a step id that belongs to
  `trivy-fs` from one that belongs to an unrelated job. Fail loudly at every
  step rather than skipping — a parity check that silently passes when it
  cannot find its subject is worse than none, because green reads as proof of
  agreement. The one legitimate skip is all four artifacts being absent, which
  is the shape of this package's synthetic drift fixtures; any subset present
  is itself checkable and any failure to wire up is reported.

  The CI-side check (`checkCIWorkflowSkipDirsFromHelper`) also asserts the
  producer step declares `shell: bash` — GitHub Actions' default `run:` shell
  omits `-o pipefail`, so without it an unreadable specs file failing inside
  `trivy_skip_dirs_csv`'s pipeline would silently produce an empty skip-dirs
  value instead of failing the step.

  **The output-name-write assertion was attempted and retired (#5927, five
  review rounds).** A `trivyOutputAssignmentPattern` line-regex used to also
  require the producer step's `run:` block to WRITE the exact output name the
  trivy-action step's `skip-dirs` expression references into
  `$GITHUB_OUTPUT`, via an `echo`/`printf` line assigning that name
  (single-source review F7, F8; round-2 P2-1 tightened F8's original bare
  `strings.Contains(runBlock, outputName)` check, which the step's own
  sourcing/calling lines could satisfy regardless of what its output line
  wrote). Rounds 3 and 4 each closed one further boundary defect in that same
  pattern — a `\b` word boundary letting a hyphenated sibling key like
  `skip-dirs=` read as writing `dirs`, then four more sibling-key shapes
  (`my.dirs=`, `a/dirs=`, `a:dirs=`, `v1.dirs=`) defeating the denylist round
  3 built to fix it. Round 5 did not find a sixth instance; it proved the
  premise false instead. The pattern's own doc comment justified its LEFT
  boundary — `[ \t"']`, "immediately after whitespace or an opening quote
  character" — by arguing "a real key assignment can only begin where a new
  shell word begins". That is true only in **unquoted** shell text. Every
  assignment this pattern was ever asked to match lives **inside a quoted
  argument** (`echo "dirs=${d}"`), where whitespace does not start a shell
  word — so `[ \t"']` is exactly the complement `[^ \t"']` round 4's denylist
  was, rewritten from the other side. Proven against real bash and
  end-to-end through this package's own drift check:
  `echo "my dirs=${d}" >> "$GITHUB_OUTPUT"` is accepted with a real key of
  `"my dirs"` (`outputs.dirs` stays empty); `echo 'a dirs=x' >>
  "$GITHUB_OUTPUT"` is accepted with a real key of `"a dirs"`; `echo
  "pre"dirs=x >> "$GITHUB_OUTPUT"` is accepted with a real key of
  `"predirs"`; and `echo "dirs=${d}" >> "${GITHUB_OUTPUT}x"` /
  `echo "dirs=${d}" >> "$GITHUB_OUTPUT.bak"` are both accepted while writing
  to a different file entirely. Deciding which shell word an `=` belongs to
  needs quote and command-separator tracking — mid-line bash parsing, the
  same unsound bar the top of this file documents this package's design as
  existing to avoid. The pinning test suite carried no information about the
  class either: all four of round 4's negative cases use separators the
  round-3 allowlist already excluded, so a green suite never exercised the
  gap round 5 found.

  `trivyOutputAssignmentPattern` and its call site in
  `checkCIWorkflowSkipDirsFromHelper` were deleted rather than patched a
  sixth time. **This is a narrowing of the check, not a strengthening.**
  `checkCIWorkflowSkipDirsFromHelper` itself is still live — what remains
  proves the producer step is sourced, called, `shell: bash`-declared, and
  that the trivy-action step's
  `skip-dirs` input references some output of that same step by id — it does
  NOT verify the producer step writes that exact output key. A key mismatch
  (a typo, a wrong-cased name, or any of the shapes above) now yields an
  empty `skip-dirs` value at scan time: trivy scans everything instead of
  skipping the intentionally-vulnerable fixture directories, and the
  `trivy-fs` job fails loudly on its own findings. The consequence is
  diagnosis time, not scan coverage — the job does not go green on a
  misconfigured skip list, it fails for a different, less obvious reason.

- **checkScriptTriggerCoverage (`scripttrigger.go`, check 8) requires triggers
  to be declared, not derived.** An implicit rule — treating a gate's own
  `local` scripts, and whatever they source, as triggers automatically —
  would make this check's drift impossible by construction. This branch's
  registry diff is 56 lines (`git diff --numstat origin/main HEAD --
  specs/ci-gates.v1.yaml`); 54 of them are `scripts/` trigger paths check 8
  required on landing (#5762) — the other 2
  (`go/internal/serviceintelhttp/**`, `.github/openapi-known-drift.txt`)
  were already on the branch before check 8 existed — they land in the
  branch's first commit, `scripttrigger.go` in a later one — and are unrelated
  to it. Stated as an ordering rather than a commit distance on purpose: a
  rebase renumbers the distance but not the order. An implicit-derivation
  rule would have cut those 54 lines to zero, not the whole 56. Declaring
  keeps the registry the single readable answer to "what selects
  this gate": a reviewer reads `triggers:` and knows the whole selection
  surface without also reading the gate's local command and every script it
  sources. (The two most recent additions, #5934 review: `perf-evidence` and
  `nancy` each run a `scripts/dev/precommit-go.sh` subcommand that execs
  another script directly rather than sourcing it, so neither was visible to
  the sourcing walk below — see checkScriptTriggerCoverage's doc comment for
  the audit that found them.)

  Sourcing is followed transitively: golden-corpus-lock-cases.sh is the real
  case, sourced directly by scripts/test-verify-golden-corpus-gate.sh and
  itself sourcing two more case files the gate's `local.command` never names.
  The walk uses a visited set so a sourcing cycle cannot hang it — no script
  in this repo sources one that sources it back today, but the walk must stay
  safe regardless. Every `scripts/`-prefixed token in a compound
  `local.command` is checked too, not only the first: ci-gate-registry's
  `local.test_command` chains four scripts/ invocations with `&&`, and
  frontend-console-checks's `local.command` chains two. See
  checkScriptTriggerCoverage's doc comment for the full, open-ended list of
  narrowings this check has — it is not a fixed count, and a comment claiming
  one has already been wrong twice (#5762 rounds 9 and 10).

- **checkGoPackageTriggerCoverage (`gopkgtrigger.go`, check 10) is check 8 for
  gates written in Go, and keeps the same declared-not-derived rule.** Check 8
  only derives `scripts/`-prefixed tokens, so it is blind to the local gates
  whose implementation is a Go package (`go run ./cmd/x`, `go test
  ./internal/y`). Editing the program that IS such a gate did not select it,
  which is the #5873 false green. Check 10 derives the REQUIREMENT from the
  gate's own command and still demands the trigger be written in
  `specs/ci-gates.v1.yaml`, so `triggers:` stays the single readable answer to
  "what selects this gate".

  Three rules are worth knowing before you edit a gate's command. A
  package-level `go test ./internal/x` compiles every file in that package, so
  per-file triggers do not satisfy it — `ifa-materialized-edge-coverage` kept
  its per-file entries as the reader's map of what it guards and added
  `go/internal/ifa/**` and `go/internal/reducer/**` alongside them. A recursive
  `./...` demands a trigger reaching nested files, because that is what the
  command compiles. And a trigger must be directory-wide rather than narrowed
  by extension: `go/internal/x/*.go` does not cover the package, because
  `go/internal/capabilitycatalog` embeds `data/catalog.generated.json` and
  editing an embedded asset changes the compiled package.

  The parser models subshell scope, and that is load-bearing rather than
  fussiness. An earlier version tracked one directory forward through the whole
  command and argued a leaked `cd` was safe because it could only resolve
  DEEPER than the shell would. That reasoning is wrong whenever a broader
  ancestor trigger exists, and #5955's review produced the counterexample:
  `(cd sdk/go/collector && go test ./...) && (cd sdk/go/factschema && go test
  ./...)` put the second package under `sdk/go/collector/`, which
  `sdk/go/collector/**` matches at any depth. If you touch this parser, the
  question to ask about any narrowing is not "is it more specific" but "can a
  broader trigger swallow it".

  Do not write the number of Go-package gates into prose. It belongs in
  `goPackageGateCount`, which `TestGoPackageGateCount` derives from the
  committed registry through the production extractor. This package has paid
  for that twice: `checkVerifyScriptWorkflowMatch`'s doc comment hard-coded 29
  gates and the registry silently grew past it, and this file's first version
  said 19 by reusing a count of gates with no `scripts/` token — which includes
  the npm gates. A reviewer caught the second one.

  Widening a trigger costs `make pre-pr` time, so measure it rather than
  guessing. Landing #5873 changed exactly two selections, each by one gate: a
  reducer-touching PR went 18 → 19 gates (+10.6s warm) and a
  capability-inventory PR 17 → 18 (+2.9s warm). If a future widening costs
  materially more than that, prefer narrowing the gate's COMMAND — a
  `-run`-filtered package test still compiles the whole package, so the honest
  fix is a smaller package, not a smaller trigger.

## Common changes

- Adding a new category or requirement: add the constant, add to the validation
  map, add a `Load_Bad*` test case.
- Adding a new tier: add to `tierOrder` with the correct numeric rank, add to
  the tier-ordering tests in `select_test.go`.
- Extending `Gate` with a new field: add to `gateFile`, map in `Load`, add a
  `TestLoad_Valid*` assertion.
- Adding, removing, or rewiring a gate that changes which gates fall in the
  verify-script sound subset (a gate's `scripts/verify-*.sh` invoked by
  exactly one workflow): run
  `go test ./internal/cigates -run TestScriptWorkflowSoundSubsetCount -v` and
  update `scriptWorkflowSoundSubsetCount` in `scriptworkflow.go` (and its doc
  comment) to match what the test reports. Do not hand-edit the constant
  without re-running the test.
- Adding a new dorny/paths-filter workflow: no code change is needed in
  `pathfilter.go` itself — `checkPathFilterCoverage` picks it up automatically
  once a gate's `ci.job` resolves to a filter key, either via `append_gate` or
  via an `if:` on a paths-filter output. Add a focused case in
  `pathfilter_ifgated_test.go` (or `pathfilter_test.go` for the matrix shape)
  covering the new workflow's filter shape.

## Tests

```bash
cd go && go test ./internal/cigates/ -count=1
```

Every new branch or enum value needs a focused test. Negative tests must fail
when the production assertion is removed.
