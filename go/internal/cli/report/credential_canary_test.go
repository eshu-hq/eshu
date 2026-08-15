// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package report

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCaptureBundle_CredentialSpellingCanary sweeps one sentinel through every
// spelling a reporter can paste a credential in, varying the character
// immediately in FRONT of it, and asserts the sentinel reaches none of the
// renderings a capture produces.
//
// The character in front is what the last several defects here hid behind. The
// username and password positions differ by exactly that ("//" versus ":"),
// and the scheme-less spelling `svc:PASSWORD@host:5432/tool` differs from the
// refused `https://svc:PASSWORD@host:5432/tool` by nothing else at all — it
// parsed as scheme "svc" with an opaque body and no authority, so net/url
// reported no userinfo and the guard let it through.
//
// The last case is a POSITIVE CONTROL. `/api/v0/owners/NAME@example.com/x` is
// a path segment, not an authority, so it is accepted on purpose and the
// sentinel is expected to show up in the bundle and in the file on disk. It is
// here so the sweep cannot pass by looking at nothing: if captureRenderings
// ever stopped collecting the JSON or the written bytes, every absence
// assertion above would still pass and this one would fail.
func TestCaptureBundle_CredentialSpellingCanary(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	// t.Cleanup, not defer: the subtests below are parallel, so this function
	// returns while they are still paused and a deferred Close would shut the
	// server down before the first request reaches it.
	t.Cleanup(server.Close)

	tests := []struct {
		// name says which character sits immediately before the sentinel.
		name string
		// value is the target the reporter typed, sentinel included.
		value string
		// wantVisible marks the positive control: a value that is accepted,
		// and whose sentinel therefore has to be found in the renderings.
		wantVisible bool
	}{
		{name: "authority start, right after //", value: "https://" + credentialSentinel + "@h.internal/tool"},
		{name: "after a colon, password position", value: "https://svc:" + credentialSentinel + "@h.internal/tool"},
		{name: "after a letter, mid-password", value: "https://svc:pw" + credentialSentinel + "@h.internal/tool"},
		{name: "after a dot", value: "https://svc:pw." + credentialSentinel + "@h.internal/tool"},
		{name: "after a dash", value: "https://svc:pw-" + credentialSentinel + "@h.internal/tool"},
		{name: "after an at sign", value: "https://svc:pw@" + credentialSentinel + "@h.internal/tool"},
		{name: "after a quote, which net/url refuses to parse", value: `https://svc:pw"` + credentialSentinel + "@h.internal/tool"},
		{name: "after a space, which net/url refuses to parse", value: "https://svc:pw " + credentialSentinel + "@h.internal/tool"},
		{name: "after a colon with no scheme at all", value: "svc:" + credentialSentinel + "@h.internal:5432/tool"},
		{name: "after a letter with no scheme at all", value: "svc:pw" + credentialSentinel + "@h.internal/tool"},
		{name: "after a colon in a scheme-relative target", value: "//svc:" + credentialSentinel + "@h.internal/tool"},
		{name: "after a colon under an uppercase scheme", value: "SVC:" + credentialSentinel + "@h.internal/tool"},
		{name: "after a colon with an empty username", value: "https://:" + credentialSentinel + "@h.internal/tool"},
		{
			name:        "after a slash, inside a path segment (positive control)",
			value:       "/api/v0/owners/" + credentialSentinel + "@example.com/services",
			wantVisible: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Both flags, because --endpoint reaches the bundle when --tool
			// is absent and reaches the failure message either way.
			for _, flag := range []string{"--tool", "--endpoint"} {
				flag := flag
				t.Run(flag, func(t *testing.T) {
					t.Parallel()

					opts := CaptureOptions{Endpoint: tt.value}
					if flag == "--tool" {
						opts = CaptureOptions{Endpoint: "/api/v0/services/checkout/story", Tool: tt.value}
					}
					result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, opts)
					renderings := captureRenderings(t, result, err)

					if tt.wantVisible {
						if err != nil {
							t.Fatalf("CaptureBundle() error = %v, want nil for a path-segment @", err)
						}
						for _, where := range []string{"bundle_json", "on_disk"} {
							text, ok := renderings[where]
							if !ok {
								t.Fatalf("captureRenderings() collected no %s, so every absence assertion in this test is blind", where)
							}
							if !strings.Contains(text, credentialSentinel) {
								t.Errorf("positive control: sentinel missing from %s, so the sweep is not reading a real capture:\n%s", where, text)
							}
						}
						return
					}

					if err == nil {
						t.Fatalf("CaptureBundle() error = nil, want a refusal; bundle = %s", result.JSON)
					}
					var credentialErr *TargetCredentialError
					if !errors.As(err, &credentialErr) {
						t.Fatalf("CaptureBundle() error = %v (%T), want *TargetCredentialError so the CLI can map the exit code", err, err)
					}
					if credentialErr.Flag != flag {
						t.Errorf("TargetCredentialError.Flag = %q, want %q", credentialErr.Flag, flag)
					}
					for where, text := range renderings {
						if strings.Contains(text, credentialSentinel) {
							t.Errorf("credential sentinel reached %s:\n%s", where, text)
						}
					}
				})
			}
		})
	}
}
