// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

// Fault injection replay execution (R-11, #4120).
//
// faultedReplay is the production entry point for injecting faults during
// replay. It is called by RoundTripper.replay when an interaction carries a
// non-nil Fault. The caller (replay) still owns the key lookup and unmatched-
// request error; this file owns only the fault-execution path.
//
// Determinism contract: all fault decisions use per-key attempt counters
// stored in RoundTripper.attempts (guarded by the existing mu). No wall-clock
// reads, no random numbers. The same tape replayed by a fresh RoundTripper
// produces the same fault sequence every time.

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// faultedReplay executes the fault directive on interaction for the current
// invocation. attempt is the zero-based call count for this interaction's key.
// It returns the response (possibly faulted) or an error. Returning (nil, err)
// signals a transport-level fault (timeout, reset); returning (resp, nil)
// signals a delivered response (possibly with a faulted body or status).
//
// When the fault is FaultKindSequence and the attempt index falls within the
// sequence, the matching step is executed. Once the sequence is exhausted (or
// if the step Kind is empty), the real recorded response is served.
func faultedReplay(req *http.Request, interaction Interaction, attempt int) (*http.Response, error) {
	f := interaction.Fault
	if f == nil {
		// No fault directive; serve the real response normally.
		return buildResponse(req, interaction.Response)
	}

	switch f.Kind {
	case FaultKindTimeout:
		return nil, ErrFaultTimeout

	case FaultKindReset:
		return nil, ErrFaultReset

	case FaultKindPartialBody:
		return partialBodyResponse(req, interaction.Response, f.PartialBytes)

	case FaultKindStatus:
		return statusOverrideResponse(req, f.StatusCode)

	case FaultKindSequence:
		if attempt < len(f.Sequence) {
			step := f.Sequence[attempt]
			return executeStep(req, interaction.Response, step)
		}
		// Sequence exhausted: serve the real recorded response.
		return buildResponse(req, interaction.Response)

	default:
		// validate() prevents this; surface it clearly if somehow reached.
		return nil, fmt.Errorf("inputtape: unsupported fault kind %q", f.Kind)
	}
}

// executeStep applies a single SequenceStep within a FaultKindSequence.
// A zero Kind means "serve real response at this step."
func executeStep(req *http.Request, recorded RecordedResponse, step SequenceStep) (*http.Response, error) {
	switch step.Kind {
	case "":
		// Zero Kind = serve the real recorded response.
		return buildResponse(req, recorded)
	case FaultKindTimeout:
		return nil, ErrFaultTimeout
	case FaultKindReset:
		return nil, ErrFaultReset
	case FaultKindPartialBody:
		return partialBodyResponse(req, recorded, step.PartialBytes)
	case FaultKindStatus:
		return statusOverrideResponse(req, step.StatusCode)
	default:
		return nil, fmt.Errorf("inputtape: unsupported sequence step kind %q", step.Kind)
	}
}

// partialBodyResponse builds an *http.Response whose body delivers exactly n
// bytes from the recorded response body and then returns io.ErrUnexpectedEOF,
// simulating a connection that dropped mid-transfer. If the recorded body has
// fewer than n bytes, all bytes are delivered and the error is still returned.
// A zero n delivers no bytes before the error.
func partialBodyResponse(req *http.Request, recorded RecordedResponse, n int) (*http.Response, error) {
	raw, err := decodeBody(recorded.Body)
	if err != nil {
		return nil, fmt.Errorf("inputtape: decode recorded body for partial fault: %w", err)
	}
	if n < 0 {
		n = 0
	}
	partial := raw
	if n < len(raw) {
		partial = raw[:n]
	}
	header := make(http.Header, len(recorded.Header))
	for k, vs := range recorded.Header {
		cp := append([]string(nil), vs...)
		// Sort header values to match buildResponse's deterministic replay
		// ordering exactly, so a partial-body fault reconstructs the same header
		// shape a normal replay would for the same recorded response.
		sort.Strings(cp)
		header[http.CanonicalHeaderKey(k)] = cp
	}
	resp := &http.Response{
		StatusCode:    recorded.StatusCode,
		Status:        fmt.Sprintf("%d %s", recorded.StatusCode, http.StatusText(recorded.StatusCode)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          &truncatedReader{r: bytes.NewReader(partial)},
		ContentLength: -1, // unknown: connection will drop mid-body
		Request:       req,
	}
	return resp, nil
}

// statusOverrideResponse builds a minimal *http.Response with the given HTTP
// status code and an empty body. The full recorded response is not served:
// this fault simulates a 4xx/5xx with no payload so the caller's HTTP-error
// handling path is exercised without touching the recorded body.
func statusOverrideResponse(req *http.Request, code int) (*http.Response, error) {
	resp := &http.Response{
		StatusCode:    code,
		Status:        fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(nil)),
		ContentLength: 0,
		Request:       req,
	}
	return resp, nil
}

// truncatedReader wraps a bytes.Reader and appends io.ErrUnexpectedEOF after
// the underlying reader is exhausted. This simulates a connection that closed
// cleanly from the reader's perspective but before the full response was
// transferred — callers that expect more data see the unexpected-EOF error
// rather than a clean io.EOF.
type truncatedReader struct {
	r *bytes.Reader
}

func (tr *truncatedReader) Read(p []byte) (int, error) {
	n, err := tr.r.Read(p)
	if err == io.EOF {
		// Replace clean EOF with unexpected EOF: the transfer was not complete.
		// This handles both the (0, io.EOF) case (exhausted on a subsequent call)
		// and the (n, io.EOF) case (bytes.Reader exhausted in one call when the
		// caller's buffer is large enough to hold all remaining bytes).
		return n, io.ErrUnexpectedEOF
	}
	// bytes.Reader.Read never returns a non-nil, non-EOF error; the only
	// possible return here is (n, nil). Returning nil directly rather than
	// forwarding err avoids wrapcheck flagging an unwrapped external error on
	// a path that is, in practice, unreachable for bytes.Reader.
	return n, nil
}

func (tr *truncatedReader) Close() error { return nil }
