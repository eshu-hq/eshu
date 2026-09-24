// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package runtime

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

const (
	// groupKillRetries and groupKillRetryInterval bound the re-kill loop that
	// runs after the first group SIGKILL. The loop runs on exec.Cmd's context
	// watcher goroutine, so its worst case (retries * interval = 100ms) is the
	// most a cancel can delay that goroutine; it exits at the first probe that
	// finds the group empty, which is the normal case.
	groupKillRetries       = 20
	groupKillRetryInterval = 5 * time.Millisecond

	// processGroupWaitDelay is the backstop for Cmd.Wait after the context is
	// done or the child exits: if a descendant still holds the command's
	// stdout/stderr pipes open after this long, Wait force-closes them and
	// returns exec.ErrWaitDelay instead of blocking for that descendant's
	// whole lifetime. Without it a single leaked pipe holder stalls shutdown.
	processGroupWaitDelay = 2 * time.Second
)

// NewProcessGroupCommand builds a cancellable command whose whole process
// tree dies with the context. Subprocesses that fork helper grandchildren
// (collector git's remote-https transport wrapper and shallow-boundary
// rev-list; operator extension hosts) strand those grandchildren when only
// the direct child is killed: orphaned, they reparent to PID 1 and linger
// until something reaps them. Running the child as a process-group leader
// and killing the group on cancel closes that strand path — the orphan
// reaper stays the backstop for the success-path race, not the only defense.
//
// A single killpg is not enough on every kernel: a child the leader is
// forking at the instant of the kill can be missed even though the call
// returns success (observed on darwin; #7066). The survivor is orphaned but
// keeps the group id and the command's stdout/stderr pipes, so Cmd.Wait would
// block until it exited on its own. Cancel therefore re-sends SIGKILL while
// the group still has members (bounded by groupKillRetries), and WaitDelay
// guarantees Wait returns even if a pipe holder somehow outlives the kills.
func NewProcessGroupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- callers pass internally constructed binaries and arguments, never user text
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = processGroupWaitDelay
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		killProcessGroup(cmd.Process.Pid)
		return nil
	}
	return cmd
}

// killProcessGroup SIGKILLs process group pgid, then keeps re-sending SIGKILL
// until a probe (signal 0) reports the group empty or the retry budget is
// spent. A negative pid targets the whole group: the direct child plus any
// helper grandchildren it forked. ESRCH (group already gone) is the common
// outcome and ends the loop; the group id cannot be recycled while any member
// (including the unreaped leader) exists, so the probe never signals an
// unrelated group.
func killProcessGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	for i := 0; i < groupKillRetries; i++ {
		time.Sleep(groupKillRetryInterval)
		if syscall.Kill(-pgid, 0) != nil {
			return
		}
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
