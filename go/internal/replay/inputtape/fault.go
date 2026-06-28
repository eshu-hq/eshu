// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

// Fault-injection types for the R-11 fault-injection tape (Layer 2 of the
// deterministic replay framework, epic #4102).
//
// A Fault is an optional directive on an Interaction that scripted faults to
// inject during replay. Faults are part of the tape, not runtime config, so
// every replay run of the same tape injects the same faults in the same order
// — deterministic by construction, no wall-clock or random elements.
//
// Fault directives must not carry any secret values; they are validated by
// NewReplayer before the tape is accepted. The FaultKindSequence kind supports
// retry-then-succeed scenarios: steps are consumed in order and the real
// recorded response is served once all steps are exhausted.

import (
	"context"
	"errors"
	"fmt"
)

// FaultKind labels the class of fault to inject on a replayed interaction.
type FaultKind string

const (
	// FaultKindTimeout simulates a request that times out before the server
	// sends any response. The replayer returns ErrFaultTimeout (which wraps
	// context.DeadlineExceeded) without sleeping on the wall clock, so tests
	// remain fast and deterministic. Callers that retry on timeout errors will
	// see subsequent interactions according to the normal or sequence rules.
	FaultKindTimeout FaultKind = "timeout"

	// FaultKindPartialBody simulates a truncated response: the replayer returns
	// an http.Response with status 200 and a body that yields exactly
	// Fault.PartialBytes bytes then io.ErrUnexpectedEOF, as if the connection
	// dropped mid-transfer. A PartialBytes value of zero truncates to nothing.
	FaultKindPartialBody FaultKind = "partial_body"

	// FaultKindReset simulates a connection reset: the replayer returns
	// ErrFaultReset (no response), as if the peer closed the connection without
	// sending any bytes. Callers that retry on connection errors proceed to the
	// next step in the sequence or the real response.
	FaultKindReset FaultKind = "reset"

	// FaultKindStatus overrides the recorded response status code with
	// Fault.StatusCode. The response body is empty. Use this to inject 4xx/5xx
	// responses. The real recorded response is NOT served — only the overridden
	// status is returned so the caller's error-handling path is exercised.
	FaultKindStatus FaultKind = "status"

	// FaultKindSequence enables a retry-then-succeed scenario. Each element of
	// Fault.Sequence is a step consumed in order on successive invocations of
	// the interaction; once all steps are exhausted the real recorded response
	// is served for that invocation and all subsequent ones. A step with no
	// Kind (the zero value) is treated as "serve real response" and can be
	// used to insert a success in the middle of a sequence if needed, though
	// typically the exhaustion rule handles this.
	FaultKindSequence FaultKind = "sequence"
)

// validFaultKinds is the set of recognised FaultKind values used by validate.
var validFaultKinds = map[FaultKind]struct{}{
	FaultKindTimeout:     {},
	FaultKindPartialBody: {},
	FaultKindReset:       {},
	FaultKindStatus:      {},
	FaultKindSequence:    {},
}

// ErrFaultTimeout is returned by RoundTrip when a FaultKindTimeout or a
// sequence step with FaultKindTimeout is active. It wraps
// context.DeadlineExceeded so callers that inspect errors.Is(err,
// context.DeadlineExceeded) also match, but callers that specifically test for
// an injected tape fault can use errors.Is(err, ErrFaultTimeout).
var ErrFaultTimeout = fmt.Errorf("inputtape: fault timeout: %w", context.DeadlineExceeded)

// ErrFaultReset is returned by RoundTrip when a FaultKindReset or a sequence
// step with FaultKindReset is active. It represents a connection reset by the
// peer with no response bytes sent.
var ErrFaultReset = errors.New("inputtape: fault connection reset")

// Fault is an optional fault directive attached to an Interaction. When
// present, the replayer injects the described fault instead of (or before)
// serving the real recorded response.
//
// Only one of the fault fields is meaningful per Kind:
//   - FaultKindTimeout: no extra fields.
//   - FaultKindPartialBody: PartialBytes controls how many bytes are served.
//   - FaultKindReset: no extra fields.
//   - FaultKindStatus: StatusCode is the overriding HTTP status code.
//   - FaultKindSequence: Sequence holds the ordered steps.
type Fault struct {
	// Kind is the fault class. Required.
	Kind FaultKind `json:"kind"`

	// PartialBytes is the number of recorded response body bytes to deliver
	// before injecting io.ErrUnexpectedEOF. Only meaningful for
	// FaultKindPartialBody. Zero truncates to nothing (no bytes delivered).
	PartialBytes int `json:"partial_bytes,omitempty"`

	// StatusCode is the HTTP status code to return instead of the recorded one.
	// Only meaningful for FaultKindStatus. Must be a valid HTTP status (100–599).
	StatusCode int `json:"status_code,omitempty"`

	// Sequence is the ordered list of fault steps to inject on successive
	// invocations. Only meaningful for FaultKindSequence. Once all steps are
	// consumed the real recorded response is served for that and all subsequent
	// invocations, implementing the retry-then-succeed pattern without wall-clock
	// dependence.
	Sequence []SequenceStep `json:"sequence,omitempty"`
}

// SequenceStep is one element in a FaultKindSequence. It shares the same
// fields as Fault (Kind, PartialBytes, StatusCode) but does not support nested
// sequences — sequences are one level deep only. A step with a zero Kind
// (empty string) means "serve the real recorded response at this step" and
// can be used to model mid-sequence successes, though exhaustion covers the
// common retry-then-succeed case.
type SequenceStep struct {
	// Kind is the fault to apply at this step. An empty string means: serve the
	// real recorded response.
	Kind FaultKind `json:"kind,omitempty"`

	// PartialBytes is meaningful when Kind is FaultKindPartialBody.
	PartialBytes int `json:"partial_bytes,omitempty"`

	// StatusCode is meaningful when Kind is FaultKindStatus.
	StatusCode int `json:"status_code,omitempty"`
}

// validate checks that f is structurally valid so NewReplayer can reject a
// malformed fault directive before replay starts.
func (f *Fault) validate(interactionIndex int) error {
	if f == nil {
		return nil
	}
	if _, ok := validFaultKinds[f.Kind]; !ok {
		return fmt.Errorf("interaction[%d]: fault: unknown kind %q", interactionIndex, f.Kind)
	}
	switch f.Kind {
	case FaultKindTimeout, FaultKindReset:
		// No extra fields to validate for these kinds.
	case FaultKindPartialBody:
		if f.PartialBytes < 0 {
			return fmt.Errorf("interaction[%d]: fault: partial_bytes must be >= 0, got %d", interactionIndex, f.PartialBytes)
		}
	case FaultKindStatus:
		if f.StatusCode < 100 || f.StatusCode > 599 {
			return fmt.Errorf("interaction[%d]: fault: status_code %d out of range [100,599]", interactionIndex, f.StatusCode)
		}
	case FaultKindSequence:
		if len(f.Sequence) == 0 {
			return fmt.Errorf("interaction[%d]: fault: sequence must have at least one step", interactionIndex)
		}
		for si, step := range f.Sequence {
			if step.Kind == "" {
				// Zero kind = serve real response at this step; valid.
				continue
			}
			if _, ok := validFaultKinds[step.Kind]; !ok {
				return fmt.Errorf("interaction[%d]: fault sequence[%d]: unknown kind %q", interactionIndex, si, step.Kind)
			}
			if step.Kind == FaultKindSequence {
				return fmt.Errorf("interaction[%d]: fault sequence[%d]: nested sequences are not supported", interactionIndex, si)
			}
			if step.Kind == FaultKindStatus && (step.StatusCode < 100 || step.StatusCode > 599) {
				return fmt.Errorf("interaction[%d]: fault sequence[%d]: status_code %d out of range [100,599]", interactionIndex, si, step.StatusCode)
			}
			if step.Kind == FaultKindPartialBody && step.PartialBytes < 0 {
				return fmt.Errorf("interaction[%d]: fault sequence[%d]: partial_bytes must be >= 0, got %d", interactionIndex, si, step.PartialBytes)
			}
		}
	}
	return nil
}
