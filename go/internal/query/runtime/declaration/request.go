// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	// maxRepoIDs bounds the repo_ids filter so the IN list stays a small,
	// plannable parameter rather than a client-sized one.
	maxRepoIDs = 100
	// maxRepoIDLength and maxFilterLength bound each filter value; real
	// repository ids and runtime or source_kind names are far shorter.
	maxRepoIDLength = 512
	maxFilterLength = 128
	// maxRequestBytes caps the request body before decoding.
	maxRequestBytes = 1 << 20
	// maxVersionDigits keeps a version_below component inside int64.
	maxVersionDigits = 9
	maxVersionParts  = 3
)

// listRequest is the wire shape of POST /api/v0/runtime-declarations/query.
// Unknown fields are rejected: a misspelled filter such as "majorbelow" would
// otherwise be ignored and answer with every declaration, which reads as a
// clean fleet.
type listRequest struct {
	Runtime      string   `json:"runtime"`
	VersionBelow string   `json:"version_below"`
	MajorBelow   *int     `json:"major_below"`
	SourceKind   string   `json:"source_kind"`
	RepoIDs      []string `json:"repo_ids"`
	Limit        *int     `json:"limit"`
	Cursor       string   `json:"cursor"`
}

// request is a validated listRequest.
type request struct {
	runtime    string
	sourceKind string
	repoIDs    []string
	// bounds holds the version_below components (major, then optional minor and
	// patch); major_below is the one-component case. Empty means no comparison.
	bounds []int
	limit  int
	cursor *cursorKey
}

// compares reports whether the request asks for a version comparison.
func (r request) compares() bool { return len(r.bounds) > 0 }

// boundText renders the comparison bound as dotted text for the response.
func (r request) boundText() string {
	parts := make([]string, len(r.bounds))
	for i, part := range r.bounds {
		parts[i] = strconv.Itoa(part)
	}
	return strings.Join(parts, ".")
}

// requestError is a 400 caused by the caller; its message is safe to return.
type requestError string

func (e requestError) Error() string { return string(e) }

// parseRequest decodes and validates the request body.
func parseRequest(w http.ResponseWriter, r *http.Request) (request, error) {
	var body listRequest
	if r.Body == nil {
		return request{}, requestError("request body is required")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return request{}, requestError("request body is required")
		}
		return request{}, requestError("invalid JSON request body: " + describeDecodeError(err))
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return request{}, requestError("request body must hold one JSON object")
	}
	return body.validate()
}

// describeDecodeError keeps the decoder's own wording for the cases that help
// a caller fix the request (an unknown field, a wrong type) and stays generic
// otherwise.
func describeDecodeError(err error) string {
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &typeErr):
		return fmt.Sprintf("field %q has the wrong type", typeErr.Field)
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return strings.TrimPrefix(err.Error(), "json: ")
	default:
		return "malformed JSON"
	}
}

func (b listRequest) validate() (request, error) {
	req := request{
		runtime:    strings.TrimSpace(b.Runtime),
		sourceKind: strings.TrimSpace(b.SourceKind),
		limit:      defaultLimit,
	}
	if len(req.runtime) > maxFilterLength || len(req.sourceKind) > maxFilterLength {
		return request{}, requestError(fmt.Sprintf("runtime and source_kind must be at most %d characters", maxFilterLength))
	}
	if b.Limit != nil {
		if *b.Limit < 1 || *b.Limit > maxLimit {
			return request{}, requestError(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
		}
		req.limit = *b.Limit
	}
	repoIDs, err := cleanRepoIDs(b.RepoIDs)
	if err != nil {
		return request{}, err
	}
	req.repoIDs = repoIDs
	if err := b.setBounds(&req); err != nil {
		return request{}, err
	}
	if cursor := strings.TrimSpace(b.Cursor); cursor != "" {
		key, err := decodeCursor(cursor)
		if err != nil {
			return request{}, requestError(err.Error())
		}
		req.cursor = &key
	}
	return req, nil
}

// setBounds resolves major_below or version_below into req.bounds. A version
// comparison is only meaningful inside one runtime (Node 20 and Python 20 are
// unrelated), so it requires the runtime filter.
func (b listRequest) setBounds(req *request) error {
	versionBelow := strings.TrimSpace(b.VersionBelow)
	switch {
	case b.MajorBelow != nil && versionBelow != "":
		return requestError("send major_below or version_below, not both")
	case b.MajorBelow != nil:
		if *b.MajorBelow < 0 {
			return requestError("major_below must be zero or greater")
		}
		req.bounds = []int{*b.MajorBelow}
	case versionBelow != "":
		bounds, err := parseVersionBelow(versionBelow)
		if err != nil {
			return err
		}
		req.bounds = bounds
	}
	if req.compares() && req.runtime == "" {
		return requestError("major_below and version_below compare versions within one runtime, so runtime is required")
	}
	return nil
}

// parseVersionBelow parses "20", "20.10" or "20.10.3" into its components.
// Anything else (a leading v, a range, a fourth component) is rejected rather
// than guessed.
func parseVersionBelow(text string) ([]int, error) {
	parts := strings.Split(text, ".")
	if len(parts) > maxVersionParts {
		return nil, requestError("version_below takes at most major.minor.patch")
	}
	bounds := make([]int, len(parts))
	for i, part := range parts {
		if part == "" || len(part) > maxVersionDigits || strings.TrimLeft(part, "0123456789") != "" {
			return nil, requestError("version_below must be digits separated by dots, such as 20, 20.10 or 20.10.3")
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, requestError("version_below must be digits separated by dots, such as 20, 20.10 or 20.10.3")
		}
		bounds[i] = value
	}
	return bounds, nil
}

// cleanRepoIDs trims, rejects blanks, and de-duplicates while keeping caller
// order.
func cleanRepoIDs(raw []string) ([]string, error) {
	if len(raw) > maxRepoIDs {
		return nil, requestError(fmt.Sprintf("repo_ids accepts at most %d ids", maxRepoIDs))
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > maxRepoIDLength {
			return nil, requestError("repo_ids entries must be non-empty ids of at most 512 characters")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// echo returns the filters as applied, so a caller can confirm what the page
// answers (for example that a version bound was read as 20.10 and not 20).
func (r request) echo() map[string]any {
	filters := map[string]any{}
	if r.runtime != "" {
		filters["runtime"] = r.runtime
	}
	if r.sourceKind != "" {
		filters["source_kind"] = r.sourceKind
	}
	if len(r.repoIDs) > 0 {
		filters["repo_ids"] = r.repoIDs
	}
	if r.compares() {
		filters["version_below"] = r.boundText()
	}
	return filters
}
