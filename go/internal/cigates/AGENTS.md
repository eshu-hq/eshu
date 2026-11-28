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
  tracked FILE (#6159). There is no waiver field and no warn-only mode: a
  trigger matching zero paths can never select its gate, so the gate reads as
  wired for a surface it no longer guards. If the tracked path set cannot be
  read, that is an error too, not a skip — an unverifiable trigger must not
  read as present. Resolve globs against the TRACKED set, never a filesystem
  walk: a walk lets a build artifact satisfy a trigger CI can never fire on,
  and makes the verdict depend on the developer's tree. The same rule covers jobs: a job in a workflow a blocking gate names must be owned by a row or needed by an owned job (`jobownership.go`, drift check 13); give a new job an owner row, never an exception list. A blocking owner row's literal triggers must match the workflow's `pull_request.paths:` (`ownerrowtriggers.go`).
- **The universe is FILES. Do not put directories back in it, on either half.**
  `Select` matches triggers against changed paths, and every caller supplies
  files (`git diff --name-only`, GitHub's pull-files response), never the
  directories containing them: `MatchGlob("go/internal/*",
  "go/internal/cigates")` is true while
  `MatchGlob("go/internal/*", "go/internal/cigates/glob.go")` is false, and
  only the second is a question `Select` asks. So `loadTrackedPaths` derives
  NO ancestor directories, and `checkTriggerPathsExist` rejects a literal
  trigger that stats as a directory for the same reason. #6159's first draft
  did the opposite, on the reasoning that the literal half accepted one via
  `os.Stat`, and certified `go/cmd/collector-**` — the golden-corpus gate's
  only claim on the collector binaries — which matched the directory
  `go/cmd/collector-tempo` and no file under it. `directoryMatch` derives
  directories on the FAILURE path only, to name the working `dir/**` spelling;
  never to feed a passing verdict. README.md carries the dialect gap that
  produced it, which recurs whenever a trigger is copied from a workflow.
- **Answering "no match" is bounded, and the bound is load-bearing.**
  `matchSegments` branches at every `**`, so two or more let a naive recursion
  re-explore (pattern suffix, path suffix) states exponentially — 24.7s for
  ONE 18-segment candidate against 17 consecutive `**` plus a missing literal,
  times the ~20k candidates every trigger is run through. The memo on those
  states makes the always-on check safe (11µs after). It is allocated only for
  patterns carrying two or more `**`, and `matchesAny` decides that ONCE per
  pattern via `matchSegmentsBounded`, not per candidate — folding the count
  back into the per-candidate call measured 49ms -> 62ms. Do not "simplify" it
  to normalizing `**/**` into `**`: that fixes only the adjacent spelling. The
  two `NoMatchIsBoundedOnAdversarialPattern` cases cover both spellings on
  both entry points; their fixture DEPTH is load-bearing, since the blow-up
  scales with path length and the standard 4-segment fixture let the
  `Validate` case pass with the memo disabled.
- **The tracked path universe is enumerated once per `Validate` call.** ~500
  glob triggers against ~20k paths is ~10M pattern comparisons; re-enumerating
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
- **The terminal publisher's verdict is OBSERVED, never read.** Do not add a
  check that decides what `required-gates.yml` publishes by matching text in
  its `run:` script. Four review rounds on #6218 did exactly that -- one check
  per case arm, one for the `-f state=` argument, one for the lines between
  `esac` and the publish -- and three were defeated by an ordinary prose
  comment moving a substring anchor, the last one by a single line reading
  `# the gh api -X POST call below publishes the status`. That step is 55
  lines of which 34 are comments, so prose is its normal content, not an
  exotic input. `requiredworkflow_publishrun.go` runs the step under bash with
  `gh` intercepted and `requiredworkflow_publishcontract.go` asserts on the
  recorded argv; add new publisher rules there, as claims about a posted
  VALUE. Two related traps this already fell into: do not require a
  particular PHRASE in a description (a rewording then reds -- assert
  distinctness instead), and do not select the terminal step by a literal it
  happens to contain (`state=failure` was the old selector, so rewording that
  arm silently switched the whole contract off).

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
  to it — not values compared (`trivyskipdirs.go`).**
  `specs/trivy-skip-dirs.txt` is the single skip-dirs list, and
  `scripts/lib/trivy-skip-dirs.sh` (`trivy_skip_dirs_csv`) is the single
  derivation pipeline. Both `scripts/dev/trivy-fs-local.sh` and
  `security-scan.yml`'s `trivy-fs` producer step must `source` the helper and
  call `trivy_skip_dirs_csv`, with `pipefail` enforced locally and `shell: bash`
  declared on the CI step. The check validates:
  1. `specs/trivy-skip-dirs.txt` is non-empty, has no duplicates, no leading or
     trailing whitespace, no embedded commas or `#` characters, no glob
     metacharacters, no absolute paths (leading `/`), and no entries normalizing
     to `.`, `""`, or `..`.
  2. `scripts/lib/trivy-skip-dirs.sh` references the specs path once.
  3. `scripts/dev/trivy-fs-local.sh` sources the helper once, calls
     `trivy_skip_dirs_csv` once, and enables `pipefail`.
  4. `security-scan.yml`'s `trivy-fs` job sets `skip-dirs` strictly to
     `${{ steps.<id>.outputs.<name> }}` and that producer step declares
     `shell: bash`, sources the helper, and calls `trivy_skip_dirs_csv`.
  This proves wiring, not downstream shell data flow or exact `$GITHUB_OUTPUT`
  key assignment — do not reintroduce mid-line bash parsing regexes. Keep
  `source` and `set -o pipefail` at column 0 of their own lines so the anchored
  matchers fail closed.

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
  sources. (The most recent addition, #5934 review: `perf-evidence` runs a
  `scripts/dev/precommit-go.sh` subcommand that execs another script directly
  rather than sourcing it, so it was not visible to the sourcing walk below —
  see checkScriptTriggerCoverage's doc comment for the audit that found it;
  the nancy `nancy` subcommand did the same until the gate's deletion in #10989.)

  Sourcing, and Python import (#9479), is followed transitively: golden-corpus-lock-cases.sh is the real
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
