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
	"time"
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

// Short names the command and its leading subcommand words ("gh api graphql",
// "git fetch") without flags or values, so a timeout line stays readable and
// never carries a query or a token.
func (c Command) Short() string {
	words := []string{c.Name}
	for _, a := range c.Args {
		if strings.HasPrefix(a, "-") || len(words) >= 4 {
			break
		}
		words = append(words, a)
	}
	return strings.Join(words, " ")
}

const (
	defaultGHTimeout  = 60 * time.Second
	defaultGitTimeout = 120 * time.Second
	// killGrace is how long a cancelled child may keep its pipes open before
	// Wait gives up on it, so a grandchild (ssh) cannot hold the poll loop.
	killGrace = 5 * time.Second
)

// withTimeouts bounds every gh and git child process. A call that outlives its
// bound returns an error that names the command; the poll then moves on. A
// cancellation of the parent context is not a timeout and is returned as is.
func withTimeouts(next Exec, cfg Config) Exec {
	return func(ctx context.Context, c Command) ([]byte, error) {
		limit := time.Duration(0)
		switch c.Name {
		case cfg.GH:
			limit = cfg.GHTimeout
		case cfg.Git:
			limit = cfg.GitTimeout
		}
		if limit <= 0 {
			return next(ctx, c)
		}
		child, cancel := context.WithTimeout(ctx, limit)
		defer cancel()
		out, err := next(child, c)
		if ctx.Err() == nil && errors.Is(child.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("timed out after %s: %s", limit, c.Short())
		}
		return out, err
	}
}

// gitEnv is the environment for a git child: it must never stop to ask for a
// credential, because nobody is at the terminal.
func gitEnv(environ []string) []string {
	return append(append([]string(nil), environ...), "GIT_TERMINAL_PROMPT=0")
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
	cmd.WaitDelay = killGrace
	confineChild(cmd)
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
	return nil, fmt.Errorf("run %s: %w", c.Short(), err)
}
