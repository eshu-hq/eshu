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
  glob block, only when that workflow uses the matrix-dispatch
  `append_gate`/dorny pattern (e.g. `static-contract-gates.yml`). It skips
  rather than guesses when the mapping from gate to filter key is ambiguous:
  an unparseable or non-matrix workflow, a `ci.job` with no matching
  `append_gate` call, or a glob-form trigger. Do not extend it to compare a
  glob-form trigger against a glob-form filter pattern — that equivalence is
  out of scope, not merely unimplemented.
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

## Common changes

- Adding a new category or requirement: add the constant, add to the validation
  map, add a `Load_Bad*` test case.
- Adding a new tier: add to `tierOrder` with the correct numeric rank, add to
  the tier-ordering tests in `select_test.go`.
- Extending `Gate` with a new field: add to `gateFile`, map in `Load`, add a
  `TestLoad_Valid*` assertion.
- Adding a new dorny/paths-filter matrix-dispatch workflow: no code change is
  needed in `pathfilter.go` itself — `checkPathFilterCoverage` picks it up
  automatically once a gate's `ci.job` resolves to a filter key via
  `append_gate`. Add a focused case in `pathfilter_test.go` covering the new
  workflow's filter shape.

## Tests

```bash
cd go && go test ./internal/cigates/ -count=1
```

Every new branch or enum value needs a focused test. Negative tests must fail
when the production assertion is removed.
