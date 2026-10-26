// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// confineChild puts the child in its own process group and makes cancellation
// (a deadline or an interrupt) kill the whole group. Killing only the direct
// child leaves its own children (an ssh under git fetch, a pager under gh)
// running, and they keep the output pipes open after the poll has moved on.
//
// A run from an interactive terminal puts each child in a background process
// group, so a child that reads the terminal (an ssh passphrase prompt; git is
// covered by GIT_TERMINAL_PROMPT=0) is stopped by SIGTTIN until its deadline
// kills it. The intended Monitor or background use has no terminal. Untested
// on a terminal.
func confineChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
