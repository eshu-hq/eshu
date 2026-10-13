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
- Do not set `cmd.WaitDelay`. `runtime.NewProcessGroupCommand` documents why:
  Go applies it after a zero exit too, and the clone path would treat the
  resulting `exec.ErrWaitDelay` as a failure.
- A parent-context cancel is a shutdown, not a deadline. Keep the
  `ctx.Err() != nil` check in `Run` ahead of the label, and never count it.
- The `operation` label set is closed (`clone`, `fetch`, `ls_remote`, `other`).
  Add a value here and in the telemetry docs together.
- A zero limit is the default, never "no deadline". Do not add a way to turn
  the deadline off: that brings back the 300 s fleet pause of #8052.
