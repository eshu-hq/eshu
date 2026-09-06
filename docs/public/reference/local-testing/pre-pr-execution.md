# Pre-PR Execution Ownership

`make pre-pr` keeps local proof before publication and runs the blocking gates
selected by the branch. Its documentation fast path changes which Go lanes are
needed; it does not turn off contract checks or focused fixture-consumer tests.

## Whole-module checks

On the full lane, the selected-gate runner owns the whole-module Go prelude.
It resolves formatting, lint, build, and vet commands from the gate registry.
Formatting precedes lint because they share mutable helper configuration;
build and vet run alongside that sequence. These checks run even when an
unknown changed path forced the full lane without matching their usual Go
triggers.

The same runner retains those results for the subsequent selected gates. An
identical command with the same hosted owner reuses its result, including a
failure. Results exist only in that invocation; this is not a cache across
commits or promotion attempts. Missing or invalid required core gates fail
promotion. Other selected gates and applicable verifier tests still execute.

Package documentation is owned by its selected registry gate. The full file-cap
check remains required when selected, even if the focused changed-file check
already passed: the two checks have different input scopes.

## Verifier tests and repository checks

A verifier's primary command checks the changed repository. Its self-test
checks that the verifier rejects deliberately broken examples. Some existing
suites perform both jobs, so their names alone cannot justify skipping them.

The citation suite exposes `--repository-only` for its real-tree baseline,
scan-floor, and ledger checks, and `--fixtures-only` for verifier regression
cases. Its default invocation still runs the complete suite. The gate's primary
command retains the real-tree checks whenever source, documentation, or fixture
changes select it. Only the fixture suite uses narrower `self_test_triggers`.

Measurement-citation and documentation-build self-test triggers include their
implementation and harness inputs. Documentation checks that consume real
pages keep those pages in their self-test selection. Suites whose real-tree
checks have not been separated retain the conservative default behavior.

CI continues to run its independent checks. A local reuse record does not
replace hosted validation or the independent semantic review required by the
repository's promotion process.

## Reading evidence

The gate JSON report records executed and reused commands, command hashes,
self-test skips, failures, and durations. Parallel command durations are not
additive wall time. Use the runner's elapsed duration for that stage and the
full preflight summary for the other stages. A successful stamp still belongs
to the exact commit; rebasing or amending requires fresh promotion.

See [documentation fast-path boundaries](pre-pr-docs-fastpath.md) and
[Local Testing](../local-testing.md) for the rest of the promotion workflow.
