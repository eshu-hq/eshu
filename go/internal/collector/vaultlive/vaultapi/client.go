// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
)

// Adapter is a read-only, metadata-only Vault REST client implementing
// vaultlive.Client. It holds a short-lived token supplied by the caller; Eshu
// never persists it.
type Adapter struct {
	httpClient     *http.Client
	address        string
	token          string
	namespace      string
	onAPICall      func(operation, result string)
	lastHTTPStatus int
}

// recordAPICall reports one Vault API operation outcome to the optional
// observer. operation is a bounded enum (the list-method name); result is
// "success", "timeout", "auth_error", "not_found", or "transport_error". It
// carries no path, token, or address.
func (a *Adapter) recordAPICall(operation string, err error) {
	if a.onAPICall == nil {
		return
	}
	if err != nil {
		a.onAPICall(operation, classifyError(err))
		return
	}
	if a.lastHTTPStatus == http.StatusNotFound {
		a.onAPICall(operation, "not_found")
		a.lastHTTPStatus = 0
		return
	}
	a.onAPICall(operation, classifyError(nil))
}

// Config configures the adapter. Address is the Vault API base (for example
// https://vault.example.com:8200). Token is a short-lived read-only token bound
// to the secrets/IAM read-only policy. Namespace is optional (Vault Enterprise).
type Config struct {
	Address    string
	Token      string
	Namespace  string
	HTTPClient *http.Client
	// OnAPICall, if set, is invoked once per list operation with a bounded
	// operation enum and a "success"/"error" result. It carries no secret,
	// path, token, or address — only the operation label and outcome.
	OnAPICall func(operation, result string)
}

// New creates a metadata-only Vault adapter. It does not validate the token; the
// first call surfaces an auth error.
func New(cfg Config) (*Adapter, error) {
	baseURL, err := sdk.ParseBaseURL("vault", cfg.Address)
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("vault base_url scheme must be http or https")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("vault token is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = sdk.DefaultHTTPClient(30 * time.Second)
	}
	return &Adapter{
		httpClient: client,
		address:    strings.TrimRight(baseURL.String(), "/"),
		token:      strings.TrimSpace(cfg.Token),
		namespace:  strings.TrimSpace(cfg.Namespace),
		onAPICall:  cfg.OnAPICall,
	}, nil
}

// errForbiddenDataPath is returned if any request would touch a KV data path —
// the in-code guarantee that the adapter never reads a secret value.
var errForbiddenDataPath = fmt.Errorf("vaultapi: refusing to request a KV /data/ path (metadata-only)")

// errForbiddenPathTraversal is returned if a constructed path contains a "."
// or ".." segment, which a Vault-returned key name could otherwise use to
// escape the metadata subtree.
var errForbiddenPathTraversal = fmt.Errorf("vaultapi: refusing a path with a traversal segment")

// doRequest issues one GET against /v1/<path>, optionally as a LIST, and decodes
// the JSON body into out. It rejects any traversal segment and any KV data path
// before the request is made, so a hostile Vault LIST response cannot redirect
// the adapter to a non-metadata or secret-value path.
func (a *Adapter) doRequest(ctx context.Context, path string, list bool, out any) (bool, error) {
	clean := strings.TrimLeft(path, "/")
	if hasTraversalSegment(clean) {
		return false, errForbiddenPathTraversal
	}
	if isKVDataPath(clean) {
		return false, errForbiddenDataPath
	}
	endpoint := a.address + "/v1/" + clean
	if list {
		endpoint += "?list=true"
	}
	operation := clean
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("vaultapi: build request for %q: %w", clean, wrapTransportError(operation, err))
	}
	req.Header.Set("X-Vault-Token", a.token)
	if a.namespace != "" {
		req.Header.Set("X-Vault-Namespace", a.namespace)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return false, wrapTransportError(operation, err)
	}
	defer func() { _ = resp.Body.Close() }()

	a.lastHTTPStatus = resp.StatusCode
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	case http.StatusForbidden:
		return false, fmt.Errorf("vaultapi: forbidden metadata read (check read-only policy): %w", classifyHTTPStatus(operation, resp))
	default:
		return false, fmt.Errorf("vaultapi: unexpected metadata status: %w", classifyHTTPStatus(operation, resp))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return false, fmt.Errorf("vaultapi: read body for %q: %w", clean, err)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return false, fmt.Errorf("vaultapi: decode %q: %w", clean, err)
		}
	}
	return true, nil
}

// isKVDataPath reports whether a Vault path addresses a KV secret-data
// operation (which must never be read). It scans segments left to right and is
// positional, not a substring match: a "data" segment seen before any
// "metadata" segment is treated as a KV data operation, while a "metadata"
// segment seen first means any later "data" is just a secret legitimately named
// "data" under .../metadata/... and is allowed. (This adapter only ever builds
// .../metadata/... paths for KV, so the guard is a defense-in-depth backstop;
// the broader "data before metadata" rule is intentional so a buggy or hostile
// caller that constructs a bare <mount>/data/... path is still rejected.)
func isKVDataPath(path string) bool {
	for _, segment := range strings.Split(strings.ToLower(path), "/") {
		switch segment {
		case "metadata":
			return false
		case "data":
			return true
		}
	}
	return false
}

// hasTraversalSegment reports whether any path segment is "." or "..", which a
// Vault-returned key name could use to escape the intended subtree. url.PathEscape
// does not encode dots, so this guard is required in addition to per-segment
// escaping.
func hasTraversalSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// listKeys issues a LIST and returns the child keys, or nil if the path is
// absent.
func (a *Adapter) listKeys(ctx context.Context, path string) ([]string, error) {
	var payload struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	ok, err := a.doRequest(ctx, path, true, &payload)
	if err != nil || !ok {
		return nil, err
	}
	return payload.Data.Keys, nil
}

// hashPolicyBody returns a stable hash of a raw policy body so the body itself
// never leaves the adapter.
func hashPolicyBody(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// durationSeconds parses a Vault duration that may be a JSON number (seconds) or
// a Go duration string (for example "3h0m0s"), returning whole seconds.
func durationSeconds(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return int(n)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if d, err := time.ParseDuration(strings.TrimSpace(s)); err == nil {
			return int(d.Seconds())
		}
	}
	return 0
}

// pathEscape escapes a single Vault path segment for use in a request path.
func pathEscape(segment string) string {
	return url.PathEscape(strings.TrimSuffix(segment, "/"))
}

var _ vaultlive.Client = (*Adapter)(nil)
