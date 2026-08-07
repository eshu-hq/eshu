# #5944 — execute CI gate self-tests locally

## Problem

`ci-gates run` loaded and validated `local.test_command`, but executed only
`local.command`. A registry row could therefore advertise a local self-test
that never ran during `make pre-pr`.

The `ci-gate-registry` row exposed the gap. Its self-test command includes the
registry verifier tests, pre-PR scheduling tests, generated-doc tests, live
required-status tests, and `internal/cigates` tests. Before this change, the
registry declaration itself did not run any of them. The #5939 workaround
separately reached `internal/cigates` for registry and workflow changes, while
the other mirrors depended on direct invocations outside the registry runner.

## Failure proof

Before the implementation, this command failed:

```bash
cd go
go test ./cmd/ci-gates -count=1 -run '^TestExecuteGates'
```

Observed failures:

- the trace contained `command` but not `test`;
- a blocking `test_command` failure returned `nil`;
- a primary-command failure prevented the test command from appearing in the
  trace.

Exit code: `1`.

## Result

The local runner now:

1. runs `local.command`;
2. runs a distinct, non-empty `local.test_command`, even when the primary
   command failed;
3. attributes self-test failures as `blocking test_command` or
   `advisory test_command`;
4. executes byte-identical command/test-command pairs once;
5. continues through every selected gate and returns failure only when at least
   one blocking command failed; and
6. generates the public CI-gates reference with the complete ordered local
   execution sequence, while rendering byte-identical pairs once.

The old `specs/ci-gates.v1.yaml` and `.github/workflows/**` fixture-consumer
mapping was removed because it duplicated the now-executable
`ci-gate-registry` self-test.

## Green proof

```bash
cd go
go test ./cmd/ci-gates ./internal/cigates -count=1
```

Both packages passed. The CLI integration test loads a YAML registry, selects a
gate from a changed path, runs the binary with `--repo-root`, and observes the
primary command followed by its test command.

The registered verifier and exact self-test command also passed:

```bash
bash scripts/verify-ci-gates-registry.sh --drift
bash scripts/test-verify-ci-gates-registry.sh && \
  bash scripts/test-pre-pr-whole-module-gates.sh && \
  bash scripts/test-generate-ci-gates-doc.sh && \
  bash scripts/test-verify-live-required-status-checks.sh && \
  (cd go && go test ./internal/cigates -count=1)
```

A registry-driven hygiene run on the real branch printed `TEST` entries for
the selected gates and passed:

```bash
bash scripts/dev/run-selected-gates.sh \
  --base origin/main \
  --tier pre-pr \
  --category hygiene
```

Observed wall time: `204.37s`. This run was functional proof, not the primary
performance comparison, because its cache state differed from the baseline.

## Comparable performance measurement

Two binaries were built: the before binary from base
`37034374e789b44a4233dea127fabde41cfff3a2`, and the after binary from the
working branch. Both ran against the same working tree, registry, changed-path
set, `hygiene` category, warmed caches, and repo root.

| Runner | Behavior | Wall time |
| --- | --- | ---: |
| Before | `local.command` only | `87.34s` |
| After | command plus distinct `local.test_command` | `110.37s` |

The added self-test cost was `23.03s`, or `26.4%`, for this selected gate set.
The comparison does not claim a repository-wide fixed cost: selection is
path-dependent, and other diffs select different self-tests.

## Operational impact

This changes a local and CI-helper CLI, not an Eshu service runtime. It adds no
database, graph, network, worker, retry, lock, or telemetry path. Operators now
see a separate `TEST` line before a self-test runs, and failures name whether
the primary command or `test_command` failed.

No-Regression Evidence: primary commands retain their existing order, shell,
working directory, environment, blocking/advisory semantics, and accumulate-
all-results behavior.

No-Observability-Change: no service metric, span, structured log, or status
contract changes; the only new signal is local CLI output for an execution path
that was previously silent.
