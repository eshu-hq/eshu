# Telemetry directory gate waiver (#10124)

At `8fa400413cebe38326872c93972d6a4614cd4251`, the dirgate pre-push
check fails on `go/internal/telemetry` even with no file change in that
directory. The gate reports 68 non-test Go files against 67 waived: one
unwaived file already on the base, so every later pull request inherits
RED.

Commit `41a6c4a121` (PR #10124) added
`go/internal/telemetry/instruments_delta_union_code_call_jvm.go` without
a growth trailer. That file registers the delta-union outcome counters of
the code-call symbol definition read and the JVM reachability read on the
package `Instruments` registry. This wiring belongs with the other
leaf-package instrument registrations in `go/internal/telemetry`; moving
it to a shared package would move registry composition across the
existing package boundary.

The one-commit repair carries exactly `internal/telemetry +1` against
#10124 in its commit trailer (this commit). It changes no Go source, no
cap, and no write-once baselines. The trailer covers the existing
unwaived unit; it grants no allowance for later directory growth.

The repair commit also carries this file so the CI plan has a changed
path to evaluate (`ci-gates pr-plan` fails an empty diff).

## Reproduction and acceptance

From the repair worktree, run:

```bash
ESHU_DIRGATE_BASE=8fa400413ceb \
  bash scripts/dev/precommit-go.sh dirgate-all
```

On the base the gate reports RED (`68 -> 68`, needs `+1`, `+0` from the
change). On the repair commit it reports `0 RED, 0 NOTE` and exits 0.
