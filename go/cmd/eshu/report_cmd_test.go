// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/reportbundle"
)

// canaryEnvelopeServer returns a canned query.ResponseEnvelope carrying a
// verbatim truth envelope plus a citation embedding an Excerpt (inline
// content bytes), so the capture command's assertions can prove the truth
// envelope survives byte-for-byte and the excerpt never reaches a
// public-profile bundle.
func canaryEnvelopeServer(t *testing.T, wantPath string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantPath != "" && r.URL.Path != wantPath {
			t.Fatalf("request path = %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{
			"data": {
				"owner": "platform-team",
				"truncated": true,
				"citations": [{"repo_id": "demo/service", "relative_path": "main.go", "excerpt": "func Handler() { return nil }"}]
			},
			"truth": {
				"level": "exact",
				"capability": "trace.service_story",
				"profile": "local_authoritative",
				"basis": "authoritative_graph",
				"backend": "nornicdb",
				"freshness": {"state": "fresh"}
			},
			"error": null
		}`))
	}))
}

// TestReportCapture_AgainstEnvelopeServer proves `eshu report capture` fetches
// the envelope via the API client, stores the query.TruthEnvelope verbatim,
// drops the embedded citation excerpt, and produces a bundle that passes its
// own Validate gate.
func TestReportCapture_AgainstEnvelopeServer(t *testing.T) {
	t.Parallel()

	server := canaryEnvelopeServer(t, "/api/v0/services/checkout/story")
	defer server.Close()

	cmd := &cobra.Command{}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	mustSetFlag(t, cmd, "service-url", server.URL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story")
	mustSetFlag(t, cmd, "params", `{"repo":"demo/service","api_key":"sk-live-should-not-leak"}`)
	mustSetFlag(t, cmd, "note", "expected the owning team, got an empty list")

	if err := runReportCapture(cmd, nil); err != nil {
		t.Fatalf("runReportCapture() error = %v, want nil", err)
	}

	var bundle reportbundle.Bundle
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("decode captured bundle: %v\noutput: %s", err, out.String())
	}

	if bundle.SchemaVersion != reportbundle.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", bundle.SchemaVersion, reportbundle.SchemaVersion)
	}
	if bundle.Response.Truth == nil {
		t.Fatalf("Response.Truth is nil, want the verbatim truth envelope")
	}
	if bundle.Response.Truth.Level != "exact" || bundle.Response.Truth.Backend != "nornicdb" {
		t.Fatalf("Response.Truth = %+v, want verbatim server truth envelope", bundle.Response.Truth)
	}
	if !bundle.Response.Truncated {
		t.Fatalf("Response.Truncated = false, want true (observed from response data)")
	}
	if bundle.Redaction.Profile != reportbundle.ProfilePublic {
		t.Fatalf("Redaction.Profile = %q, want %q", bundle.Redaction.Profile, reportbundle.ProfilePublic)
	}

	raw := out.String()
	if strings.Contains(raw, "sk-live-should-not-leak") {
		t.Fatalf("captured bundle leaks the api_key sentinel value:\n%s", raw)
	}
	if strings.Contains(raw, "\"excerpt\":") {
		t.Fatalf("captured bundle carries a live excerpt key:\n%s", raw)
	}

	if err := reportbundle.Validate(bundle, reportbundle.ValidateOptions{RequirePublic: true}); err != nil {
		t.Fatalf("Validate(bundle, RequirePublic=true) error = %v, want nil", err)
	}
}

// TestReportCapture_GETMergesEndpointQueryWithParams is the CLI-level proof for
// the request URL fetchReportEnvelope builds. The other capture tests all pass
// an --endpoint with no query string, so none of them reaches the merge loop,
// the repeated-value branch, or the collision rule — a regression there would
// build a wrong request and, because Capture splits the same target, record
// parameters that no longer describe it.
//
// It also pins the deliberate asymmetry: the request keeps the credential
// (it is the reporter's own API call, and the answer under investigation is the
// one that credential returns), the bundle does not.
func TestReportCapture_GETMergesEndpointQueryWithParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.URL.Path != "/api/v0/services/checkout/story" {
			t.Errorf("request path = %q, want the bare path with the query string split off", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	mustSetFlag(t, cmd, "service-url", server.URL)
	mustSetFlag(t, cmd, "endpoint",
		"/api/v0/services/checkout/story?repo=demo%2Fservice&tag=alpha&tag=beta&limit=5&api_key=sk-live-should-not-leak")
	mustSetFlag(t, cmd, "params", `{"limit":25}`)

	if err := runReportCapture(cmd, nil); err != nil {
		t.Fatalf("runReportCapture() error = %v, want nil", err)
	}

	// The request the server actually received.
	if got := gotQuery.Get("repo"); got != "demo/service" {
		t.Errorf("request repo = %q, want the endpoint parameter merged in", got)
	}
	if got := gotQuery["tag"]; len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("request tag = %#v, want both repeated endpoint values in order", got)
	}
	if got := gotQuery["limit"]; len(got) != 1 || got[0] != "25" {
		t.Errorf("request limit = %#v, want the explicit --params value to replace the endpoint's, not append to it", got)
	}
	if got := gotQuery.Get("api_key"); got != "sk-live-should-not-leak" {
		t.Errorf("request api_key = %q, want the reporter's own credential still sent with the query under investigation", got)
	}

	// The bundle recorded alongside it.
	var bundle reportbundle.Bundle
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("decode captured bundle: %v\noutput: %s", err, out.String())
	}
	if bundle.Query.Target != "/api/v0/services/checkout/story" {
		t.Errorf("Query.Target = %q, want the bare path", bundle.Query.Target)
	}
	if got := bundle.Query.Params["repo"]; got != "demo/service" {
		t.Errorf("Query.Params[\"repo\"] = %#v, want the endpoint parameter recorded", got)
	}
	tags, ok := bundle.Query.Params["tag"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Errorf("Query.Params[\"tag\"] = %#v, want both repeated values recorded in order", bundle.Query.Params["tag"])
	}
	if got := bundle.Query.Params["limit"]; got != float64(25) {
		t.Errorf("Query.Params[\"limit\"] = %#v, want the explicit --params value, matching what was issued", got)
	}
	if _, present := bundle.Query.Params["api_key"]; present {
		t.Errorf("Query.Params carries api_key; the request may send it, the shared artifact may not")
	}
	if strings.Contains(out.String(), "sk-live-should-not-leak") {
		t.Errorf("captured bundle leaks the endpoint credential:\n%s", out.String())
	}
}

// TestReportCapture_RefusesUnparseableEndpointQueryString proves the CLI fails
// closed on an endpoint whose query string net/url cannot parse, instead of
// issuing a request with the query silently emptied and writing a bundle that
// still carries the raw target.
func TestReportCapture_RefusesUnparseableEndpointQueryString(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server was called; capture must refuse the malformed endpoint before issuing the request")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	mustSetFlag(t, cmd, "service-url", server.URL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story?api_key=sk-live-should-not-leak&bad=%ZZ")

	err := runReportCapture(cmd, nil)
	if err == nil {
		t.Fatalf("runReportCapture() error = nil, want refusal; stdout = %s", out.String())
	}
	if strings.Contains(out.String(), "sk-live-should-not-leak") {
		t.Errorf("refused capture still wrote a bundle carrying the credential:\n%s", out.String())
	}
}

// TestReportCapture_RequestFailureDoesNotEchoEndpointQueryString covers the
// last place a reporter's credential reaches a terminal. The CLI has to put the
// real query string on the wire — the whole point is to reproduce the request
// the reporter actually ran — and net/http embeds the full request URL in the
// error it returns when that request fails. So a mistyped host, an unreachable
// service, or a dropped connection printed
// `Get "http://host/path?api_key=sk-live-...": dial tcp ...` straight to stderr
// and into whatever CI log captured it.
//
// The bundle-side redaction cannot help here: this error is produced before
// Capture ever runs. The command replaces the URL with the bare endpoint path
// instead, which is what a reader needs to fix the problem anyway.
func TestReportCapture_RequestFailureDoesNotEchoEndpointQueryString(t *testing.T) {
	t.Parallel()

	// A server that is closed immediately, so its port is bound to nothing and
	// the request fails at dial time with a *url.Error carrying the full URL.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	closedURL := server.URL
	server.Close()

	const endpointSentinel = "CLI-ENDPOINT-SENTINEL-3f8b21"
	const paramSentinel = "CLI-PARAM-SENTINEL-c47e09"

	cmd := &cobra.Command{}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	mustSetFlag(t, cmd, "service-url", closedURL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story?api_key="+endpointSentinel)
	mustSetFlag(t, cmd, "params", `{"access_token":"`+paramSentinel+`"}`)

	err := runReportCapture(cmd, nil)
	if err == nil {
		t.Fatalf("runReportCapture() error = nil, want a request failure against a closed port")
	}

	egress := err.Error() + "\n" + out.String() + "\n" + errOut.String()
	for _, sentinel := range []string{endpointSentinel, paramSentinel} {
		if strings.Contains(egress, sentinel) {
			t.Errorf("request failure echoed the credential sentinel %q to the user:\n%s", sentinel, egress)
		}
	}
	// The message still has to be actionable: the path a reader needs to fix
	// survives, only its query string is gone.
	if !strings.Contains(err.Error(), "/api/v0/services/checkout/story") {
		t.Errorf("request failure error = %q, want it to name the endpoint path so the reporter can act on it", err.Error())
	}
}

// TestReportCapture_IncludePayloadsWarnsLoudlyAndFailsRequirePublic proves
// --include-payloads flips the bundle profile, prints a loud stderr warning,
// and that the resulting bundle fails a subsequent --require-public check.
func TestReportCapture_IncludePayloadsWarnsLoudlyAndFailsRequirePublic(t *testing.T) {
	t.Parallel()

	server := canaryEnvelopeServer(t, "")
	defer server.Close()

	cmd := &cobra.Command{}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	mustSetFlag(t, cmd, "service-url", server.URL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story")
	mustSetFlag(t, cmd, "include-payloads", "true")

	if err := runReportCapture(cmd, nil); err != nil {
		t.Fatalf("runReportCapture() error = %v, want nil", err)
	}

	if !strings.Contains(strings.ToLower(errOut.String()), "private") {
		t.Fatalf("stderr = %q, want a loud private-triage-only warning", errOut.String())
	}

	var bundle reportbundle.Bundle
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("decode captured bundle: %v", err)
	}
	if bundle.Redaction.Profile != reportbundle.ProfilePrivateTriage {
		t.Fatalf("Redaction.Profile = %q, want %q", bundle.Redaction.Profile, reportbundle.ProfilePrivateTriage)
	}

	if err := reportbundle.Validate(bundle, reportbundle.ValidateOptions{RequirePublic: true}); err == nil {
		t.Fatalf("Validate(bundle, RequirePublic=true) error = nil, want rejection of a private-triage bundle")
	}
}

// TestReportValidate_RequirePublic proves `eshu report validate
// --require-public` passes a public bundle and rejects a private-triage one.
func TestReportValidate_RequirePublic(t *testing.T) {
	t.Parallel()

	publicBundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	privateBundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface:         "api",
		Target:          "/api/v0/services/checkout/story",
		IncludePayloads: true,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}

	tests := []struct {
		name    string
		bundle  reportbundle.Bundle
		wantErr bool
	}{
		{name: "public bundle passes --require-public", bundle: publicBundle, wantErr: false},
		{name: "private-triage bundle fails --require-public", bundle: privateBundle, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(tt.bundle)
			if err != nil {
				t.Fatalf("marshal bundle: %v", err)
			}

			cmd := &cobra.Command{}
			addReportValidateFlags(cmd)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetIn(bytes.NewReader(raw))
			mustSetFlag(t, cmd, "require-public", "true")

			err = runReportValidate(cmd, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("runReportValidate() error = %v, wantErr %v (output: %s)", err, tt.wantErr, out.String())
			}
		})
	}
}

func mustSetFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("Set(%q, %q) error = %v", name, value, err)
	}
}
