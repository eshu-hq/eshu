// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apirecording

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SchemaVersion is the only recording schema version this package reads and
// writes. Bump it (with a migration note) on any breaking change to the
// Recording JSON shape; do not silently change the layout.
const SchemaVersion = "apirecording.v1"

// Transport names the seam an exchange was driven through. The recorder and
// validator are transport-agnostic: every exchange carries the seam it was
// recorded at so a single recording file can mix HTTP-API exchanges (R-8) with
// MCP tool-call exchanges (R-9), both of which dispatch through the same query
// handler mux. A nil/empty Transport defaults to TransportHTTP when recorded.
type Transport string

const (
	// TransportHTTP marks an exchange driven directly against an http.Handler as
	// an HTTP request (the R-8 API/query path).
	TransportHTTP Transport = "http"
	// TransportMCP marks an exchange driven as an MCP tool call that dispatches
	// to the same handler mux (reserved for R-9 #4111; the recording format
	// accepts it today so R-9 reuses this package without a format change).
	TransportMCP Transport = "mcp"
)

// RequestDescriptor is the transport-agnostic description of one request to
// drive against a handler. It is the input the recorder replays and the
// validator re-drives, so it must be fully deterministic: no clocks, no
// randomness, no environment reads. Headers carry the content negotiation that
// selects the response shape (notably the canonical-envelope Accept header), so
// they are part of the recorded contract.
type RequestDescriptor struct {
	// Name is a stable, human-readable label for the exchange. It is the key the
	// validator matches a recorded exchange by, so it must be unique within a
	// recording and stable across re-records.
	Name string `json:"name"`
	// Transport is the seam the request is driven through. Empty defaults to
	// TransportHTTP at record time.
	Transport Transport `json:"transport,omitempty"`
	// Method is the HTTP method (GET, POST, ...). Required.
	Method string `json:"method"`
	// Path is the request path including any query string. Required.
	Path string `json:"path"`
	// Headers are the request headers to set. Use this to opt into the canonical
	// envelope with Accept: application/eshu.envelope+json so the recorded shape
	// is the envelope, not a backward-compat payload.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is the request body sent verbatim. Empty means no body.
	Body string `json:"body,omitempty"`
}

// transport returns the effective transport, defaulting empty to TransportHTTP.
func (d RequestDescriptor) transport() Transport {
	if t := Transport(strings.TrimSpace(string(d.Transport))); t != "" {
		return t
	}
	return TransportHTTP
}

// validate rejects a descriptor that cannot be deterministically driven.
func (d RequestDescriptor) validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("request name is required")
	}
	if strings.TrimSpace(d.Method) == "" {
		return fmt.Errorf("request %q: method is required", d.Name)
	}
	if strings.TrimSpace(d.Path) == "" {
		return fmt.Errorf("request %q: path is required", d.Name)
	}
	switch d.transport() {
	case TransportHTTP, TransportMCP:
	default:
		return fmt.Errorf("request %q: unsupported transport %q", d.Name, d.Transport)
	}
	return nil
}

// RecordedResponse is the canonicalized response captured for one request. Body
// holds the canonical JSON serialization (sorted keys, volatile fields
// collapsed, secrets redacted) so a re-record is byte-identical when the handler
// output is shape-equivalent and a diff highlights only real shape changes.
type RecordedResponse struct {
	// Status is the HTTP status code the handler wrote.
	Status int `json:"status"`
	// Body is the canonical JSON response body. It is stored as a decoded JSON
	// value (object/array) so the recording file is itself a reviewable JSON
	// document rather than an escaped string blob.
	Body any `json:"body"`
}

// Exchange pairs the request descriptor with the canonical response recorded for
// it. It is the unit of the recording and the unit the validator asserts on.
type Exchange struct {
	// Request is the descriptor that was driven.
	Request RequestDescriptor `json:"request"`
	// Response is the canonical response captured for the request.
	Response RecordedResponse `json:"response"`
}

// Recording is the root document persisted to a golden file: a schema version
// plus the ordered set of recorded exchanges. Exchanges are sorted by request
// name on write so the file is stable regardless of input order.
type Recording struct {
	// SchemaVersion must equal SchemaVersion.
	SchemaVersion string `json:"schema_version"`
	// Exchanges is the recorded set, sorted by request name.
	Exchanges []Exchange `json:"exchanges"`
}

// validate rejects a recording that cannot be safely replayed.
func (r Recording) validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %q (want %q)", r.SchemaVersion, SchemaVersion)
	}
	if len(r.Exchanges) == 0 {
		return errors.New("recording must contain at least one exchange")
	}
	seen := make(map[string]struct{}, len(r.Exchanges))
	for i, ex := range r.Exchanges {
		if err := ex.Request.validate(); err != nil {
			return fmt.Errorf("exchange[%d]: %w", i, err)
		}
		if _, dup := seen[ex.Request.Name]; dup {
			return fmt.Errorf("exchange[%d]: duplicate request name %q", i, ex.Request.Name)
		}
		seen[ex.Request.Name] = struct{}{}
	}
	return nil
}

// sortExchanges orders exchanges by request name in place so the persisted file
// is stable regardless of the order requests were supplied to the recorder.
func sortExchanges(exchanges []Exchange) {
	sort.SliceStable(exchanges, func(a, b int) bool {
		return exchanges[a].Request.Name < exchanges[b].Request.Name
	})
}
