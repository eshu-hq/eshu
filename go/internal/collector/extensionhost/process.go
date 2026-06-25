// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	defaultStdoutLimitBytes = int64(4 * 1024 * 1024)
	defaultStderrLimitBytes = int64(64 * 1024)
)

// ProcessRunner launches a local collector SDK process with JSON stdin/stdout.
type ProcessRunner struct {
	Command          string
	Args             []string
	Env              []string
	Dir              string
	StdoutLimitBytes int64
	StderrLimitBytes int64
}

// RunCollector starts the configured process and decodes one SDK result.
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
	cmd.Env = append(os.Environ(), r.Env...)
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

	var result sdkcollector.Result
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return sdkcollector.Result{}, fmt.Errorf("decode extension result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return sdkcollector.Result{}, errors.New("decode extension result: trailing JSON value")
		}
		return sdkcollector.Result{}, fmt.Errorf("decode extension result trailer: %w", err)
	}
	return result, nil
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
