# AGENTS.md — go/internal/collector/repo/git/deadline

## Read first

1. `go/internal/collector/repo/git/deadline/README.md` — surface and signals
2. `go/internal/collector/repo/git/deadline/doc.go` — the package contract
3. `go/internal/runtime/README.md` — `NewProcessGroupCommand`, which this
   package relies on to kill git and its helper processes together

## Rules

- This package is a leaf. `git` imports it; it must never import `git`.
- Build every command from the context `Run` hands to `build`. A command built
  from the parent context gets no deadline.
- Build it with `runtime.NewProcessGroupCommand` (the git package does this
  through `newGitCommand`). `Run` does not create the process group; a command
  without one leaves git-remote-http alive after the kill.
- `Run` sets `cmd.WaitDelay` itself, not the process-group constructor.
  `runtime.NewProcessGroupCommand` leaves it unset because Go applies it after
  a zero exit too and callers would treat `exec.ErrWaitDelay` as a failure;
  `Run` maps `ErrWaitDelay` after a zero exit back to success. Keep that
  mapping.
- A parent-context cancel is a shutdown, not a deadline. Keep the
  `ctx.Err() != nil` check in `Run` ahead of the label, and never count it.
- The `operation` label set is closed (`clone`, `fetch`, `ls_remote`,
  `resolve`); `Run` refuses any other value. Add a value here, in `Limits.For`,
  and in the telemetry docs together.
- A zero limit is the default, never "no deadline". Do not add a way to turn
  the deadline off: that brings back the 300 s fleet pause of #8052.
- `RunRetrying` runs `Command.Build` once per attempt. A caller that shares an
  output buffer across attempts resets it in `Build`; otherwise the classifier
  and the caller read the previous attempt's text or output.
- `Command.BeforeRetry` runs between a failed attempt and its retry, only when
  a retry is going to start. Its error ends the retry, hands the sweep budget
  back, and leaves the original failure final. Use it for cleanup the retry
  needs (a clone removes its partial directory). Only an idempotent command may
  be retried in place.
- The sweep budget is the atomic counter `WithSweep` creates. Keep any other
  retry state local to the call: no package-level or loop-level mutable state,
  and the backoff sleeps inside the operation (`Command.Sleep` and
  `Command.Jitter` are the test seam).
- The class table in `retry_class.go` is closed. The precedence is: the
  remote's answers (rate limit, not found, auth, HTTP 4xx, object integrity)
  are never retried; then a transport line is retried whatever pack lines
  follow it; then pack follow-on lines alone are not retried; unnamed text is
  `git_sync_failure` and is not retried. Add a class with a test row (real
  multi-line stderr, not one line), and a row in the metrics docs together.
