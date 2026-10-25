// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Command describes one external process the watcher runs: a gh call or a
// local git call. Env is the complete environment for the process, so the
// caller decides which GitHub token the child sees.
type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

// String renders the command for error messages without its environment, so a
// token never reaches a log line.
func (c Command) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// ExitError reports that a process started and exited non-zero. git
// merge-tree uses exit code 1 to mean "conflicts", so callers must be able to
// tell that apart from a process that never started.
type ExitError struct {
	Code   int
	Stderr string
}

// Error implements error.
func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d: %s", e.Code, strings.TrimSpace(e.Stderr))
}

// Exec runs a Command and returns its standard output. A non-zero exit is an
// *ExitError; any other error means the process did not run.
type Exec func(ctx context.Context, c Command) ([]byte, error)

// osExec is the production Exec: it runs the command with os/exec.
func osExec(ctx context.Context, c Command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...) // #nosec G204 -- Name is the operator-chosen gh or git binary; Args are built by this package, never passed through a shell.
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.Bytes(), &ExitError{Code: exitErr.ExitCode(), Stderr: stderr.String()}
	}
	return nil, fmt.Errorf("run %s: %w", c, err)
}
