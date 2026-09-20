// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package runtime

import (
	"context"
	"os/exec"
	"syscall"
)

// NewProcessGroupCommand builds a cancellable command whose whole process
// tree dies with the context. Subprocesses that fork helper grandchildren
// (collector git's remote-https transport wrapper and shallow-boundary
// rev-list; operator extension hosts) strand those grandchildren when only
// the direct child is killed: orphaned, they reparent to PID 1 and linger
// until something reaps them. Running the child as a process-group leader
// and killing the group on cancel closes that strand path — the orphan
// reaper stays the backstop for the success-path race, not the only defense.
func NewProcessGroupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- callers pass internally constructed binaries and arguments, never user text
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid targets the whole group: the direct child plus any
		// helper grandchildren it forked. ESRCH (group already gone) is the
		// common exit-race outcome and is safe to ignore.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return nil
	}
	return cmd
}
