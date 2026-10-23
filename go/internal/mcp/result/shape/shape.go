// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resultshape

import (
	"context"
	"fmt"
	"strings"
)

// Mode is the wire layout of a tools/call result.
type Mode string

const (
	// Dual sends the text summary, the envelope as an embedded resource, and
	// structuredContent. It is the default and the layout used before Structured
	// existed.
	Dual Mode = "dual"
	// Structured sends one text summary and structuredContent, with no embedded
	// resource, so the payload is on the wire once.
	Structured Mode = "structured"
)

// Parse maps a raw setting to a Mode. An empty value selects Dual so an unset
// variable keeps today's layout. Matching is case-insensitive; any other value,
// including one with surrounding whitespace, is an error that names the
// allowed values, so a typo cannot silently fall back. Callers that accept
// padded input trim it first.
func Parse(raw string) (Mode, error) {
	switch Mode(strings.ToLower(raw)) {
	case "", Dual:
		return Dual, nil
	case Structured:
		return Structured, nil
	default:
		return "", fmt.Errorf("unknown MCP result shape %q (want dual or structured)", raw)
	}
}

// OrDual returns mode, or Dual when mode is the zero value. Result values that
// predate the shape field carry the zero value and must render as Dual.
func OrDual(mode Mode) Mode {
	if mode == "" {
		return Dual
	}
	return mode
}

type contextKey struct{}

// WithOverride returns ctx carrying mode as the result shape for the request
// being served.
func WithOverride(ctx context.Context, mode Mode) context.Context {
	return context.WithValue(ctx, contextKey{}, mode)
}

// FromContext returns the shape WithOverride stored in ctx, and false when ctx
// carries none.
func FromContext(ctx context.Context) (Mode, bool) {
	mode, ok := ctx.Value(contextKey{}).(Mode)
	return mode, ok && mode != ""
}
