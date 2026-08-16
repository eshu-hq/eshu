// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Envelope is the canonical `{data, truth, error}` envelope emitted by
// `eshu first-run --json`. It is the one decode shape for every consumer of a
// saved first-run artifact: the evidence report lifts Data into a full Result
// (restoring Truth), and the onboarding benchmark scores the same fields the
// command already proved. Field types match the emitter exactly, so a corrupt
// or mistyped artifact fails the parse instead of being silently consumed.
type Envelope struct {
	// Data is the machine-readable first-run result.
	Data Result `json:"data"`
	// Truth carries the freshness/completeness/backend labels for the answer.
	// A missing or empty Truth map means the answer lacks provenance and a
	// consumer must not trust it.
	Truth map[string]any `json:"truth"`
	// Error is non-nil when the run failed.
	Error *EnvelopeError `json:"error"`
}

// EnvelopeError is the error object inside the JSON envelope. It preserves the
// human-readable failure cause exactly as first-run reported it.
type EnvelopeError struct {
	// Message is the failure cause preserved by first-run.
	Message string `json:"message"`
}

// ParseEnvelope decodes the canonical `eshu first-run --json` output. It
// returns a descriptive error when the payload is not the expected envelope so
// callers fail loudly rather than silently consuming malformed input.
func ParseEnvelope(raw []byte) (Envelope, error) {
	var env Envelope
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&env); err != nil {
		return Envelope{}, fmt.Errorf("decode first-run envelope: %w", err)
	}
	return env, nil
}
