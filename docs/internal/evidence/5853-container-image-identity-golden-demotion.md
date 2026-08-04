# #5853 container image identity golden demotion proof

## Result

B-7 now exercises the production digest-v3 support writer across two ordered
publications for the same image reference:

1. `tag_resolved` publishes one current support.
2. `stale_tag` publishes no canonical support and has no completeness warning
   that could hold the prior decision.
3. The writer installs an explicit empty active set, so the prior support stays
   immutable history but is no longer current.

The proof runs after B-7 initializes Postgres and before cassette replay. It is
bounded by a 60-second Go test timeout and reports its own wall time separately
from pipeline phase timings.

## Why this is not a second cassette epoch

The B-12 `list_container_image_identities` assertion still proves that the
authored corpus exposes exactly one current identity. It cannot prove a
transition from canonical to demoted: after #5909, collector settle waits for
every cassette scope before the first reducer drain, so the gate sees the full
static evidence set at once. Splitting that evidence into another cassette
would either be vacuous or reintroduce a start-order race.

The focused Postgres lifecycle is deterministic and uses the writer production
wires in `cmd/reducer`: `PostgresContainerImageIdentitySupportWriter`. A staged
second cassette replay would require new temporal orchestration without adding
stronger protection for this regression.

## BITES mutation

The regression test was run with a deliberate mutation in
`planContainerImageIdentityRetirement`: every noncanonical decision was carried
forward as held support instead of being retired.

```go
if decision.CanonicalWrites <= 0 {
    heldDecisions = append(heldDecisions, decision)
    continue
}
```

Command:

```bash
bash scripts/verify-golden-corpus-gate.sh
```

Result: exit 1 at the new B-7 lifecycle phase, before cassette replay.

```text
demoted current supports = 1, want 0
```

This is the user-visible failure the gate is meant to catch. The test reaches
the durable current-support readback; it does not stop at an internal planner
shape assertion.

The mutation was then removed. `git diff --quiet --
go/internal/reducer/container_image_identity_retirement.go` exited 0, proving
the production file returned byte-for-byte to the branch base.

## Restored proof

Focused verification after restoring production source:

```text
bash scripts/test-verify-golden-corpus-gate.sh
  exit 0

cd go && go test ./internal/reducer ./internal/goldengate ./cmd/golden-corpus-gate -count=1
  exit 0
```

The same live command then passed before review:

```text
container image identity demotion proof completed in 2s
summary: 521 pass, 0 required-fail, 1 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 112s, budget ceiling 1800s)
```

The advisory was the existing shared-runner maintenance-drain timing check:
25 seconds observed against a 19-second advisory ceiling. Every required check
passed, all queues reached their required terminal state, and the MCP
`list_container_image_identities` assertion returned exactly one result.

## Review hardening and final local proof

The preliminary review found that the static mirror's substring check would
still accept the exact Go test command followed by `|| true`. The command is
now anchored as one complete executable line, including the DSN assignment,
package, exact test regex, count, timeout, and JSON output mode. The enclosing
failure branch is anchored too. Committed negative cases prove that the mirror
rejects each of these weakenings:

- a trailing `|| true`;
- a different package;
- a missing timeout; and
- an unanchored test regex.

Go exits zero when `-run` matches no tests, so command exactness alone is not
enough. B-7 now parses the structured Go test event stream and requires exactly
one `run`, exactly one `pass`, and zero `skip` events for the named lifecycle
test. Executable negative cases reject a zero-test success, a skipped test, and
duplicate run events.

The mirror and live-test files were also split before reaching the repository's
500-line limit. Cleanup now reports a failed scope deletion or database close
instead of discarding either error.

Post-hardening verification used one retained B-7 database only long enough to
exercise repeat isolation:

```text
bash scripts/test-verify-golden-corpus-gate.sh
  exit 0

bash scripts/verify-golden-corpus-gate.sh --keep
  container image identity demotion proof completed in 8s
  summary: 521 pass, 0 required-fail, 1 advisory-warn
  PASS: B-7 golden corpus gate green (elapsed 123s, budget ceiling 1800s)

ESHU_POSTGRES_TEST_DSN=... go test ./internal/reducer \
  -run '^TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive$' \
  -count=2 -timeout=60s
  exit 0
```

The retained Compose stack, volumes, and live-gate lock were then removed. The
two consecutive test executions shared the same initialized Postgres and both
completed cleanly, proving that the first run's cleanup did not contaminate the
second.

After adding the structured event guard and extracting backend readiness into
its existing helper, the normal-cleanup B-7 run reported:

```text
=== RUN   TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive
--- PASS: TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive (0.07s)
container image identity demotion proof completed in 3s
summary: 521 pass, 0 required-fail, 1 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 116s, budget ceiling 1800s)
```

## Current-base reconciliation

PR #5914 advanced `main` and changed the B-12 snapshot while this branch was in
final review. The branch was rebased onto that merge, preserving its newer
language-query assertions and reapplying only #5853's container-image identity
description change. The same focused Go packages, static mirror, and strict docs
build passed after the rebase.

The full current-base B-7 run then passed with the exact demotion `run` and
`pass` events, a 3-second demotion probe, `521 pass, 0 required-fail, 1
advisory-warn`, and 116 seconds total. This proves the demotion guard and
#5914's newer golden contract coexist on the actual merge base.

## Cold-cache CI correction

The first PR run exposed a cold-cache-only stream bug in the gate helper. Go
module-download diagnostics were written to stderr before the structured test
events. The helper redirected stderr into the JSON file, so `jq` rejected the
otherwise valid one-run, one-pass event stream with an invalid-literal parse
error.

The helper now writes structured stdout and diagnostic stderr to separate log
files. It renders both for diagnosis, but only the stdout file reaches the event
counter. The static mirror pins the separate redirects and includes a negative
case proving that non-JSON diagnostics mixed back into the structured file fail
loudly.

After that correction, the static mirror passed and the complete local B-7 run
again reported the exact lifecycle test run and pass, `521 pass, 0
required-fail, 1 advisory-warn`, and 118 seconds total.

## Performance and observability

No-Regression Evidence: this changes test and gate work only; it does not alter
production SQL, reducer decisions, queue behavior, graph writes, worker counts,
or runtime settings. The deterministic database probe took 2 seconds in the
warm BITES-restored run, 8 seconds after the test moved to a new file and Go
rebuilt the package, and 3 seconds in the final structured-event run. The final
complete gate took 116 seconds, equal to the immediately preceding #5847 run on
the same local B-7 corpus and topology. The final stream-separation run took 118
seconds, a 2-second (+1.7%) difference from that baseline and well below the
repository's investigation threshold, so no gate wall-time regression was
measured.

No-Observability-Change: production instrumentation is unchanged. Operators
continue to see container image identity decisions and retirement outcomes in
`eshu_dp_container_image_identity_decisions_total` and
`eshu_dp_container_image_identity_retirements_total`, along with the existing
reducer and Postgres duration signals. The gate adds one human-readable probe
duration line for CI diagnosis.
