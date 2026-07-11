// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
