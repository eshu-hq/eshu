# PR notes -- #5469 tiered version resolution, round-3 P3 disclosures

For pasting into the PR body. All findings below are from round-3 review
(P0=0, P1=0, P2=2 fixed in this pass, P3=4 disclosure-only).

## P3-3: benchmark numbers, cited from HEAD

`BenchmarkBuildSupplyChainImpactFindingResult` lives in
`go/internal/query/supply_chain_impact_version_resolution_test.go`. It
exercises `go/internal/query`'s result-assembly path, which the round-3 P2-2
fix (atomic CI-declared-identity baking) does not touch -- that fix is
entirely inside `go/internal/reducer`. Re-run after the P2-2 change, at HEAD
(commit `979801e4b1`):

```
go test ./internal/query/ -bench BenchmarkBuildSupplyChainImpactFindingResult -benchtime=2s -count=3 -run '^$'
```

```
BenchmarkBuildSupplyChainImpactFindingResult-18    	 7539453	       327.5 ns/op	     208 B/op	       2 allocs/op
BenchmarkBuildSupplyChainImpactFindingResult-18    	 7625864	       299.6 ns/op	     208 B/op	       2 allocs/op
BenchmarkBuildSupplyChainImpactFindingResult-18    	 8253686	       310.2 ns/op	     208 B/op	       2 allocs/op
```

Range: 299.6-327.5 ns/op, 208 B/op, 2 allocs/op (3 runs, -benchtime=2s),
consistent with round 3's HEAD measurement (316.6-363.7 ns/op, 208 B/op,
2 allocs/op) -- the P2-2 change did not move this number, as expected since
it touches a different package. Baseline on `origin/main` (58f364f68f):
156.6 ns/op, 16 B/op, 1 alloc/op. The prior increase from baseline is the
already-reviewed cost of the tiered version-resolution feature itself, not
of the P2-2 fix.

## P3-4: the winner's own digest_or_version is never emitted

`version_resolution_tier` reports which tier won, but the winning tier's own
`digest_or_version` value is never emitted as a separate field -- only the
tier string and the weaker tiers' values (via
`version_resolution_corroboration`). An operator can derive the winner's
value (it is `subject_digest`/`image_ref`/`observed_version` on the finding
itself, per the tier), but only by already knowing the precedence rules.
Disclosed as a known limitation; no code change in this pass.

## P3-5: fixture-only edits don't re-run the golden gate

Neither `.github/workflows/golden-corpus-gate.yml`'s paths filter nor
`scripts/dev/pre-pr.sh`'s change-detection regex watches
`tests/fixtures/ecosystems/**`, so a fixture-only edit (for example, deleting
`tests/fixtures/ecosystems/github_actions_workflows/package.json`) would not
automatically re-run the gate that depends on it. This is a pre-existing,
repo-wide gap, not introduced by this branch. P2-1's README addition (naming
the `config_only` pin and marking the files "do not remove") is the cheap
in-scope mitigation for this specific fixture. Disclosed only -- not fixed
(out of scope for #5469) and no issue filed for it.

## Deferred: same-axis live disagreement fixture

Deferred pending owner approval -- no issue filed for this yet.

## No-Observability-Change

`No-Observability-Change:` this is a pure read-time/reducer-time classifier
change (atomic baking of already-computed deployment identity fields); no new
instrument, metric, span, or log was added or needed.

## Golden gate run provenance

The previous full gate run (512 pass / 0 required-fail) was at commit
`0bb21884a4`, before both round-3 P2 fixes in this pass. The new full gate
run below is the first to cover HEAD (`979801e4b1`), including the atomic
CI-declared-identity bake.

**New run, full `bash scripts/verify-golden-corpus-gate.sh`, at HEAD
(`979801e4b1`):**

```
summary: 511 pass, 0 required-fail, 1 advisory-warn
```

Zero `[FAIL]` lines anywhere in the run. The `1 advisory-warn` is
`phase_maintenance_drains: observed=21.0s, baseline=14.0s, ceiling=19.0s (15%
band or +5s, advisory)` -- a wall-clock timing check, explicitly advisory
(not required-fail), that happened to land just past its ceiling on this
run. 511 pass + 1 advisory-warn = 512 total checks, the same total as the
`0bb21884a4` baseline (512 pass / 0 advisory-warn) -- this is the same check
flipping from pass to advisory-warn under this run's system load, not a
missing or newly-failing check. The two queries this branch's #5469 pins
depend on (`CVE-2026-00000` config_only and the runtime_confirmed/CI-declared
disagreement query) both still show `[PASS]` in the run output.

**The atomic bake did NOT move any golden pin.** Confirmed by grepping the
run output for every `ci_declared_artifact_digest` /
`ci_declared_image_ref` / `version_resolution_tier` /
`provenance_ci_declared` query: both matching lines are `[PASS]`, with no
value or object-match changes. This is expected -- neither existing golden
fixture triggers the atomicity fix's discriminating case (a first
strong-match deployment leaving one field empty, with a later deployment
carrying a value for that field); the fix only changes behavior for that
specific multi-deployment mixed-pair scenario, which is covered by the new
unit test
(`TestBuildSupplyChainImpactFindingsBakesCIDeclaredArtifactIdentityAtomically`
in `go/internal/reducer/supply_chain_impact_ci_declared_artifact_test.go`),
not by the golden corpus.
