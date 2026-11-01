// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// waitDelay bounds how long Wait keeps reading pipes after the process
	// is killed, so a grandchild holding stdout can not stall a run.
	waitDelay = 2 * time.Second
	// rssSampleInterval paces VmHWM reads while the child runs. VmHWM is a
	// high-water mark, so a late sample still sees an earlier peak; only
	// growth in the final interval before exit can be missed.
	rssSampleInterval = 25 * time.Millisecond
)

// runResult is the bounded evidence of one subprocess run.
type runResult struct {
	stdout      []byte
	stderrBytes int64
	peakRSS     int64
	started     bool
	signal      string
}

// cappedBuffer keeps at most limit bytes. On overflow it records the
// condition and cancels the run instead of blocking or growing.
type cappedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	keep     bool
	limit    int64
	total    int64
	overflow bool
	cancel   context.CancelFunc
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += int64(len(p))
	if b.total > b.limit {
		if !b.overflow {
			b.overflow = true
			b.cancel()
		}
		return 0, errors.New("output cap exceeded")
	}
	if b.keep {
		b.buf.Write(p)
	}
	return len(p), nil
}

func (b *cappedBuffer) state() (bool, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.overflow, b.total
}

// run executes the pinned binary once: no shell, no PATH, a scrubbed
// environment, a private working directory (os.MkdirTemp creates it 0700)
// removed on every exit path, image bytes on stdin, capped stdout and stderr,
// RLIMIT_AS on the child, and the run deadline. Stderr content is counted and discarded; it is never
// returned or logged.
func (e *Engine) run(ctx context.Context, body []byte) (runResult, error) {
	scratch, err := os.MkdirTemp(e.cfg.ScratchDir, "eshu-ocr-")
	if err != nil {
		return runResult{}, failure(OutcomeInternal, "create scratch dir", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	runCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	stdout := &cappedBuffer{keep: true, limit: e.cfg.MaxOutputBytes, cancel: cancel}
	stderr := &cappedBuffer{limit: e.cfg.MaxStderrBytes, cancel: cancel}

	cmd := exec.CommandContext(runCtx, e.cfg.BinaryPath, e.args...) // #nosec G204 -- digest-pinned absolute binary with a fixed argument vector
	cmd.Dir = scratch
	cmd.Env = []string{
		"OMP_THREAD_LIMIT=1",
		"LC_ALL=C",
		"HOME=" + scratch,
		"TMPDIR=" + scratch,
	}
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	configureProcess(cmd)

	e.execCount.Add(1)
	if err := cmd.Start(); err != nil {
		return runResult{}, classifyStartError(ctx, runCtx, err)
	}
	result := runResult{started: true}
	if err := limitAddressSpace(cmd.Process.Pid, e.cfg.MaxAddressSpaceBytes); err != nil {
		cancel()
		_ = cmd.Wait()
		return result, failure(OutcomeInternal, "apply address-space limit", err)
	}
	var peak atomic.Int64
	sampled := make(chan struct{})
	stopSampling := make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(rssSampleInterval)
		defer ticker.Stop()
		for {
			value := sampleHighWaterRSS(cmd.Process.Pid)
			if value > peak.Load() {
				peak.Store(value)
			}
			if e.onRSSSample != nil {
				e.onRSSSample(value)
			}
			select {
			case <-stopSampling:
				return
			case <-ticker.C:
			}
		}
	}()
	waitErr := cmd.Wait()
	close(stopSampling)
	<-sampled
	result.peakRSS = peak.Load()
	result.signal = terminationSignal(cmd.ProcessState)

	stdoutOverflow, _ := stdout.state()
	stderrOverflow, stderrTotal := stderr.state()
	result.stderrBytes = stderrTotal
	switch {
	case ctx.Err() != nil:
		return result, failure(OutcomeCanceled, "caller context ended", ctx.Err())
	case stdoutOverflow:
		return result, failure(OutcomeOutputOversize, fmt.Sprintf("stdout exceeded %d bytes", e.cfg.MaxOutputBytes), nil)
	case stderrOverflow:
		return result, failure(OutcomeOutputOversize, fmt.Sprintf("stderr exceeded %d bytes", e.cfg.MaxStderrBytes), nil)
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return result, failure(OutcomeTimeout, fmt.Sprintf("exceeded %s", e.cfg.Timeout), runCtx.Err())
	case result.signal != "":
		return result, failure(OutcomeSignaled, "terminated by "+result.signal, nil)
	case waitErr != nil:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return result, failure(OutcomeExecFailed, fmt.Sprintf("exit code %d", exitErr.ExitCode()), nil)
		}
		return result, failure(OutcomeExecFailed, "wait", waitErr)
	}
	result.stdout = stdout.buf.Bytes()
	return result, nil
}

// classifyStartError classifies a cmd.Start failure the way the running case
// is classified. exec.Cmd.Start returns the context error when the context
// ends before the process starts, so a caller that gave up is canceled and an
// expired run deadline is a timeout; only a start failure with both contexts
// still live (missing or non-executable binary, fork failure) is exec_failed.
func classifyStartError(ctx, runCtx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return failure(OutcomeCanceled, "caller context ended before start", ctx.Err())
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return failure(OutcomeTimeout, "run deadline passed before start", runCtx.Err())
	}
	return failure(OutcomeExecFailed, "start", err)
}
