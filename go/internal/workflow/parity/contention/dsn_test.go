// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"net"
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
		err := loopbackOnly(dsn, false)
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

// TestInTreeDSNMustNameOneServer: the disposable-database helper dials the
// DSN again, so a multi-host DSN could reach a second server the safety
// check never saw. A TLS fallback to the same host is still one server.
func TestInTreeDSNMustNameOneServer(t *testing.T) {
	cases := map[string]bool{
		"postgres://u:p@127.0.0.1:5432/postgres?sslmode=disable":   true,
		"postgres://u:p@127.0.0.1:5432/postgres?sslmode=prefer":    true,
		"postgres://u@127.0.0.1:15432,127.0.0.1:15433/postgres":    false,
		"host=127.0.0.1,127.0.0.1 port=5432,15432 dbname=postgres": false,
	}
	for dsn, ok := range cases {
		err := singleServer(dsn)
		if ok && err != nil {
			t.Fatalf("%s: refused a one-server DSN: %v", dsn, err)
		}
		if !ok && err == nil {
			t.Fatalf("%s: accepted a DSN that names more than one server", dsn)
		}
	}
}

// TestLocalhostMustResolveToLoopback: a host name is resolved, and every
// address must be loopback; a name that does not resolve is refused.
func TestLocalhostMustResolveToLoopback(t *testing.T) {
	if err := loopbackOnly("postgres://u@localhost:5432/x", false); err != nil {
		t.Fatalf("localhost refused: %v", err)
	}
	if err := loopbackOnly("postgres://u@no-such-host.invalid:5432/x", false); err == nil {
		t.Fatalf("an unresolvable host name was accepted")
	}
}

// fakeResolver swaps the package resolver for one test.
func fakeResolver(t *testing.T, answers map[string][]string) {
	t.Helper()
	saved := lookupHost
	lookupHost = func(host string) ([]string, error) {
		if a, ok := answers[host]; ok {
			return a, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	t.Cleanup(func() { lookupHost = saved })
}

// TestResolvedNamesMustBeLoopbackOnly: every resolved address counts; one
// routable address among loopback ones is enough to refuse the name.
func TestResolvedNamesMustBeLoopbackOnly(t *testing.T) {
	fakeResolver(t, map[string][]string{
		"pg-forward": {"127.0.0.1", "::1"},
		"core-db":    {"10.0.0.5"},
		"mixed":      {"127.0.0.1", "10.0.0.5"},
	})
	if err := loopbackOnly("postgres://u@pg-forward:5432/x", false); err != nil {
		t.Fatalf("all-loopback name refused: %v", err)
	}
	for _, host := range []string{"core-db", "mixed"} {
		if err := loopbackOnly("postgres://u@"+host+":5432/x", false); err == nil {
			t.Fatalf("%s resolves to a routable address and was accepted", host)
		}
	}
}

// TestLiveHostsMustBeIPLiterals: the live target accepts loopback IP
// literals only, so no name is resolved between the check and the dial.
func TestLiveHostsMustBeIPLiterals(t *testing.T) {
	fakeResolver(t, map[string][]string{"localhost": {"127.0.0.1"}})
	if err := loopbackOnly("postgres://u@localhost:5432/x", true); err == nil {
		t.Fatalf("live accepted a host name")
	}
	for _, dsn := range []string{"postgres://u@127.0.0.1:5432/x", "postgres://u@[::1]:5432/x"} {
		if err := loopbackOnly(dsn, true); err != nil {
			t.Fatalf("live refused loopback literal %s: %v", dsn, err)
		}
	}
	if err := loopbackOnly("postgres://u@localhost:5432/x", false); err != nil {
		t.Fatalf("local refused localhost: %v", err)
	}
}

// TestEndpointsOverlapChecksEveryHost: the in-tree endpoint is compared with
// every host the satellite DSN can dial, not only its first.
func TestEndpointsOverlapChecksEveryHost(t *testing.T) {
	inTree := "postgres://u@127.0.0.1:32780/postgres?sslmode=disable"
	cases := map[string]bool{
		"postgres://u@127.0.0.1:5000,127.0.0.1:32780/workflow_claims?sslmode=disable": true,
		"postgres://u@127.0.0.1:32780,127.0.0.1:5000/workflow_claims?sslmode=disable": true,
		"postgres://u@127.0.0.1:5000,127.0.0.1:5001/workflow_claims?sslmode=disable":  false,
	}
	for satellite, want := range cases {
		shared, err := endpointsOverlap(inTree, satellite)
		if err != nil {
			t.Fatalf("%s: %v", satellite, err)
		}
		if (shared != "") != want {
			t.Fatalf("%s: shared endpoint %q, want overlap=%v", satellite, shared, want)
		}
	}
}
