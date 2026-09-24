# Process-group cancel: re-kill (#7066)

`runtime.NewProcessGroupCommand` (`go/internal/runtime/processgroup_unix.go`)
built commands whose group was SIGKILLed once on context cancel. #7066 showed
that is not always enough: the collector git cancellation test
`TestSyncGitRepositoriesPropagatesCancellationDuringListRefs` failed about 5% of
runs under load on darwin/arm64, returning at cancel + the fake git's full sleep
instead of near-immediately.

Root-Cause Evidence: throwaway instrumentation of the `Cancel` func logged the
`syscall.Kill(-pid, SIGKILL)` result and, 300ms later, `ps` for the group. In 9
of 9 failing runs the kill returned nil, the group leader (`sh`) was gone, and a
`sleep` child in the same process group survived with PPID 1
(`97214 97197 1 S sleep 5`). A child being forked at the instant of the kill is
missed by killpg. The orphan holds the command's stdout/stderr pipes (Go creates
pipes for a `bytes.Buffer` writer), so `Cmd.Wait` blocked until it exited on its
own; `WaitDelay` was unset. The cancel fired about 1ms after the marker in every
failing run, so it was not slow spawn or polling. A standalone exec/Setpgid
program never reproduced it (0 of about 2000 iterations); the window only opens
under the `-race` test binary with 8 parallel processes.

## Change

- After the first SIGKILL, `Cancel` probes the group with signal 0 and re-sends
  SIGKILL while it has members, at most 20 times, 5ms apart. It exits at the
  first empty probe, which is the normal case. The loop runs on the
  `exec.Cmd` context watcher goroutine, so 100ms is the worst-case delay of that
  goroutine, and the group id cannot be recycled while any member (including the
  unreaped leader) exists.
- `WaitDelay` stays unset. An earlier revision set it to 2s as a backstop, but
  Go applies it after a successful exit too and returns `exec.ErrWaitDelay`
  when the exit status was 0 (probe: `sh -c 'echo out; sleep 30 & exit 0'`
  returned that error after 2.01s with stdout intact). `cloneRepository` treats
  any error as failure and removes the checkout, so a good clone could be
  deleted. The re-kill loop alone fixes #7066 (below), so the delay was dropped;
  `TestNewProcessGroupCommandKeepsSuccessWhenDescendantHoldsPipes` pins it.
- No new metric or span: the `Cancel` path has no existing counter seam and the
  constructor stays a plain `exec.Cmd` builder, as the telemetry-coverage row for
  it already records.

## Proof

Host: darwin/arm64, 18 CPUs, Go 1.27.1 (go.mod minimum 1.26.6), `go test -c -race`, 8 parallel
processes.

| Case | Runs | Failures |
| --- | --- | --- |
| git cancel test, origin/main a34030491e | 120, 120, 120 | 8, 5, 8 |
| git cancel test, `sleep 30` fake git | 120 | 7 late returns near 30s (plus load-only marker timeouts) |
| git cancel test, re-kill only (throwaway) | 240 | 0 |
| new runtime regression, base code | 24 x 60 rounds | 21 of 24 test runs failed, 63 rounds |
| new runtime regression, fixed (final head, no `WaitDelay`) | 40 x 60 rounds | 0 |
| new runtime regression, re-kill removed (earlier revision, WaitDelay 2s kept) | 24 x 60 rounds | 20 of 24 test runs failed, 71 rounds |
| hardened git cancel tests, fixed (final head, 4 tests) | 320 (4 tests x 8 x 10) | 0 |
| new runtime regression, final head with re-kill loop removed | 24 x 60 rounds | 24 of 24 test runs failed, 112 rounds (group survivor) |
| new runtime regression, base `processgroup_unix.go` (final test file) | 24 x 60 rounds | 24 of 24 test runs failed, 345 rounds |
| exit-0 command with a descendant holding the pipes, before/after dropping `WaitDelay` | 1 | before: `exec.ErrWaitDelay` after 2.01s; after: nil error, stdout intact |
| hardened git cancel tests, base `processgroup_unix.go` | 240 | 3, each 30.0s after cancel |

The regression is
`TestNewProcessGroupCommandKillsChildForkedDuringCancel`
(`go/internal/runtime/processgroup_fork_unix_test.go`): it cancels the instant a
shell that is forking `sleep` writes its marker, requires `Wait` to return within
5s of cancel and requires the process group to be empty. The git tests now block
the fake git for 30s and measure elapsed from `cancel()`, not from test start.

No-Regression Evidence: baseline is origin/main a34030491e where cancel returned at cancel + the fake git sleep in 5 to 8 of 120 stressed runs, and after this change 0 of 320 stressed git runs and 0 of 2400 runtime regression rounds fail; input shape is one fake git that blocks 30s and is cancelled about 1ms after it starts, run on darwin/arm64 with -race; the healthy path adds one 5ms sleep plus one signal-0 probe per cancelled command and nothing on the uncancelled path so it is safe because the loop is bounded to 100ms and exits at the first empty probe.

No-Observability-Change: the change alters how a cancelled child process is killed inside an exec.Cmd builder and emits no signal of its own; operators see cancellation latency through the existing git-sync operation spans and logGitSyncStarted/Completed/Failed structured logs, and a stalled shutdown that previously waited on an orphaned descendant now returns within the bound.
