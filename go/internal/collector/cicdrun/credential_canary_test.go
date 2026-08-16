// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// credentialSentinel is planted inside credential positions below and must
// never survive into a sanitized URL or a persisted envelope.
const credentialSentinel = "EMBEDDED-CRED-SENTINEL-6119"

// TestSanitizeDeploymentURLDropsOpaqueCredentials sweeps the sentinel
// through the userinfo spellings url.Parse does NOT surface as User: the old
// code set User = nil and re-strung the URL, but an opaque
// `svc:SECRET@host/x` keeps its credential in Opaque, which String()
// round-trips verbatim. environment_url and log_url are set by whatever
// created the deployment, so this shape arrives from outside.
//
// The wantKept row is the positive control: a path-segment "@" is accepted
// on purpose, so the sweep cannot pass by returning "" for everything.
func TestSanitizeDeploymentURLDropsOpaqueCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		wantKept bool
	}{
		{name: "opaque password position", value: "svc:" + credentialSentinel + "@h.internal:5432/env"},
		{name: "opaque mid-password", value: "svc:pw" + credentialSentinel + "@h.internal/env"},
		{name: "opaque under an uppercase scheme", value: "SVC:" + credentialSentinel + "@h.internal/env"},
		{name: "scheme-relative userinfo", value: "//svc:" + credentialSentinel + "@h.internal/env"},
		{name: "unparseable value", value: "https://svc:pw\"" + credentialSentinel + "@h.internal/env"},
		{name: "unparseable only as an authority", value: "svc:pw]" + credentialSentinel + "@h.internal/env"},
		{name: "hierarchical password stays redacted", value: "https://svc:" + credentialSentinel + "@h.internal/env"},
		{name: "path-segment at sign survives (positive control)", value: "https://h.internal/envs/" + credentialSentinel + "@example.com/prod", wantKept: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sanitizeDeploymentURL(tt.value)
			if tt.wantKept {
				if !strings.Contains(got, credentialSentinel) {
					t.Fatalf("sanitizeDeploymentURL(%q) = %q dropped a value that carries no credential; the sweep is over-redacting", tt.value, got)
				}
				return
			}
			if strings.Contains(got, credentialSentinel) {
				t.Errorf("sanitizeDeploymentURL(%q) = %q kept the credential sentinel", tt.value, got)
			}
		})
	}
}

// TestStripSensitiveURLDropsOpaqueCredentials sweeps the sentinel through the
// userinfo spellings url.Parse does NOT surface as User. stripSensitiveURL —
// the sanitizer every envelope's source_ref.source_uri passes through — tested
// parsed.User after a plain parse, and an opaque `svc:SECRET@host/x` parses
// with User == nil and the credential in Opaque, so it re-emitted the value
// verbatim while its sibling sanitizeDeploymentURL was fixed.
//
// The wantKept rows are positive controls: a path-segment "@" and a purl are
// kept on purpose, so the sweep cannot pass by returning "" for everything.
func TestStripSensitiveURLDropsOpaqueCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		wantKept bool
	}{
		{name: "opaque password position", value: "svc:" + credentialSentinel + "@h.internal:5432/run"},
		{name: "opaque mid-password", value: "svc:pw" + credentialSentinel + "@h.internal/run"},
		{name: "opaque under an uppercase scheme", value: "SVC:" + credentialSentinel + "@h.internal/run"},
		{name: "scheme-relative userinfo", value: "//svc:" + credentialSentinel + "@h.internal/run"},
		{name: "unparseable value", value: "https://svc:pw\"" + credentialSentinel + "@h.internal/run"},
		{name: "unparseable only as an authority", value: "svc:pw]" + credentialSentinel + "@h.internal/run"},
		{name: "hierarchical userinfo stays dropped", value: "https://svc:" + credentialSentinel + "@h.internal/run"},
		{name: "path-segment at sign survives (positive control)", value: "https://h.internal/runs/" + credentialSentinel + "@example.com/x", wantKept: true},
		{name: "purl survives (positive control)", value: "pkg:npm/" + credentialSentinel + "@4.17.21", wantKept: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stripSensitiveURL(tt.value)
			if tt.wantKept {
				if !strings.Contains(got, credentialSentinel) {
					t.Fatalf("stripSensitiveURL(%q) = %q dropped a value that carries no credential; the sweep is over-redacting", tt.value, got)
				}
				return
			}
			if strings.Contains(got, credentialSentinel) {
				t.Errorf("stripSensitiveURL(%q) = %q kept the credential sentinel", tt.value, got)
			}
		})
	}
}

// TestNewEnvelopeNeverPersistsOpaqueCredentialSourceURI drives the real
// envelope constructor with a credentialed opaque SourceURI and asserts the
// sentinel is absent from the marshaled envelope — the bytes that persist.
// The second run is the positive control: a path-segment sentinel must reach
// the same bytes, so the absence assertion is reading a real envelope.
func TestNewEnvelopeNeverPersistsOpaqueCredentialSourceURI(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, sourceURI string) string {
		t.Helper()
		envelope := newEnvelope(FixtureContext{
			ScopeID:             "ci-cd:github-actions:example/repo",
			GenerationID:        "generation-1",
			CollectorInstanceID: "fixture-gh",
			FencingToken:        1,
			ObservedAt:          time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			SourceURI:           sourceURI,
		}, "ci.pipeline_run", "run:example", "record-1", map[string]any{"provider": "github_actions"})
		out, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}
		return string(out)
	}

	persisted := build(t, "svc:"+credentialSentinel+"@h.internal:5432/run")
	if strings.Contains(persisted, credentialSentinel) {
		t.Errorf("credential sentinel reached the persisted envelope:\n%s", persisted)
	}

	control := build(t, "https://h.internal/runs/"+credentialSentinel+"@example.com/x")
	if !strings.Contains(control, credentialSentinel) {
		t.Fatalf("positive control: path-segment sentinel missing from the marshaled envelope, so the absence assertion above is blind:\n%s", control)
	}
}

// TestDeploymentEnvelopesNeverPersistOpaqueCredential drives the real
// deployment-envelope builder with a credentialed opaque environment_url and
// log_url and asserts the sentinel is absent from the marshaled envelopes —
// the bytes that persist. The second run is the positive control: a
// path-segment sentinel must reach the same bytes, so the absence assertion
// is reading real envelopes.
func TestDeploymentEnvelopesNeverPersistOpaqueCredential(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, deploymentURL string) string {
		t.Helper()
		encodedURL, err := json.Marshal(deploymentURL)
		if err != nil {
			t.Fatalf("encode deployment URL: %v", err)
		}
		raw := []byte(`{"deployments":[{
			"deployment":{"id":9001,"sha":"0123456789abcdef0123456789abcdef01234567","environment":"production"},
			"statuses":[{"id":8001,"state":"success","environment_url":` + string(encodedURL) + `,"log_url":` + string(encodedURL) + `}]
		}]}`)
		envelopes, err := GitHubActionsDeploymentEnvelopes(raw, FixtureContext{
			ScopeID:             "ci-cd:github-actions:example/repo",
			GenerationID:        "generation-1",
			CollectorInstanceID: "fixture-gh-deployments",
			FencingToken:        1,
			ObservedAt:          time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			SourceURI:           "https://github.com/example/repo",
			Repository:          "example/repo",
		})
		if err != nil {
			t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
		}
		out, err := json.Marshal(envelopes)
		if err != nil {
			t.Fatalf("marshal envelopes: %v", err)
		}
		return string(out)
	}

	persisted := build(t, "svc:"+credentialSentinel+"@h.internal:5432/env")
	if strings.Contains(persisted, credentialSentinel) {
		t.Errorf("credential sentinel reached the persisted envelopes:\n%s", persisted)
	}

	control := build(t, "https://h.internal/envs/"+credentialSentinel+"@example.com/prod")
	if !strings.Contains(control, credentialSentinel) {
		t.Fatalf("positive control: path-segment sentinel missing from the marshaled envelopes, so the absence assertion above is blind:\n%s", control)
	}
}
