// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"errors"
	"fmt"
	"strings"
)

// CurrentSchemaVersion is the only tape schema version this package reads and
// writes. Bump it only with a migration path; the loader rejects any other
// value so a stale or future tape fails loudly rather than replaying garbage.
const CurrentSchemaVersion = "1"

// Tape is the root document of an input-tape JSON file. It records HTTP
// request->response interactions captured at the http.RoundTripper boundary so
// a collector can be replayed with no network access and no live credentials.
//
// The document is written canonically (sorted object keys, redacted secrets) by
// the recorder so a committed tape is stable, reviewable in diffs, and safe to
// store in the repository.
type Tape struct {
	// SchemaVersion must equal CurrentSchemaVersion.
	SchemaVersion string `json:"schema_version"`
	// Collector names the collector kind the tape was recorded against (e.g.
	// "loki"). Informational only; replay does not validate it so any client
	// wired to the same endpoints can replay the tape.
	Collector string `json:"collector,omitempty"`
	// Interactions is the ordered list of recorded request->response pairs. The
	// order is the order of first recording; replay matches by RequestKey, not by
	// position, so a collector that issues requests in a different order still
	// resolves each one.
	Interactions []Interaction `json:"interactions"`
}

// Interaction is one recorded request->response pair. RequestKey is the
// deterministic match key derived from the (redacted) request; on replay an
// incoming request is reduced to its key and matched against this field.
type Interaction struct {
	// RequestKey is the deterministic match key for the request. It is a stable
	// hash of method, path, sorted query, and (when present) the canonicalized
	// request body, computed after redaction so a secret never participates in
	// the key. See requestKey.
	RequestKey string `json:"request_key"`
	// Request is the recorded (redacted) request, kept for human review of the
	// tape. It is not used to match on replay; RequestKey is.
	Request RecordedRequest `json:"request"`
	// Response is the recorded (redacted) response served on a key match.
	Response RecordedResponse `json:"response"`
}

// RecordedRequest is the redacted, canonical view of a captured request. It
// exists for tape reviewability; matching uses Interaction.RequestKey.
type RecordedRequest struct {
	// Method is the HTTP method (e.g. "GET").
	Method string `json:"method"`
	// Path is the request URL path.
	Path string `json:"path"`
	// Query is the URL query with each value list sorted; secret query
	// parameters are redacted to the redaction sentinel.
	Query map[string][]string `json:"query,omitempty"`
	// Header is the request headers with each value list sorted; secret-bearing
	// headers (Authorization and friends) are redacted.
	Header map[string][]string `json:"header,omitempty"`
	// Body is the recorded request body. Empty when the request had no body.
	Body RecordedBody `json:"body,omitempty"`
}

// RecordedResponse is the redacted view of a captured response, holding enough
// to reconstruct an *http.Response on replay.
type RecordedResponse struct {
	// StatusCode is the HTTP status code (e.g. 200).
	StatusCode int `json:"status_code"`
	// Header is the response headers with each value list sorted; secret-bearing
	// headers are redacted.
	Header map[string][]string `json:"header,omitempty"`
	// Body is the recorded response body. Empty when the response had no body.
	Body RecordedBody `json:"body,omitempty"`
}

// BodyEncoding labels how a RecordedBody.Data is stored.
type BodyEncoding string

const (
	// BodyEncodingNone marks an absent body (no bytes were read).
	BodyEncodingNone BodyEncoding = ""
	// BodyEncodingJSON marks a body stored as canonicalized JSON text. The Data
	// field holds the canonical JSON string so the tape stays reviewable.
	BodyEncodingJSON BodyEncoding = "json"
	// BodyEncodingText marks a UTF-8, non-JSON body stored verbatim as text.
	BodyEncodingText BodyEncoding = "text"
	// BodyEncodingBase64 marks a non-UTF-8 (opaque/binary) body stored base64.
	BodyEncodingBase64 BodyEncoding = "base64"
)

// RecordedBody is a captured request or response body. Encoding selects how
// Data is interpreted when reconstructing the raw bytes on replay.
type RecordedBody struct {
	// Present is true when a body was captured (even an empty one with a content
	// length of zero is recorded as not present; only read bytes count).
	Present bool `json:"present,omitempty"`
	// Encoding labels how Data holds the bytes.
	Encoding BodyEncoding `json:"encoding,omitempty"`
	// Data holds the body: canonical JSON text, verbatim UTF-8 text, or a base64
	// string, per Encoding.
	Data string `json:"data,omitempty"`
}

// validate checks structural invariants the recorder must have produced and the
// replayer relies on. A malformed tape fails here rather than silently serving a
// wrong or empty response.
func (t Tape) validate() error {
	if t.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %q (want %q)", t.SchemaVersion, CurrentSchemaVersion)
	}
	seen := make(map[string]struct{}, len(t.Interactions))
	for i, in := range t.Interactions {
		if strings.TrimSpace(in.RequestKey) == "" {
			return fmt.Errorf("interaction[%d]: request_key is required", i)
		}
		if _, dup := seen[in.RequestKey]; dup {
			return fmt.Errorf("interaction[%d]: duplicate request_key %q", i, in.RequestKey)
		}
		seen[in.RequestKey] = struct{}{}
		if in.Response.StatusCode == 0 {
			return fmt.Errorf("interaction[%d]: response.status_code is required", i)
		}
	}
	return nil
}

// errEmptyTape reports a tape with no interactions, which is almost always a
// recording mistake; replaying it can only ever produce unmatched-request
// errors.
var errEmptyTape = errors.New("input tape contains no interactions")
