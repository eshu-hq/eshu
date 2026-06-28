// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apirecording

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/replay"
)

// Options configures how an API recording is canonicalized. The zero value uses
// the API-flavor defaults (DefaultOptions). The fields mirror the replay
// canonical core but are scoped to the response shapes the query handlers emit:
// run-specific fields that would churn a re-record collapse to fixed sentinels,
// and configured secrets redact.
type Options struct {
	// canonical is the underlying canonical-core options the response body is
	// serialized with. It is built from DefaultOptions and extended via
	// WithRedactedKeys; callers configure it through the Options constructors
	// rather than reaching into the core directly.
	canonical replay.CanonicalOptions
}

// DefaultOptions returns the API-flavor canonical defaults. The query response
// envelope carries two run-specific fields that must not churn a re-recorded
// golden: correlation_id (minted per request) and observed_at (a wall-clock
// instant). Both collapse to fixed sentinels so a shape-equivalent response
// re-records byte-identically. No secret keys are configured by default; a
// recorder adds them with WithRedactedKeys for the fields its handler is known
// to carry.
func DefaultOptions() Options {
	base := replay.CanonicalOptions{
		VolatileKeys: map[string]string{
			"observed_at":    replay.SentinelObservedAt,
			"correlation_id": sentinelCorrelationID,
		},
		SecretKeys: map[string]string{},
		Indent:     "  ",
	}
	return Options{canonical: base}
}

// sentinelCorrelationID replaces the per-request correlation id in a recorded
// error envelope. It is a fixed, obviously-synthetic value so a re-record does
// not churn the golden on a field that changes every request.
const sentinelCorrelationID = "canonical-correlation-id"

// WithRedactedKeys returns a copy of o with each named object key marked for
// secret redaction wherever it appears in a recorded response. The receiver is
// not mutated, so a shared DefaultOptions value is safe to extend per call site.
func (o Options) WithRedactedKeys(keys ...string) Options {
	o.canonical = o.canonical.WithRedactedKeys(keys...)
	return o
}

// Record drives each request against h via httptest, canonicalizes the response
// body, and returns the recording. The handler is the real query/API mux with
// stubbed dependencies, so the recorded shapes are the genuine handler output,
// not a re-implementation. Requests may be supplied in any order; the recording
// is sorted by request name so the persisted file is stable. Record is
// deterministic given a deterministic handler: it performs no I/O beyond driving
// the in-process handler.
func Record(h http.Handler, requests []RequestDescriptor, opts Options) (Recording, error) {
	if h == nil {
		return Recording{}, fmt.Errorf("apirecording: handler is nil")
	}
	if len(requests) == 0 {
		return Recording{}, fmt.Errorf("apirecording: no requests to record")
	}
	exchanges := make([]Exchange, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for i, req := range requests {
		if err := req.validate(); err != nil {
			return Recording{}, fmt.Errorf("apirecording: request[%d]: %w", i, err)
		}
		if _, dup := seen[req.Name]; dup {
			return Recording{}, fmt.Errorf("apirecording: duplicate request name %q", req.Name)
		}
		seen[req.Name] = struct{}{}

		resp, err := driveOne(h, req, opts)
		if err != nil {
			return Recording{}, fmt.Errorf("apirecording: request %q: %w", req.Name, err)
		}
		exchanges = append(exchanges, Exchange{Request: req, Response: resp})
	}
	sortExchanges(exchanges)
	return Recording{SchemaVersion: SchemaVersion, Exchanges: exchanges}, nil
}

// driveOne runs a single request through the handler and returns its canonical
// response. The response body is canonicalized so the stored form is stable and
// reviewable; a non-JSON body is an error because the API contract this package
// guards is JSON-shaped.
func driveOne(h http.Handler, req RequestDescriptor, opts Options) (RecordedResponse, error) {
	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}
	httpReq := httptest.NewRequest(req.Method, req.Path, bodyReader)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httpReq)

	canonicalBody, err := canonicalizeResponseBody(rec.Body.Bytes(), opts)
	if err != nil {
		return RecordedResponse{}, err
	}
	return RecordedResponse{Status: rec.Code, Body: canonicalBody}, nil
}

// canonicalizeResponseBody canonicalizes a JSON response body and returns it as
// a decoded JSON value so the recording file embeds it as a readable document.
// It runs the raw body through the shared canonical core (sorted keys, volatile
// collapse, secret redaction) and then decodes the canonical bytes back to a
// value. An empty body is rejected: every recorded query response is a JSON
// document, so an empty body signals a handler that did not run.
func canonicalizeResponseBody(raw []byte, opts Options) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("empty response body; expected a JSON document")
	}
	canonical, err := replay.Canonicalize(raw, opts.canonical)
	if err != nil {
		return nil, fmt.Errorf("canonicalize response body: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(canonical))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode canonical response body: %w", err)
	}
	return value, nil
}
