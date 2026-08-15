// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// WriteArtifact sends the rendered packet to stdout, or to the file named by
// out when it is non-empty. Writing to a file also prints a one-line
// confirmation to stderr, so a packet redirected to disk still leaves a trace
// in the operator's terminal.
//
// The file is always owner-only. os.WriteFile applies its mode only when it
// creates the file, so regenerating an artifact over an existing world-readable
// file would otherwise leave the old, broader permissions in place; the
// explicit Chmod closes that.
//
// The caller resolves the two writers (in the CLI, cmd.OutOrStdout and
// cmd.ErrOrStderr) so this function has no cobra dependency.
func WriteArtifact(stdout, stderr io.Writer, out string, data []byte) error {
	if strings.TrimSpace(out) == "" {
		_, err := stdout.Write(data)
		// Unwrapped: on a broken pipe the operator should read the writer's own
		// message, not a prefix invented here.
		return err //nolint:wrapcheck
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return fmt.Errorf("write investigation packet: %w", err)
	}
	if err := os.Chmod(out, 0o600); err != nil {
		return fmt.Errorf("set investigation packet permissions: %w", err)
	}
	_, _ = fmt.Fprintf(stderr, "wrote investigation packet to %s\n", out)
	return nil
}
