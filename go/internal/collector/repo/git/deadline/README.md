# deadline

Per-command deadlines for the git collector's repository sync (#8052).

## Why

The sync loop in `go/internal/collector/repo/git` runs one repository at a
time. A git command that waits on DNS lasts until libcurl gives up after about
300 s, and the whole fleet sync waits with it. This package gives every git
command its own deadline so one stalled fetch costs at most that deadline.

## Surface

- `Limits` and `Limits.For(subcommand)`: the clone deadline for `clone`, the
  fetch deadline for every other subcommand. A zero field means the default
  (`DefaultFetch` 90 s, `DefaultClone` 600 s), never unbounded.
- `FromEnv`: reads `ESHU_GIT_FETCH_TIMEOUT_SECONDS` and
  `ESHU_GIT_CLONE_TIMEOUT_SECONDS`. A value that is not a whole number of
  seconds from 1 to 86400 is a startup error naming the variable and the value.
- `Run`: runs a command under its deadline. On expiry it returns `*Error`
  (command, limit, elapsed). A parent-context cancel returns the plain process
  error and counts nothing.
- `Operation`: the closed metric label set `clone`, `fetch`, `ls_remote`,
  `other`.
- `WithMetrics`: carries the telemetry instruments in the context so every path
  that can hit a deadline counts it once, at the kill.

## Signals

`eshu_dp_git_fetch_deadline_exceeded_total{operation}`, the
`git.deadline_exceeded` span event on the active span, and the
`git repository sync failed` log line the git package writes with
`deadline_exceeded`, `git_command`, and `deadline_seconds`.
