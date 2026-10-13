// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"errors"
	"regexp"
	"strings"
)

// The closed failure-class set of a network git command (#8091). The first
// seven classes are retryable connectivity failures; the rest are the failures a
// retry can never fix, plus ClassOther for anything the table does not name.
// ClassOther keeps the pre-existing git_sync_failure label so a failure the
// table does not know looks the same as before the retry existed.
const (
	// ClassDeadlineExceeded is a command the sync loop killed at its deadline.
	ClassDeadlineExceeded = "deadline_exceeded"
	// ClassDNSResolve is a name-resolution failure or timeout.
	ClassDNSResolve = "dns_resolve_failed"
	// ClassConnect is a failed or timed-out TCP connect.
	ClassConnect = "connect_failed"
	// ClassConnectionReset is a connection that dropped mid-exchange: reset,
	// receive failure, empty reply, a closed transfer, or a hang-up that comes
	// with a transport error line.
	ClassConnectionReset = "connection_reset"
	// ClassHTTP2Framing is an HTTP/2 framing-layer or stream error.
	ClassHTTP2Framing = "http2_framing"
	// ClassLowSpeedAbort is a transfer git aborted for moving fewer than
	// http.lowSpeedLimit bytes per second for http.lowSpeedTime (curl 28).
	ClassLowSpeedAbort = "low_speed_abort"
	// ClassTLSHandshake is a TLS handshake failure.
	ClassTLSHandshake = "tls_handshake"

	// ClassAuth is an authentication or credential failure (HTTP 401, 403).
	ClassAuth = "auth_failed"
	// ClassHTTPClient is any other HTTP 4xx response.
	ClassHTTPClient = "http_4xx"
	// ClassNotFound is a repository the remote does not have.
	ClassNotFound = "repository_not_found"
	// ClassRateLimited is a rate-limit response (HTTP 429 or a rate-limit text).
	ClassRateLimited = "rate_limited"
	// ClassPackObject is a pack or object error.
	ClassPackObject = "pack_object_error"
	// ClassOther is every failure the table does not name. It is the existing
	// git_sync_failure class, so unknown text keeps its old label.
	ClassOther = "git_sync_failure"
)

// httpClientError matches an HTTP 4xx status git prints through libcurl.
var httpClientError = regexp.MustCompile(`returned error: (4\d\d)`)

type classRule struct {
	class    string
	patterns []string
}

// answerRules are the remote's or the repository's own answers. They run
// first and are never retried: a 403 beside a resolver line, or a corrupt
// object beside a transport line, is the answer, not transport noise. The
// order is the order of precedence.
var answerRules = []classRule{
	{ClassRateLimited, []string{"rate limit", "too many requests"}},
	{ClassNotFound, []string{"repository not found"}},
	{ClassAuth, []string{
		"authentication failed", "could not read username", "permission denied",
		"invalid username or password", "returned error: 401", "returned error: 403",
	}},
	// Integrity errors git reports about objects it already holds or was
	// handed whole. The follow-on lines a cut transfer prints (index-pack
	// failed, bad pack header, ...) are not here: see packFollowOnPatterns.
	{ClassPackObject, []string{"bad object", "missing object", "pack has "}},
}

// transportRules classify a failure of the connection itself, most specific
// first: a TLS failure often prints "Connection reset" too and must stay a TLS
// failure. A transport line wins over git's pack follow-on lines, which are a
// local result of the cut and not an answer the remote gave.
var transportRules = []classRule{
	{ClassDNSResolve, []string{"could not resolve host", "resolving timed out", "could not resolve hostname"}},
	{ClassTLSHandshake, []string{"ssl_connect", "ssl_error_syscall", "tls connect error"}},
	{ClassHTTP2Framing, []string{"error in the http2 framing layer", "http/2 stream"}},
	{ClassLowSpeedAbort, []string{"operation too slow"}},
	{ClassConnect, []string{"failed to connect", "connection timed out", "ssh: connect to host"}},
	{ClassConnectionReset, []string{
		"connection reset", "recv failure", "empty reply from server",
		"transfer closed with", "kex_exchange_identification",
		"connection closed by",
	}},
}

// packFollowOnPatterns are the lines git prints after a pack it could not
// read. With a transport line they are the effect of a cut transfer and the
// transport class stands; alone they are a pack error and are never retried.
var packFollowOnPatterns = []string{
	"index-pack failed", "invalid index-pack output", "unpack-objects failed",
	"bad pack header", "did not receive expected object",
}

// rpcFailedText is git's prefix for a smart-HTTP request that failed in curl;
// it is itself a transport line, whatever curl said after it.
const rpcFailedText = "rpc failed; curl"

// Classify names the failure of one command attempt and reports whether a
// retry can plausibly fix it. stderr is the git error text of that attempt.
// Precedence: a deadline kill is always ClassDeadlineExceeded and retryable;
// then the answers (rate limit, not found, auth, any HTTP 4xx, object
// integrity), never retried; then a transport line, retryable, whatever pack
// lines follow it; then lone pack follow-on lines, never retried. Text the
// table does not name is ClassOther and never retried: an unknown failure is
// not guessed to be transient. git's "remote end hung up unexpectedly" is
// retried only when a transport line comes with it, because the same line also
// ends an auth rejection or a server-side crash.
func Classify(err error, stderr string) (class string, retryable bool) {
	var deadlineErr *Error
	if errors.As(err, &deadlineErr) {
		return ClassDeadlineExceeded, true
	}
	text := strings.ToLower(stderr)
	if match := httpClientError.FindStringSubmatch(text); match != nil {
		switch match[1] {
		case "429":
			return ClassRateLimited, false
		case "401", "403":
			return ClassAuth, false
		}
	}
	for _, rule := range answerRules {
		if containsAny(text, rule.patterns) {
			return rule.class, false
		}
	}
	if httpClientError.MatchString(text) {
		return ClassHTTPClient, false
	}
	for _, rule := range transportRules {
		if containsAny(text, rule.patterns) {
			return rule.class, true
		}
	}
	if strings.Contains(text, rpcFailedText) {
		return ClassConnectionReset, true
	}
	if containsAny(text, packFollowOnPatterns) {
		return ClassPackObject, false
	}
	return ClassOther, false
}

func containsAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

// needsAddressRefresh reports the classes a stale or wrong address can cause,
// the ones that ask the per-host address cache to look the host up again.
func needsAddressRefresh(class string) bool {
	return class == ClassDNSResolve || class == ClassConnect
}
