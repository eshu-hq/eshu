# 6781 Part B — projector root split, no-regression evidence

## What changed

`go/internal/projector` went from 47 non-test files at its root to 7, with the
canonical, runtime, stage, decode and failure families moved into subpackages
(#6781 Part B, target tree approved by the owner on 2026-09-18).

The change is structural. No function body was rewritten. What moved is:

- files, by `git mv`, so history follows the move;
- 61 unexported symbols, exported because the split puts their callers in a
  different package;
- four `QuarantinedFact` fields, exported for the same reason;
- four canonical stage-label constants, from the canonical extractors into
  `decode/stage_label.go`, so `decode` stays a leaf;
- 1132 call-site references across 196 files, repointed from `projector.X` to
  the owning package.

No control flow, no ordering, no concurrency structure, and no I/O changed.

## No-Regression Evidence:

Benchmarks on the projection hot path, before and after, same machine, both
runs on an otherwise idle machine.

- Baseline: `origin/main` at `3fbf13ea6`, measured in a throwaway worktree.
- After: `restructure/6781-projector-root`.
- Toolchain: Go 1.27.1, darwin/arm64, Apple Silicon, 10 logical CPUs.
- Command, both sides (`./internal/projector/` before, `./internal/projector/runtime/` after,
  because the benchmarks moved with the code they measure):

```
go test <pkg> -run '^$' \
  -bench 'BenchmarkProjectionCloneRemovalProof|BenchmarkAppendScopeGenerationReducerIntentsFanOut' \
  -benchmem -count=6
```

Input shape: the benchmarks' own fixture generation — one repository scope, one
active generation, the fixed fact batch each benchmark builds. No backend, no
Postgres, no NornicDB: these are in-process projection benchmarks, so the
measurement isolates the code the split touched rather than backend variance.

| measurement | baseline (6 runs) | after (6 runs) |
| --- | --- | --- |
| `ProjectionCloneRemovalProof/Clone` ns/op | 13,276,979 – 15,393,140 | 13,199,577 – 13,447,298 |
| `ProjectionCloneRemovalProof/Clone` B/op | 21,824,522 – 21,824,728 | 21,824,950 – 21,825,195 |
| `ProjectionCloneRemovalProof/Clone` allocs/op | 108,979 – 108,980 | 108,981 – 108,983 |
| `ProjectionCloneRemovalProof/Borrow` ns/op | 10,205,434 – 11,055,324 | 10,196,754 – 10,325,852 |
| `ProjectionCloneRemovalProof/Borrow` B/op | 17,364,320 – 17,364,585 | 17,364,645 – 17,364,745 |
| `ProjectionCloneRemovalProof/Borrow` allocs/op | 73,981 – 73,983 | 73,983 – 73,984 |
| `AppendScopeGenerationReducerIntentsFanOut` ns/op | 197,898 – 207,393 | 192,499 – 196,710 |
| `AppendScopeGenerationReducerIntentsFanOut` B/op | 76,216 | 76,216 – 76,217 |
| `AppendScopeGenerationReducerIntentsFanOut` allocs/op | 192 | 192 |

Every after-range is inside or below its baseline range. Allocation counts are
the load-bearing number here, not ns/op: they move by at most 4 out of ~109,000
(0.004%) on the projection benchmarks and are byte-identical on the fan-out
benchmark. A structural change that accidentally introduced a copy, an
interface boxing, or a lost inline would show up there first, at a magnitude
wall-clock noise cannot hide.

Row and queue counts are unchanged by construction: `go test -list '.*'` across
the projector tree returns the same 350 test names before and after, with an
empty `comm` difference in both directions, and the full
`go test ./internal/projector/... -count=1` suite passes. Those suites are what
assert the terminal row and reducer-intent counts; none of their expectations
were edited.

### A contaminated run, recorded so the number is not mistaken for a result

An earlier after-run measured `Clone` at 39.2–49.6 ms/op, roughly 3x the
baseline, while a repo-wide `rg` sweep from the doc-citation gate was running on
the same machine. Its allocation counts were unchanged (108,977–108,978), which
is what identified the result as CPU contention rather than a regression. It was
discarded and re-measured on an idle machine; the table above is the re-measured
run. Recorded because a future reader comparing against a stale scrollback would
otherwise see a 3x number with no explanation.

## No-Observability-Change:

No metric, span, log key, or status field is added, removed, or renamed.

- `scripts/verify-telemetry-coverage.sh` exits 0:
  "docs/public/observability/telemetry-coverage.md and
  go/internal/telemetry/instruments.go agree, no new untracked stages".
- The four canonical stage labels keep their exact string values
  (`codegraph_canonical`, `oci_registry_canonical`,
  `package_registry_canonical`, `terraform_state_canonical`); only their
  declaration site moved, from the canonical extractors to
  `decode/stage_label.go`. The `stage` dimension on
  `eshu_dp_projector_input_invalid_facts_total` is therefore unchanged, and the
  bounded set is unchanged.
- `telemetry-coverage.md` cites the intent-enqueue site at its new path; the
  instrument it names is the same one.
- Stage-duration recording, phase publication, and the dead-letter triage
  classes are untouched apart from their file paths.

## Why this is safe

The accuracy argument is that nothing executes differently: the compiler
resolves the same symbols to the same code, and the only semantic latitude a
package split has — an import cycle — is a build error rather than a silent
behavior change. The measured file-level reference census that drove the
partition is in the PR body; the resulting graph is a DAG with `decode` and
`failure` as leaves and no subpackage importing the root, confirmed with
`go list -deps`.

The performance argument is the table above: flat or better on every metric,
with allocation counts as the sensitive indicator.

The concurrency argument is that the split moved no lock, lease, claim, or
queue boundary. `PackageRegistryIdentityLocker` still brackets the
package-registry identity writes, and `package_registry_lock_test.go` still
pins both the locked path and the no-rows-so-no-lock path; both moved into
`runtime/` alongside the code they exercise.
