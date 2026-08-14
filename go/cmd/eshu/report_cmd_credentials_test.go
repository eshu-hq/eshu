// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// credentialSentinel stands in for a password a reporter pasted into a target.
// It is a bare token with no sensitive-looking key in front of it, because that
// is the shape the bundle's redaction cannot see: every rule in
// internal/reportbundle matches on a KEY name, and URL userinfo has none.
const credentialSentinel = "ESHU6059SENTINELc47e09"

// captureEgress is every place `eshu report capture` can put text in front of a
// person: both streams, the error it returns, and the bytes it leaves on disk
// under --out. A redaction assertion has to cover all four — the bundle file is
// the one that gets attached to a public issue, and it is the one a
// stdout-only assertion misses.
type captureEgress struct {
	stdout  string
	stderr  string
	outFile string
	err     error
}

func (e captureEgress) renderings() map[string]string {
	out := map[string]string{"stdout": e.stdout, "stderr": e.stderr, "outfile": e.outFile}
	if e.err != nil {
		out["error"] = e.err.Error()
	}
	return out
}

// runCaptureForEgress runs `report capture` against serverURL with flags set,
// writing the bundle to a temp --out path so the bytes on disk are asserted
// alongside the streams.
func runCaptureForEgress(t *testing.T, serverURL string, flags map[string]string) captureEgress {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "bundle.json")
	cmd := &cobra.Command{}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	mustSetFlag(t, cmd, "service-url", serverURL)
	mustSetFlag(t, cmd, "out", outPath)
	for name, value := range flags {
		mustSetFlag(t, cmd, name, value)
	}
	err := runReportCapture(cmd, nil)
	onDisk, readErr := os.ReadFile(outPath) // #nosec G304 -- test-owned temp path
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read --out bundle: %v", readErr)
	}
	return captureEgress{stdout: out.String(), stderr: errOut.String(), outFile: string(onDisk), err: err}
}

// TestReportCapture_RefusesTargetCarryingURLCredentials is the regression test
// for a credential pasted into --tool or --endpoint as URL userinfo
// (`https://user:PASSWORD@host/...`).
//
// Every redaction rule in internal/reportbundle matches an object KEY name, and
// SplitTargetQuery converts a target's query string back into keys so those
// rules can reach it. Userinfo sits BEFORE the "?", so the split never sees it
// and no key name exists to match: the password landed verbatim in
// query.target of a bundle stamped `"profile": "public"`, `"rules": []`, and
// `"status": "passed"`. It leaked and certified that it had screened.
//
// The username position is covered as well as the password position, because
// the two differ by the character in front of the secret ("//" versus ":") and
// a check anchored on the wrong one passes for the case it was written against.
func TestReportCapture_RefusesTargetCarryingURLCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	defer server.Close()

	tests := []struct {
		name  string
		flags map[string]string
	}{
		{
			// The secret in the password position, preceded by ":".
			name: "tool carries userinfo password",
			flags: map[string]string{
				"endpoint": "/api/v0/services/checkout/story",
				"tool":     "https://svc:" + credentialSentinel + "@mcp.internal:5432/tool",
			},
		},
		{
			// The secret in the username position, preceded by "//".
			name: "tool carries userinfo username",
			flags: map[string]string{
				"endpoint": "/api/v0/services/checkout/story",
				"tool":     "https://" + credentialSentinel + "@mcp.internal:5432/tool",
			},
		},
		{
			name: "endpoint carries userinfo password",
			flags: map[string]string{
				"endpoint": "https://svc:" + credentialSentinel + "@api.internal:8080/api/v0/services/checkout/story",
			},
		},
		{
			// Userinfo plus a query string: the query half already had a rule,
			// the userinfo half must not ride in behind it.
			name: "endpoint carries userinfo alongside a query string",
			flags: map[string]string{
				"endpoint": "https://svc:" + credentialSentinel + "@api.internal:8080/api/v0/x?repo=demo%2Fservice",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			egress := runCaptureForEgress(t, server.URL, tt.flags)
			if egress.err == nil {
				t.Fatalf("runReportCapture() error = nil, want a refusal; stdout = %s", egress.stdout)
			}
			for where, text := range egress.renderings() {
				if strings.Contains(text, credentialSentinel) {
					t.Errorf("credential sentinel reached %s:\n%s", where, text)
				}
			}
			// The refusal still has to tell the reporter which flag to fix.
			if !strings.Contains(egress.err.Error(), "credential") {
				t.Errorf("refusal error = %q, want it to name the problem so the reporter can act on it", egress.err.Error())
			}
		})
	}
}

// TestReportCapture_AcceptsBenignAtSignInPath keeps the refusal above from
// becoming a ban on "@". An "@" inside a path segment is not an authority
// component and carries no credential; net/url is what decides the difference,
// rather than a hand-written character rule of the kind that has already been
// wrong here once.
func TestReportCapture_AcceptsBenignAtSignInPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	defer server.Close()

	egress := runCaptureForEgress(t, server.URL, map[string]string{
		"endpoint": "/api/v0/owners/dev@example.com/services",
	})
	if egress.err != nil {
		t.Fatalf("runReportCapture() error = %v, want nil for a path-segment @", egress.err)
	}
	if gotPath != "/api/v0/owners/dev@example.com/services" {
		t.Errorf("request path = %q, want the endpoint issued unchanged", gotPath)
	}
	if !strings.Contains(egress.outFile, "dev@example.com") {
		t.Errorf("--out bundle lost the benign path segment:\n%s", egress.outFile)
	}
}

// TestReportCapture_RequestFailureDoesNotEchoTargetCredentials covers the same
// secret on the failure rendering. requestErrorWithoutURL replaces the failed
// request's URL with the bare endpoint path, which keeps the query string out
// of the message — and left userinfo in, because the path IS the substitute.
// The guard is repeated inside that function rather than relying on the
// up-front refusal reaching it first: an ordering assumption is what the
// original defect was made of.
func TestReportCapture_RequestFailureDoesNotEchoTargetCredentials(t *testing.T) {
	t.Parallel()

	safe := requestErrorWithoutURL(
		&url.Error{
			Op:  "Get",
			URL: "http://host/x?api_key=" + credentialSentinel,
			Err: errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
		},
		"https://svc:"+credentialSentinel+"@db.internal:5432/api/v0/x",
	)
	if safe == nil {
		t.Fatalf("requestErrorWithoutURL() = nil, want an error")
	}
	if strings.Contains(safe.Error(), credentialSentinel) {
		t.Errorf("request failure error echoed the credential:\n%s", safe.Error())
	}
	if !strings.Contains(safe.Error(), "db.internal") {
		t.Errorf("request failure error = %q, want the host kept so the reporter can act on it", safe.Error())
	}
}
