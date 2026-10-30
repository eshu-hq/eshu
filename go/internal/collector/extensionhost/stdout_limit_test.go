// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Oversize-stdout verdicts (eshu-hq/eshu#9635); the benchmark and operator
// evidence live in README.md next to the other package markers.

package extensionhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// TestLimitedBufferCountsTotalBytesWritten proves the buffer reports every
// byte the child wrote across chunked writes, including bytes discarded past
// the limit, so an oversize failure can name the observed size
// (eshu-hq/eshu#9635).
func TestLimitedBufferCountsTotalBytesWritten(t *testing.T) {
	t.Parallel()

	buffer := newLimitedBuffer(4)
	if n, _ := buffer.Write([]byte("0123456789")); n != 10 {
		t.Fatalf("Write() = %d, want 10", n)
	}
	if !buffer.Truncated() {
		t.Fatal("Truncated() = false, want true")
	}
	if got := buffer.Len(); got != 4 {
		t.Fatalf("Len() = %d, want 4", got)
	}
	if got := buffer.Total(); got != 10 {
		t.Fatalf("Total() = %d, want 10", got)
	}
	// A second chunked write must accumulate, not replace, the total: a
	// `total =` mutant passes the single-write assertion above but reports
	// 10 instead of 20 here.
	if n, _ := buffer.Write([]byte("abcdefghij")); n != 10 {
		t.Fatalf("second Write() = %d, want 10", n)
	}
	if got := buffer.Total(); got != 20 {
		t.Fatalf("Total() after two writes = %d, want 20", got)
	}
}

// oversizeHelper emits helperBytes bytes on stdout when the helper env flag
// matches; the parent drives it through the test binary like the other
// process-runner helpers.
func oversizeHelper(t *testing.T) {
	t.Helper()
	n := 0
	if _, err := fmt.Sscanf(os.Getenv("ESHU_EXTENSIONHOST_PROCESS_OVERBYTES"), "%d", &n); err != nil || n <= 0 {
		t.Fatalf("bad ESHU_EXTENSIONHOST_PROCESS_OVERBYTES: %q", os.Getenv("ESHU_EXTENSIONHOST_PROCESS_OVERBYTES"))
	}
	if _, err := os.Stdout.WriteString(strings.Repeat("x", n)); err != nil {
		t.Fatalf("helper stdout write: %v", err)
	}
	os.Exit(0)
}

// assertTerminalOversize checks the stopgap contract: terminal failure,
// observed + limit byte counts, and remediation names, with no payload echo.
func assertTerminalOversize(t *testing.T, err error, observed, limit int64) {
	t.Helper()
	failure, ok := err.(extensionFailure)
	if !ok {
		t.Fatalf("error type = %T, want extensionFailure", err)
	}
	if !failure.terminal {
		t.Fatal("oversize failure is retryable, want terminal")
	}
	if failure.class != FailureClassLaunchFailure {
		t.Fatalf("failure class = %q, want %q", failure.class, FailureClassLaunchFailure)
	}
	msg := failure.Error()
	for _, want := range []string{
		fmt.Sprintf("observed_bytes=%d", observed),
		fmt.Sprintf("limit_bytes=%d", limit),
		"v1alpha3",
		"stdout_limit_bytes",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("oversize error %q misses %q", msg, want)
		}
	}
	if strings.Contains(msg, strings.Repeat("x", 16)) {
		t.Errorf("oversize error echoes payload bytes: %q", msg)
	}
}

// TestProcessRunnerStdoutLimitFailsTerminal proves an oversize satellite
// result fails the claim terminal with byte counts and remediations instead
// of burning the attempt budget on retryable launch failures (#9635).
func TestProcessRunnerStdoutLimitFailsTerminal(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "oversize" {
		oversizeHelper(t)
		os.Exit(0)
	}

	runner := ProcessRunner{
		Command:          os.Args[0],
		Args:             []string{"-test.run=TestProcessRunnerStdoutLimitFailsTerminal"},
		Env:              []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=oversize", "ESHU_EXTENSIONHOST_PROCESS_OVERBYTES=64"},
		StdoutLimitBytes: 8,
	}
	_, err := runner.RunCollector(context.Background(), Request{})
	if err == nil {
		t.Fatal("RunCollector() error = nil, want terminal oversize failure")
	}
	assertTerminalOversize(t, err, 64, 8)
}

// TestOCIRunnerStdoutLimitFailsTerminal proves the OCI runner carries the
// same terminal verdict on both call paths.
func TestOCIRunnerStdoutLimitFailsTerminal(t *testing.T) {
	t.Parallel()

	newRunner := func() OCIRunner {
		return OCIRunner{
			ImageRef:         ociTestDigest,
			StdoutLimitBytes: 8,
			exec: func(_ context.Context, _ string, _ []string, _ []byte, stdout, _ io.Writer) error {
				_, _ = stdout.Write([]byte(strings.Repeat("y", 64)))
				return nil
			},
		}
	}
	if _, err := newRunner().RunCollector(context.Background(), ociTestRequest()); err == nil {
		t.Fatal("RunCollector() error = nil, want terminal oversize failure")
	} else {
		assertTerminalOversize(t, err, 64, 8)
	}
	if _, err := newRunner().RunPagedCollector(context.Background(), ociTestRequest()); err == nil {
		t.Fatal("RunPagedCollector() error = nil, want terminal oversize failure")
	} else {
		assertTerminalOversize(t, err, 64, 8)
	}
}

// TestProcessRunnerPagedStdoutLimitFailsTerminal proves the paged call path
// carries the same terminal verdict.
func TestProcessRunnerPagedStdoutLimitFailsTerminal(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "oversize" {
		oversizeHelper(t)
		os.Exit(0)
	}

	runner := ProcessRunner{
		Command:          os.Args[0],
		Args:             []string{"-test.run=TestProcessRunnerPagedStdoutLimitFailsTerminal"},
		Env:              []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=oversize", "ESHU_EXTENSIONHOST_PROCESS_OVERBYTES=64"},
		StdoutLimitBytes: 8,
	}
	_, err := runner.RunPagedCollector(context.Background(), Request{})
	if err == nil {
		t.Fatal("RunPagedCollector() error = nil, want terminal oversize failure")
	}
	assertTerminalOversize(t, err, 64, 8)
}

// TestCollectPagesPreservesTerminalRunnerVerdict pins the paged assembler's
// verdict-preservation branch through the real path: a runner error already
// classified terminal (bare and wrapped) must survive Source.collectPages
// instead of degrading to retryable (eshu-hq/eshu#9653 review P1/P2).
func TestCollectPagesPreservesTerminalRunnerVerdict(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		failErr error
	}{
		{"bare", stdoutLimitExceededFailure(8, 64)},
		{"wrapped", fmt.Errorf("satellite call failed: %w", stdoutLimitExceededFailure(8, 64))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner := &scriptRunner{failAt: 0, failErr: tc.failErr}
			source := mustNewSource(t, runner, nil)
			_, _, err := source.NextClaimed(context.Background(), testWorkItem())
			if err == nil {
				t.Fatal("NextClaimed() error = nil, want terminal oversize failure")
			}
			var failure extensionFailure
			if !errors.As(err, &failure) {
				t.Fatalf("error type = %T, want extensionFailure to survive collectPages", err)
			}
			if !failure.TerminalFailure() {
				t.Fatal("oversize failure is retryable after collectPages, want terminal")
			}
			assertTerminalOversize(t, failure, 64, 8)
		})
	}
}
