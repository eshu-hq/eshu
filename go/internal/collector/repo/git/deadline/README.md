# deadline

Per-operation deadlines for the git collector's repository sync (#8052).

## Why

The sync loop in `go/internal/collector/repo/git` runs one repository at a
time. A git command that waits on DNS lasts until libcurl gives up after about
300 s, and the whole fleet sync waits with it. This package gives every
network-capable git command its own deadline so one stalled command costs at
most that deadline.

## Surface

- `Limits` and `Limits.For(operation)`: `Resolve` for `ls_remote` and
  `resolve`, `Fetch` for `fetch`, `Clone` for `clone`. A zero field means the
  default (`DefaultResolve` 60 s, `DefaultFetch` 90 s, `DefaultClone` 600 s),
  never unbounded. An operation outside the closed set is refused.
- `FromEnv`: reads `ESHU_GIT_RESOLVE_TIMEOUT_SECONDS`,
  `ESHU_GIT_FETCH_TIMEOUT_SECONDS` and `ESHU_GIT_CLONE_TIMEOUT_SECONDS`. A
  value that is not a whole number of seconds from 1 to 86400 is a startup
  error naming the variable and the value.
- `Run`: runs a command under its deadline. The caller builds the command with
  `runtime.NewProcessGroupCommand` (Run does not create the process group), so
  the kill reaches git and its helper together. On expiry it returns `*Error` (command, limit, elapsed). A parent-context cancel returns
  the plain process error and counts nothing. `WaitDelay` bounds how long Run
  waits on the output pipes after the kill; a command that exited zero keeps
  its success.
- `Operations`: the closed metric label set `clone`, `fetch`, `ls_remote`,
  `resolve`.
- `Limits.LowSpeedArgs`: the `-c http.lowSpeedLimit=1 -c http.lowSpeedTime=60`
  options for fetch and clone.
- `WithMetrics`: carries the telemetry instruments in the context so every path
  that can hit a deadline counts it once, at the kill.

## Signals

`eshu_dp_git_command_deadline_exceeded_total{operation}`, the
`git.deadline_exceeded` span event on the active span, and the
`git repository sync failed` log line the git package writes with
`failure_class=deadline_exceeded`, `deadline_operation`, and
`deadline_seconds`.

## Bounded retry (#8091)

`RunRetrying` runs one network command under its deadline and retries a
connectivity failure from the closed class table (`Classify`): a deadline kill,
name resolution, connect, connection reset or a transfer closed mid-way, HTTP/2 framing or stream error, TLS handshake, or low-speed abort.
Authentication, HTTP 4xx, pack or object errors, repository not found, rate
limits, and text the table does not name are never retried. Each attempt gets
its own deadline and a jittered backoff of about 3 s. `FromEnv` also reads
`ESHU_GIT_RETRY_ATTEMPTS` (default 2; 1 turns the retry off) and
`ESHU_GIT_RETRY_SWEEP_BUDGET` (default 20), the cap on retries one sweep may
start, drawn from an atomic counter that `WithSweep` creates.

No-Regression Evidence (#8091): baseline origin/main a9b222eb87, where a fetch
killed at its deadline is final and the repository waits for the next sweep
(`TestRealGitFetchRetriesAfterDeadlineAndRecovers` fails there, exit 1). After:
local git, real `git http-backend` over smart HTTP, one repository, a listener
that never answers the first request. With the 2 s test deadline and a 50 ms
backoff the repository syncs in the same sweep (2.46 s, 1 retry,
`repo_sync_failures` 0); with both attempts stalled the sweep takes 4.25 s, ends
with one `git repository sync failed` line (`attempts=2`) and one
`repo_sync_failures` increment. A healthy command pays one attempt and no sleep.
The cost is bounded: attempts (default 2, at most 5) times the deadline per
command, and at most `ESHU_GIT_RETRY_SWEEP_BUDGET` (default 20) retries per
sweep, drawn from an atomic counter, so a provider outage multiplies the sweep
by at most the budget. The race tests run 64 goroutines on one budget of 7 and
assert exactly 7 retries start.

Observability Evidence (#8091): `eshu_dp_git_command_attempts_total`,
`eshu_dp_git_command_retries_total`,
`eshu_dp_git_command_retry_budget_exhausted_total` and
`eshu_dp_git_command_attempt_duration_seconds`, the `git command attempt failed`
WARN and `git command recovered on attempt N` INFO lines, and the
`git.command_retry` span event. The tests read each series and each field from
a recorded run.
