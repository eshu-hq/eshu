# AGENTS — internal/cigates

Scoped rules for editing the CI gate registry core. Load `golang-engineering`
and `eshu-diagnostic-rigor`.

## Invariants

- **Load is the only entry point for YAML.** Never parse the YAML outside
  `Load`. Add new fields to `registryFile` / `gateFile` and map them in `Load`.
- **Select is a pure function.** It must not touch git, the filesystem, or any
  external service. Git access belongs at the CLI boundary in `cmd/ci-gates`.
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
  `generate-contracttest.sh` where local runs `verify-contracttest.sh`), so the
  broad rule flags 16 gates of which 15 are correctly wired (#5748). A script no
  workflow runs, or several run, carries no signal and is skipped. Match the
  script with a boundary check, never `strings.Contains` — `verify-X.sh` is a
  substring of `test-verify-X.sh`, and that false match turns a real mismatch
  into a pass.

## Common changes

- Adding a new category or requirement: add the constant, add to the validation
  map, add a `Load_Bad*` test case.
- Adding a new tier: add to `tierOrder` with the correct numeric rank, add to
  the tier-ordering tests in `select_test.go`.
- Extending `Gate` with a new field: add to `gateFile`, map in `Load`, add a
  `TestLoad_Valid*` assertion.
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
