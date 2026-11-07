# #10306: Retryable Classification for Rebind ActiveGeneration Lookup Failure

Port of `ba5d1d3192` onto `origin/main` (`7f3a3e526a`). The #10106 rebind
looks up the scope's active generation during `Runtime.execute`; when that
lookup itself fails transiently (database or backend error), the old code
returned a plain `fmt.Errorf` wrap. The queue Fail path treats a
non-retryable error as terminal, so a transient blip dead-lettered the
intent. The port returns `GenerationRebindLookupError` instead: retryable,
self-classified as `generation_activation_not_ready` (the enrolled
non-counting class, shared with `GenerationRebindExhaustedError`), still
unwrapping to the lookup cause. Three files, matching the source commit:
`go/internal/reducer/contract/generation_check.go` (new error type),
`go/internal/reducer/runtime.go` (return it on the lookup-error branch),
`go/internal/reducer/generation_rebind_test.go` (assert retryable and
class on top of the existing wrapped-cause assertion).

## No-Regression Evidence: statement-identical hot path, type-only change

Baseline (`7f3a3e526a`): the lookup-error branch runs one
`ActiveGeneration` primary-key probe and returns its error wrapped.
After: the same single probe runs on the same branch (reachable only
when the probe already failed); the only delta is which error value is
constructed — one struct value carrying `ScopeID`/`GenerationID`/`Err`
instead of one `fmt.Errorf` wrap. No SQL text changed, no new probe, no
claim/lease/batch/queue knob changed, no lock order or statement count
changed. The success path and every non-error branch are untouched.

Measured: `go test ./internal/reducer/ ./internal/reducer/contract/
-count=1` exits 0 at this head (reducer package ~6s, contract package
instant). The ported `TestRuntimeExecuteRebindLookupErrorPropagates`
fails without the fix (plain wrap is not retryable: `want retryable: a
transient lookup failure must not dead-letter`) and passes with it, so
the behavior delta is pinned, not assumed. Absolute latency or
production-scale queue impact is NOT_CHECKED: there is no new I/O to
measure, so no latency claim is made.

## Observability Evidence: no new metric, existing retry accounting

No new metric, span, or log was added. The new error's `FailureClass()`
returns the already-enrolled `generation_activation_not_ready` string,
which `reducerExecutionFailureClass` already maps onto the existing
`eshu_dp_reducer_retry_surge_total` counter's `failure_class` label —
the same accounting the churn-exhaustion error already rides. A
transient lookup failure therefore shows up as a scheduled retry under
that class instead of a silent dead-letter; an operator watching retry
surge by failure class sees it with no new dashboard needed.
Unwrap compatibility (`errors.Is`/`errors.As` to the lookup cause) is
covered by the pre-existing wrapped-cause assertion the port kept.
