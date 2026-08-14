// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

// Synthetic sentinels. Nothing here is a real credential, host, or account --
// each value exists only so a test can prove it did NOT reach an error string.
const (
	clientUserSentinel  = "ESHU-TEST-USER-a1b2c3"
	clientPassSentinel  = "ESHU-TEST-PASS-d4e5f6"
	clientQuerySentinel = "ESHU-TEST-QUERY-9z8y7x"
	clientSafePath      = "/api/v0/services/checkout/story"
)

// credentialBaseURL builds a base URL carrying userinfo credentials against the
// given host:port, mirroring how an operator configures ESHU_SERVICE_URL for a
// service behind basic auth.
func credentialBaseURL(hostPort string) string {
	return "http://" + clientUserSentinel + ":" + clientPassSentinel + "@" + hostPort
}

// pathWithSecretQuery returns a request path whose query string carries a
// secret, which is how the report commands pass reporter-supplied parameters.
func pathWithSecretQuery(suffix string) string {
	return clientSafePath + suffix + "?api_key=" + clientQuerySentinel
}

// assertNoCredentialEgress is the single assertion every wrap site in
// APIClient.do is judged by. Both the leaking and the non-leaking sites call
// it, so no site is graded against a different bar than its neighbours.
func assertNoCredentialEgress(t *testing.T, site string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: error = nil, want a failure to inspect", site)
	}
	got := err.Error()
	for _, secret := range []struct{ label, value string }{
		{"userinfo username", clientUserSentinel},
		{"userinfo password", clientPassSentinel},
		{"query-string secret", clientQuerySentinel},
	} {
		if strings.Contains(got, secret.value) {
			t.Errorf("%s: error text leaked the %s:\n%s", site, secret.label, got)
		}
	}
}

// TestAPIClientDo_NoWrapSiteLeaksCredentials covers every
// fmt.Errorf(...%w...) site in APIClient.do.
//
// Two of the five leaked before this test existed, and they leaked by default
// on an ordinary misconfiguration -- a wrong port, an unreachable service, a
// typo in the URL:
//
//   - "request failed" (HTTPClient.Do). net/http wraps the failure in a
//     *url.Error and runs stripPassword over the URL first, which masks the
//     userinfo password and nothing else. The username and the entire query
//     string survive into the message.
//   - "create request" (http.NewRequest). Worse: it returns url.Parse's
//     *url.Error unchanged, and stripPassword never runs on that path, so an
//     unparseable URL prints the userinfo password in cleartext.
//
// The other three -- marshal, read, decode -- never carry the URL. They are
// asserted here anyway, at the same bar, so that a later edit which starts
// folding the URL into one of those messages fails this test instead of
// shipping a fourth leak.
func TestAPIClientDo_NoWrapSiteLeaksCredentials(t *testing.T) {
	t.Parallel()

	// Port 1 on loopback: privileged, so an unprivileged test run cannot bind
	// it and no sibling test can race in and answer the dial. The connection is
	// refused immediately, which is the *url.Error shape under test.
	const refusedHostPort = "127.0.0.1:1"

	tests := []struct {
		name string
		// wantPrefix pins which wrap site the case actually exercised, so a
		// case that silently starts failing somewhere else is caught rather
		// than passing vacuously on an unrelated error.
		wantPrefix string
		call       func(t *testing.T) error
	}{
		{
			name:       "marshal request",
			wantPrefix: "marshal request:",
			call: func(t *testing.T) error {
				c := &APIClient{BaseURL: credentialBaseURL(refusedHostPort), HTTPClient: &http.Client{}}
				// A channel cannot be marshalled to JSON.
				return c.Post(pathWithSecretQuery(""), make(chan int), nil)
			},
		},
		{
			name:       "create request",
			wantPrefix: "create request:",
			call: func(t *testing.T) error {
				c := &APIClient{BaseURL: credentialBaseURL(refusedHostPort), HTTPClient: &http.Client{}}
				// A malformed percent-escape in the PATH fails url.Parse.
				// (In the query it would not: url.Parse leaves RawQuery
				// unescaped, so the request would reach the transport instead.)
				return c.Get(pathWithSecretQuery("%ZZ"), nil)
			},
		},
		{
			name:       "request failed",
			wantPrefix: "request failed:",
			call: func(t *testing.T) error {
				c := &APIClient{BaseURL: credentialBaseURL(refusedHostPort), HTTPClient: &http.Client{}}
				return c.Get(pathWithSecretQuery(""), nil)
			},
		},
		{
			name:       "read response",
			wantPrefix: "read response:",
			call: func(t *testing.T) error {
				// Content-Length promises far more than the server sends, then
				// it hangs up: io.ReadAll fails with unexpected EOF.
				host := cannedResponseServer(t, "HTTP/1.1 200 OK\r\nContent-Length: 4096\r\n\r\n{\"a\":1}")
				c := &APIClient{BaseURL: credentialBaseURL(host), HTTPClient: &http.Client{}}
				return c.Get(pathWithSecretQuery(""), nil)
			},
		},
		{
			name:       "decode response",
			wantPrefix: "decode response:",
			call: func(t *testing.T) error {
				host := cannedResponseServer(t, "HTTP/1.1 200 OK\r\nContent-Length: 11\r\n\r\nnot-json{{{")
				c := &APIClient{BaseURL: credentialBaseURL(host), HTTPClient: &http.Client{}}
				var out map[string]any
				return c.Get(pathWithSecretQuery(""), &out)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call(t)
			assertNoCredentialEgress(t, tc.name, err)
			if err != nil && !strings.HasPrefix(err.Error(), tc.wantPrefix) {
				t.Fatalf("%s: exercised the wrong wrap site; error = %v, want prefix %q",
					tc.name, err, tc.wantPrefix)
			}
		})
	}
}

// TestAPIClientDo_RedactedErrorStaysActionable is the other half of the
// contract. Redaction that strips everything is easy and useless: an operator
// staring at "request failed" with no endpoint and no cause cannot act. This
// pins what a reader is still owed after the URL is removed -- the endpoint
// path, and the real transport cause both as text and through errors.Is.
func TestAPIClientDo_RedactedErrorStaysActionable(t *testing.T) {
	t.Parallel()

	c := &APIClient{BaseURL: credentialBaseURL("127.0.0.1:1"), HTTPClient: &http.Client{}}
	err := c.Get(pathWithSecretQuery(""), nil)
	if err == nil {
		t.Fatal("Get() error = nil, want a refused connection")
	}

	if !strings.Contains(err.Error(), clientSafePath) {
		t.Errorf("redacted error dropped the endpoint path %q, leaving nothing to act on:\n%v",
			clientSafePath, err)
	}
	// The %w chain has to survive the rewrite, or callers lose the ability to
	// tell a refused connection from a timeout.
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("redaction broke the error chain: errors.Is(err, ECONNREFUSED) = false, err = %v", err)
	}
}

// cannedResponseServer serves one raw HTTP response on loopback and returns its
// host:port. It writes bytes directly rather than using httptest so a malformed
// response -- a lying Content-Length, invalid JSON -- can be produced exactly.
func cannedResponseServer(t *testing.T, response string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil || line == "\r\n" {
				break
			}
		}
		_, _ = fmt.Fprint(conn, response)
	}()

	return listener.Addr().String()
}
