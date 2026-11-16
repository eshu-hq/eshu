# NVD baseline trip line: fresh quiet hours before re-enable (#10760)

This change makes the baseline trip line refuse an early re-enable. The trip
state in `go/internal/coordinator/vulnerability/nvd_baseline_tripline.go` resets
its quiet count on the sample that opens a trip, withdraws eligibility when
backlog age is at or past 48 hours, and `Close` returns one of three results
(cleared, refused, nothing to close). The telemetry package registers one more
transition label value, `close_refused` (in `instruments_nvd_baseline.go`, with
a comment update in `instruments.go`, which is what the perf-evidence gate
flags).
Nothing in production calls the trip state yet; the baseline lane is wired in
#10746, so no runtime path reaches this code today.

## Performance Evidence

No benchmark was run. The trip state is a small value type: each `Observe` does
a few integer comparisons and each `Close` reads two fields, with no allocation,
query, lock or I/O. The one runtime-visible edit is a new constant in the
existing transition label set; it adds no new instrument and no new label key.

## No-Regression Evidence

No-Regression Evidence: the committed unit tests pin the contract: six fresh
quiet hours are required after the opening sample (five refuse, six clear), the
opening sample does not count as quiet, a sample at 48 hours withdraws
eligibility and the boundary is `<`, and a latch that is already cleared or was
never open returns nothing to close. An independent review ran eight mutants
against the state machine (reset on open dropped, reset on backlog dropped,
`Close` ignoring eligibility, threshold 6 to 5, `<` to `<=`, not-open refusing,
cleared keeping state, opening hour counted as quiet) and each failed a test.
The telemetry registration test pins the new `close_refused` label.

## Observability Evidence

Observability Evidence: `close_refused` is a new value of the existing
transition label on the baseline trip counter, so cardinality stays closed. No
emitter exists yet; the future caller records it when `Close` returns refused.
The telemetry-coverage row and the instrument description name it, and
`scripts/verify-telemetry-coverage.sh` agrees. When the caller is wired in
#10746 it should also log the refusal with the quiet-sample count, because the
counter alone does not say how many hours remain.

## Not checked

- Runtime behavior: the trip state has no production caller until #10746.
- The durable latch and the inflow measure are tracked in #10768 and #10767.
