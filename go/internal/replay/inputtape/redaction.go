// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"net/http"
	"strings"
)

// defaultSecretHeaders is the set of request/response header names whose values
// are redacted before a tape is persisted. Matching is case-insensitive on the
// canonical header name. The set covers the standard credential carriers so a
// recorded tape is credential-free and safe to commit; callers extend it with
// WithRedactedHeaders for provider-specific headers.
var defaultSecretHeaders = []string{
	"Authorization",
	"Proxy-Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Api-Key",
	"X-Auth-Token",
	"X-Amz-Security-Token",
}

// defaultSecretQueryParams is the set of URL query parameter names whose values
// are redacted before a tape is persisted. Matching is case-insensitive.
var defaultSecretQueryParams = []string{
	"token",
	"access_token",
	"api_key",
	"apikey",
	"key",
	"signature",
}

// redactionConfig holds the case-insensitive sets of secret header names, secret
// query parameter names, and volatile query parameter names. The zero value
// redacts nothing; newRedactionConfig seeds the defaults. It is read-only after
// construction so it is safe to share across concurrent RoundTrip calls without
// locking.
//
// Secret and volatile query parameters are both collapsed to a sentinel before
// the request key is computed, but for different reasons and to different
// sentinels: a secret parameter must not be stored (credential safety), while a
// volatile parameter (a per-run timestamp or nonce) must not break key matching
// across runs. Keeping them distinct keeps a recorded tape honest about which
// values were credentials versus which merely varied.
type redactionConfig struct {
	headers  map[string]struct{}
	queries  map[string]struct{}
	volatile map[string]struct{}
}

// newRedactionConfig builds a redaction config seeded with the default secret
// header and query-parameter names, extended with any caller-supplied secret or
// volatile names.
func newRedactionConfig(extraHeaders, extraQueries, volatileQueries []string) redactionConfig {
	cfg := redactionConfig{
		headers:  make(map[string]struct{}),
		queries:  make(map[string]struct{}),
		volatile: make(map[string]struct{}),
	}
	for _, h := range defaultSecretHeaders {
		cfg.headers[strings.ToLower(h)] = struct{}{}
	}
	for _, q := range defaultSecretQueryParams {
		cfg.queries[strings.ToLower(q)] = struct{}{}
	}
	for _, h := range extraHeaders {
		if t := strings.TrimSpace(h); t != "" {
			cfg.headers[strings.ToLower(t)] = struct{}{}
		}
	}
	for _, q := range extraQueries {
		if t := strings.TrimSpace(q); t != "" {
			cfg.queries[strings.ToLower(t)] = struct{}{}
		}
	}
	for _, q := range volatileQueries {
		if t := strings.TrimSpace(q); t != "" {
			cfg.volatile[strings.ToLower(t)] = struct{}{}
		}
	}
	return cfg
}

// isSecretHeader reports whether the named header carries a credential and must
// be redacted. Comparison is case-insensitive on the canonical header name.
func (c redactionConfig) isSecretHeader(name string) bool {
	_, ok := c.headers[strings.ToLower(http.CanonicalHeaderKey(name))]
	return ok
}

// isSecretQueryParam reports whether the named query parameter carries a
// credential and must be redacted. Comparison is case-insensitive.
func (c redactionConfig) isSecretQueryParam(name string) bool {
	_, ok := c.queries[strings.ToLower(name)]
	return ok
}

// isVolatileQueryParam reports whether the named query parameter varies per run
// (a timestamp or nonce) and must be normalized out of the request key so a
// replay request matches the recording despite a different value. Comparison is
// case-insensitive.
func (c redactionConfig) isVolatileQueryParam(name string) bool {
	_, ok := c.volatile[strings.ToLower(name)]
	return ok
}
