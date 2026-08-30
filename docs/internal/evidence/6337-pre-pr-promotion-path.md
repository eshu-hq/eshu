# #6337 — shorten the local promotion critical path

## Problem

`make pre-pr` had grown into both a promotion gate and a test suite for the
gate framework. A product change selected each verifier's real command and its
fixture-based `test_command`, even when no verifier code or fixture changed.
Advisory reports also sat on the blocking local path. The final workflow then
ran a second full semantic review even when preflight changed no reviewed input.

This made the safest workflow the slowest one. Agents opened several pull
requests in parallel, but one machine serialized `make pre-pr`; GitHub Actions
then repeated the product checks on each exact PR head.

## Baseline and theory

The comparable baseline is a warm `make pre-pr` on the pre-rebase #6337 head.
It took **989 seconds (16m29s)**. The selected exactness and telemetry registry
phase took **809 seconds**, more than 80% of the total. The other measured
top-level phases were docs classification 3s, format 11s, lint 10s, build 20s,
vet 13s, focused tests 6s, race 12s, and the path-triggered live/security lane
105s. These phase durations overlap because some whole-module work runs in
parallel, so they must not be added to manufacture a second total.

A representative `go/internal/parser/rust/parser.go` change selected 16 local
registry gates: 15 blocking and one advisory. Those rows contained 11 distinct
`test_command` values. The theory was that the registry phase was paying for
tests of unchanged verifier harnesses, not only product verification. The
cheapest proof was the selected-command inventory; it showed a much larger
ceiling than the known format/lint/vet overlap, whose measured warm ceiling was
about 34 seconds.

## Change

The promotion path now keeps four separate decisions:

- A selected gate's `local.command` still runs.
- `self_test_triggers` may narrow a distinct verifier `test_command` to harness
  changes. Omitted declarations stay fail-closed and run the self-test.
- Default `make pre-pr` runs blocking registry gates. `make pre-pr-full` adds
  advisory registry gates and whole-module race.
- One full preliminary semantic review can be followed by an exact mechanical
  attestation when the base, diff, worktree, claims, review packet, and verdict
  remain unchanged.

The selected-gate runner writes a per-command JSON report with command hashes,
durations, run/reuse decisions, skip reasons, and failure counts. `make pre-pr`
keeps that report beside its per-SHA push stamp. This is local CLI evidence; it
adds no service metric, span, database query, network call, worker, or lock.

## Safety boundaries

The change does not let a local pass satisfy a required GitHub status. Hosted
checks still rerun on the exact PR head and the ruleset remains the merge
authority. OS-, credential-, service-, and deployment-dependent proof stays in
GitHub Actions or on the dedicated remote validation host when the change
contract calls for it.

An explicitly declared self-test trigger must also be a primary gate trigger,
must be non-empty and unique, and requires a distinct `test_command`. Unknown
YAML fields fail registry loading. Unclassified test commands always run.

Review attestation does not review code or disposition findings. It only proves
that the inputs of an already clean full review are still the inputs being
pushed. Any mismatch requires another full review.

## After measurement

The representative parser-path run used the same selected path named above and
the new default registry policy:

```text
elapsed_seconds=227
commands_run=16
self_tests_skipped=9
advisory_skipped=1
blocking_failures=0
```

All 15 selected blocking gates kept their product command. Nine verifier
self-tests did not match a harness path, and the advisory coverage report stayed
outside the promotion path. The report identified telemetry coverage at 68.211s,
Go file cap at 52.578s, lint at 35.041s, and directory structure at 25.298s as
the remaining largest commands.

This 227-second selected-path run is not a comparable replacement for the
989-second full-branch baseline: the path set and top-level phases differ. The
final `make pre-pr` run must use the same branch shape and start/terminal events
before this note can state an end-to-end delta. Until then, the supported result
is narrower: the new policy removed nine unchanged verifier suites and one
advisory gate from this product-path critical path without dropping a blocking
product verifier.
