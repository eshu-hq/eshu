// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build windows

package runtime

import (
	"context"
	"os/exec"
)

// NewProcessGroupCommand builds a cancellable command. Windows has no
// process-group kill; exec.CommandContext's default kill of the direct child
// applies.
func NewProcessGroupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...) // #nosec G204 -- callers pass internally constructed binaries and arguments, never user text
}
