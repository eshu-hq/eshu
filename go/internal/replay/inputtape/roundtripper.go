// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// Mode selects whether a RoundTripper records live traffic or replays a tape.
type Mode int

const (
	// ModeReplay serves responses from a loaded tape and treats any unmatched
	// request as a hard error. This is the default zero value so a misconfigured
	// RoundTripper never silently reaches the network.
	ModeReplay Mode = iota
	// ModeRecord proxies each request to the wrapped transport, records the
	// redacted request->response pair, and returns the live response.
	ModeRecord
)

// ErrUnmatchedRequest is returned from RoundTrip in ModeReplay when an incoming
// request has no recorded interaction. It is a hard error by design: replay must
// never fall through to the network, so an unrecorded request fails loudly
// instead of silently issuing a live call.
var ErrUnmatchedRequest = errors.New("inputtape: no recorded interaction for request")

// Config configures a RoundTripper. The mode (record vs replay) is selected by
// the constructor — New for record, NewReplayer for replay — not by a field, so
// a zero Config is valid for either.
type Config struct {
	// Transport is the underlying transport a recorder uses to reach the real
	// endpoint. Ignored by a replayer. Nil falls back to http.DefaultTransport.
	Transport http.RoundTripper
	// RedactHeaders adds provider-specific request/response header names to
	// redact, beyond the built-in credential headers.
	RedactHeaders []string
	// RedactQueryParams adds provider-specific URL query parameter names to
	// redact, beyond the built-in credential parameters.
	RedactQueryParams []string
	// VolatileQueryParams names URL query parameters whose value varies per run
	// (a wall-clock timestamp, a nonce) but does not change the semantic
	// response. They are normalized to a fixed sentinel in the request match key
	// so a replay request with a different value still resolves to the recording.
	// The same set MUST be supplied at record and replay time. Many observability
	// collectors stamp time.Now() into start/end query params; without naming
	// those here, their requests would never match on replay.
	VolatileQueryParams []string
}

// RoundTripper is an http.RoundTripper that records HTTP interactions to a tape
// (ModeRecord) or replays them from a tape (ModeReplay). It is safe for
// concurrent use: a sync.Mutex guards the interaction map and the record order,
// and the lock is held only across the map mutation, not across the wrapped
// network round trip.
type RoundTripper struct {
	mode      Mode
	transport http.RoundTripper
	redaction redactionConfig

	mu sync.Mutex
	// interactions maps request key to recorded interaction. In ModeReplay it is
	// the loaded tape; in ModeRecord it accumulates captured interactions and
	// dedups repeat requests to the same key.
	interactions map[string]Interaction
	// order preserves first-seen request-key order so a written tape is stable
	// across runs that issue the same requests in the same order.
	order []string
}

// New builds a RoundTripper in record mode. The returned tripper proxies through
// cfg.Transport (or http.DefaultTransport) and accumulates interactions; call
// WriteTape to persist them. The caller installs it as the Transport of an
// *http.Client (or any seam that accepts an http.RoundTripper / Do-style
// client).
func New(cfg Config) *RoundTripper {
	transport := cfg.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &RoundTripper{
		mode:         ModeRecord,
		transport:    transport,
		redaction:    newRedactionConfig(cfg.RedactHeaders, cfg.RedactQueryParams, cfg.VolatileQueryParams),
		interactions: make(map[string]Interaction),
	}
}

// NewReplayer builds a RoundTripper that serves responses from tape and errors
// on any unmatched request. It performs no network I/O. The same redaction
// config that produced the tape must be supplied so an incoming credentialed
// request reduces to the same (redacted) key the recorder stored.
func NewReplayer(tape Tape, cfg Config) (*RoundTripper, error) {
	if err := tape.validate(); err != nil {
		return nil, fmt.Errorf("inputtape: invalid tape: %w", err)
	}
	if len(tape.Interactions) == 0 {
		return nil, errEmptyTape
	}
	interactions := make(map[string]Interaction, len(tape.Interactions))
	order := make([]string, 0, len(tape.Interactions))
	for _, in := range tape.Interactions {
		interactions[in.RequestKey] = in
		order = append(order, in.RequestKey)
	}
	return &RoundTripper{
		mode:         ModeReplay,
		redaction:    newRedactionConfig(cfg.RedactHeaders, cfg.RedactQueryParams, cfg.VolatileQueryParams),
		interactions: interactions,
		order:        order,
	}, nil
}

// RoundTrip implements http.RoundTripper. In ModeReplay it returns the recorded
// response for the request's key or ErrUnmatchedRequest; it never touches the
// network. In ModeRecord it proxies to the wrapped transport, records the
// redacted pair, and returns the live response.
func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.mode == ModeReplay {
		return rt.replay(req)
	}
	return rt.record(req)
}

// replay resolves req against the loaded tape. An unmatched request is a hard
// error: replay never reaches the network, so a collector that drifts off the
// recorded request set fails loudly instead of silently calling a live endpoint.
func (rt *RoundTripper) replay(req *http.Request) (*http.Response, error) {
	key, _, _, err := requestKey(req, rt.redaction)
	if err != nil {
		return nil, fmt.Errorf("inputtape: build request key: %w", err)
	}
	rt.mu.Lock()
	interaction, ok := rt.interactions[key]
	rt.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s %s (key %s)", ErrUnmatchedRequest, req.Method, req.URL.RequestURI(), key)
	}
	return buildResponse(req, interaction.Response)
}

// record proxies req to the wrapped transport, captures the redacted
// request->response pair, and returns the live response to the caller. The tape
// mutation is guarded by the mutex; the network round trip is not, so concurrent
// requests proceed in parallel and only serialize on the brief map update.
func (rt *RoundTripper) record(req *http.Request) (*http.Response, error) {
	key, recordedReq, restored, err := requestKey(req, rt.redaction)
	if err != nil {
		return nil, fmt.Errorf("inputtape: capture request: %w", err)
	}

	resp, err := rt.transport.RoundTrip(restored)
	if err != nil {
		return nil, fmt.Errorf("inputtape: live round trip: %w", err)
	}

	recordedResp, replayBody, err := captureResponse(resp, rt.redaction)
	if err != nil {
		// Drain/close already handled by captureResponse; surface the error
		// rather than returning a half-read response.
		return nil, fmt.Errorf("inputtape: capture response: %w", err)
	}
	// Hand the caller a response whose body is a fresh reader over the captured
	// bytes so the recording consumed the body without starving the caller.
	resp.Body = replayBody

	rt.mu.Lock()
	if _, exists := rt.interactions[key]; !exists {
		rt.order = append(rt.order, key)
	}
	rt.interactions[key] = Interaction{
		RequestKey: key,
		Request:    recordedReq,
		Response:   recordedResp,
	}
	rt.mu.Unlock()

	return resp, nil
}

// Tape returns the recorded interactions as a Tape, in first-seen request order.
// Safe to call concurrently with RoundTrip; it snapshots under the lock.
func (rt *RoundTripper) Tape(collector string) Tape {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	interactions := make([]Interaction, 0, len(rt.order))
	for _, key := range rt.order {
		interactions = append(interactions, rt.interactions[key])
	}
	return Tape{
		SchemaVersion: CurrentSchemaVersion,
		Collector:     collector,
		Interactions:  interactions,
	}
}

// MarshalCanonical returns the tape serialized in canonical form (sorted object
// keys, stable interaction order) so a committed tape is reviewable in diffs and
// byte-identical when re-recorded from equivalent traffic. Interactions are
// ordered by request_key in the canonical bytes; the in-memory Tape preserves
// first-seen order for human reading, but the on-disk form sorts for stability.
func (rt *RoundTripper) MarshalCanonical(collector string) ([]byte, error) {
	tape := rt.Tape(collector)
	return MarshalTape(tape)
}

// MarshalTape serializes a tape to canonical JSON: object keys sorted and the
// interactions array stably ordered by request_key. The result is deterministic
// for equivalent input so re-recording does not churn the committed file.
func MarshalTape(tape Tape) ([]byte, error) {
	sorted := append([]Interaction(nil), tape.Interactions...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].RequestKey < sorted[j].RequestKey
	})
	tape.Interactions = sorted
	if tape.SchemaVersion == "" {
		tape.SchemaVersion = CurrentSchemaVersion
	}
	// Round-trip through the shared canonical serializer so the on-disk bytes
	// match the rest of the replay framework (sorted keys, no HTML escaping,
	// trailing newline, idempotent).
	intermediate, err := jsonMarshal(tape)
	if err != nil {
		return nil, err
	}
	canonical, err := replay.Canonicalize(intermediate, replay.CanonicalOptions{})
	if err != nil {
		return nil, fmt.Errorf("inputtape: canonicalize tape: %w", err)
	}
	return canonical, nil
}
