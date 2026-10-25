// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !unix

package main

import "os/exec"

// confineChild is a no-op where process groups are unavailable; cancellation
// kills the direct child only and WaitDelay bounds how long Wait lingers.
func confineChild(cmd *exec.Cmd) {}
