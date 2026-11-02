// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLoopbackHostsChecksEveryFallback: pgx dials fallback hosts in order,
// so a loopback first host does not make the DSN local.
func TestLoopbackHostsChecksEveryFallback(t *testing.T) {
	cases := map[string]bool{
		"postgres://u:p@127.0.0.1:5432/x":                                   true,
		"postgres://u:p@localhost:5432/x":                                   true,
		"postgres://u@127.0.0.1,core-db.example.internal:5432/x":            false,
		"postgres://u@127.0.0.1:15432,127.0.0.1:15433/x":                    true,
		"host=127.0.0.1,core-db.example.internal port=5432 dbname=x user=u": false,
		"postgres://u@10.0.0.5:5432/x":                                      false,
	}
	for dsn, ok := range cases {
		err := loopbackOnly(dsn)
		if ok && err != nil {
			t.Fatalf("%s: refused a loopback DSN: %v", dsn, err)
		}
		if !ok && err == nil {
			t.Fatalf("%s: accepted a DSN that can reach a non-loopback host", dsn)
		}
	}
}

// TestClaimPlaneHTTPClientRefusesRedirects: a redirect from the forwarded
// claim plane must not carry harness writes to another address.
func TestClaimPlaneHTTPClientRefusesRedirects(t *testing.T) {
	hits := 0
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer elsewhere.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	resp, err := claimPlaneHTTPClient().Post(redirector.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if hits != 0 || resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect followed: hits=%d status=%d", hits, resp.StatusCode)
	}
}
