// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

const (
	defaultStdoutLimitBytes = int64(4 * 1024 * 1024)
	defaultStderrLimitBytes = int64(64 * 1024)
)

// ProcessRunner launches a local collector SDK process with JSON stdin/stdout.
type ProcessRunner struct {
	Command string
	Args    []string
	Env     []string
	// ScrubEnv names inherited environment variables to withhold from the
	// satellite child. The child receives only its request on stdin plus
	// Env; host credential material (for example the GitHub App private key
	// the token provider mints from) must never reach the child environment,
	// even though the satellite never reads it (eshu-hq/eshu#9256, epic
	// #9182 AC1: no private key in satellite env — presence, not use).
	// Names match exactly. Env entries are applied after scrubbing, so
	// explicit operator config still wins over an inherited value.
	ScrubEnv         []string
	Dir              string
	StdoutLimitBytes int64
	StderrLimitBytes int64
}

// RunCollector starts the configured process and decodes one SDK result.
// It keeps the legacy strict decode: paged v1alpha3 output fails closed
// here, and paged satellites run through RunPagedCollector instead.
func (r ProcessRunner) RunCollector(ctx context.Context, request Request) (sdkcollector.Result, error) {
	if r.Command == "" {
		return sdkcollector.Result{}, errors.New("extension command is required")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return sdkcollector.Result{}, fmt.Errorf("encode extension request: %w", err)
	}

	cmd := exec.CommandContext(ctx, r.Command, r.Args...) // #nosec G204 -- runs operator-configured extension command with operator-supplied args from ProcessRunner config
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(scrubbedEnviron(os.Environ(), r.ScrubEnv), r.Env...)
	cmd.Dir = r.Dir

	stdout := newLimitedBuffer(effectiveLimit(r.StdoutLimitBytes, defaultStdoutLimitBytes))
	stderr := newLimitedBuffer(effectiveLimit(r.StderrLimitBytes, defaultStderrLimitBytes))
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return sdkcollector.Result{}, ctx.Err()
		}
		return sdkcollector.Result{}, fmt.Errorf(
			"extension process failed: %w (stderr_bytes=%d stderr_truncated=%t)",
			err,
			stderr.Len(),
			stderr.Truncated(),
		)
	}
	if stdout.Truncated() {
		return sdkcollector.Result{}, fmt.Errorf(
			"extension stdout limit exceeded: limit_bytes=%d",
			stdout.Limit(),
		)
	}

	return decodeSDKResult(stdout.Bytes())
}

// RunPagedCollector starts the configured process and decodes one SDK result
// with its optional v1alpha3 page continuation, so the host can reassemble
// split results across resumed calls. Legacy single-shot output decodes with
// a nil page.
func (r ProcessRunner) RunPagedCollector(ctx context.Context, request Request) (CallResult, error) {
	if r.Command == "" {
		return CallResult{}, errors.New("extension command is required")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return CallResult{}, fmt.Errorf("encode extension request: %w", err)
	}

	cmd := exec.CommandContext(ctx, r.Command, r.Args...) // #nosec G204 -- runs operator-configured extension command with operator-supplied args from ProcessRunner config
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(scrubbedEnviron(os.Environ(), r.ScrubEnv), r.Env...)
	cmd.Dir = r.Dir

	stdout := newLimitedBuffer(effectiveLimit(r.StdoutLimitBytes, defaultStdoutLimitBytes))
	stderr := newLimitedBuffer(effectiveLimit(r.StderrLimitBytes, defaultStderrLimitBytes))
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return CallResult{}, ctx.Err()
		}
		return CallResult{}, fmt.Errorf(
			"extension process failed: %w (stderr_bytes=%d stderr_truncated=%t)",
			err,
			stderr.Len(),
			stderr.Truncated(),
		)
	}
	if stdout.Truncated() {
		return CallResult{}, fmt.Errorf(
			"extension stdout limit exceeded: limit_bytes=%d",
			stdout.Limit(),
		)
	}

	return decodePagedSDKResult(stdout.Bytes())
}

// scrubbedEnviron returns environ without the named entries. Names match
// exactly (a scrubbed prefix never removes longer names); blank entries are
// ignored. The returned slice is fresh except when there is nothing to drop,
// in which case environ is returned as-is (read-only use).
func scrubbedEnviron(environ, scrub []string) []string {
	drop := make(map[string]struct{}, len(scrub))
	for _, name := range scrub {
		if name = strings.TrimSpace(name); name != "" {
			drop[name] = struct{}{}
		}
	}
	if len(drop) == 0 {
		return environ
	}
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		if name, _, _ := strings.Cut(entry, "="); name != "" {
			if _, remove := drop[name]; remove {
				continue
			}
		}
		kept = append(kept, entry)
	}
	return kept
}

func effectiveLimit(value int64, defaultValue int64) int64 {
	if value <= 0 {
		return defaultValue
	}
	return value
}

type limitedBuffer struct {
	limit     int64
	buffer    bytes.Buffer
	truncated bool
}

func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(payload []byte) (int, error) {
	if b.limit <= 0 {
		b.truncated = true
		return len(payload), nil
	}
	remaining := int(b.limit) - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(payload), nil
	}
	if len(payload) > remaining {
		b.truncated = true
		_, _ = b.buffer.Write(payload[:remaining])
		return len(payload), nil
	}
	_, _ = b.buffer.Write(payload)
	return len(payload), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

func (b *limitedBuffer) Len() int {
	return b.buffer.Len()
}

func (b *limitedBuffer) Limit() int64 {
	return b.limit
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}
