// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// waitDelay bounds how long Wait keeps reading pipes after the engine
// process is killed, so an orphaned grandchild holding a pipe cannot hang
// the caller.
const waitDelay = 2 * time.Second

// runProcess executes the pinned binary with the fixed argument vector, an
// empty environment, the normalized WAV on stdin, and capped stdout/stderr.
// No shell is involved and no file is written. Any cap breach, deadline, or
// cancellation kills the whole process group.
func (e *Engine) runProcess(ctx context.Context, audio []byte, stats *runStats) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, e.limits.Timeout)
	defer cancel()
	stdout := &cappedBuffer{limit: e.limits.MaxStdoutBytes, onOverflow: cancel, keep: true}
	stderr := &cappedBuffer{limit: e.limits.MaxStderrBytes, onOverflow: cancel}

	// #nosec G204 -- BinaryPath is an operator-pinned, digest-verified
	// absolute path and every argument is fixed at construction; no element
	// derives from media content and no shell is invoked.
	cmd := exec.CommandContext(runCtx, e.cfg.BinaryPath, e.args...)
	cmd.Env = []string{}
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(audio)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		// Start fails with the context error when cancellation or the
		// deadline lands just before exec; report that, not a broken engine.
		if ctxErr := contextError(ctx); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, classified(ClassEngineFailed, "engine process did not start")
	}
	if err := applyAddressSpaceLimit(cmd.Process.Pid, e.limits.MaxAddressSpaceBytes); err != nil {
		cancel()
		_ = cmd.Wait()
		return nil, classified(ClassEngineFailed, "could not apply address-space cap")
	}
	waitErr := cmd.Wait()
	stats.stdoutBytes = stdout.written()
	stats.stderrBytes = stderr.written()
	stats.peakRSSBytes = peakRSSBytes(cmd.ProcessState)

	switch {
	case stdout.overflowed():
		return nil, classified(ClassResourceLimit, "engine stdout exceeds cap")
	case stderr.overflowed():
		return nil, classified(ClassResourceLimit, "engine stderr exceeds cap")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, wrapped(ClassTimeout, "engine wall-clock cap reached", runCtx.Err())
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() >= 0 {
			return nil, classified(ClassEngineFailed, fmt.Sprintf("engine exited with status %d", exitErr.ExitCode()))
		}
		return nil, classified(ClassEngineFailed, "engine terminated abnormally")
	}
	return stdout.bytes(), nil
}

// cappedBuffer counts every byte written, keeps up to limit bytes when keep
// is set, and calls onOverflow once when the limit is crossed. It never
// returns a write error, so the child sees no EPIPE before it is killed.
type cappedBuffer struct {
	limit      int64
	keep       bool
	onOverflow func()

	mu       sync.Mutex
	buf      bytes.Buffer
	total    int64
	overflow atomic.Bool
	once     sync.Once
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.limit - c.total
	c.total += int64(len(p))
	if c.keep && room > 0 {
		c.buf.Write(p[:min(int64(len(p)), room)])
	}
	if c.total > c.limit {
		c.overflow.Store(true)
		c.once.Do(c.onOverflow)
	}
	return len(p), nil
}

func (c *cappedBuffer) overflowed() bool { return c.overflow.Load() }

func (c *cappedBuffer) written() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

func (c *cappedBuffer) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.buf.Bytes())
}
