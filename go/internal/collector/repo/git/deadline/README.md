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
- `Run`: runs a command under its deadline in its process group. On expiry it
  returns `*Error` (command, limit, elapsed). A parent-context cancel returns
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
