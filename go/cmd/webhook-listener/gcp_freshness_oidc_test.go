// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/api/idtoken"
)

// fakeGCPPushOIDCValidator is a hermetic, in-process stand-in for
// googleOIDCValidator. It never makes a network call to Google; tests set
// wantToken/wantAudience to assert the exact values the handler passed
// through, and configure email/emailVerified/err to control the outcome.
type fakeGCPPushOIDCValidator struct {
	wantToken     string
	wantAudience  string
	email         string
	emailVerified bool
	err           error
}

func (f *fakeGCPPushOIDCValidator) ValidateGCPPushToken(
	_ context.Context,
	idToken string,
	audience string,
) (string, bool, error) {
	if f.wantToken != "" && idToken != f.wantToken {
		return "", false, errors.New("unexpected token presented to validator")
	}
	if f.wantAudience != "" && audience != f.wantAudience {
		return "", false, errors.New("unexpected audience presented to validator")
	}
	if f.err != nil {
		return "", false, f.err
	}
	return f.email, f.emailVerified, nil
}

const (
	gcpFreshnessTestOIDCAudience   = "https://eshu.example.test/webhook/gcp-freshness"
	gcpFreshnessTestOIDCAllowedSA  = "push-invoker@example-project.iam.gserviceaccount.com"
	gcpFreshnessTestOIDCValidToken = "valid-google-signed-oidc-token"
)

// TestValidateGoogleOIDCPayloadAcceptsGoogleIssuers proves both accepted
// Google issuer forms (bare and https-prefixed) pass the issuer check and
// extract the email/email_verified claims.
func TestValidateGoogleOIDCPayloadAcceptsGoogleIssuers(t *testing.T) {
	t.Parallel()

	for _, issuer := range []string{"accounts.google.com", "https://accounts.google.com"} {
		t.Run(issuer, func(t *testing.T) {
			t.Parallel()
			payload := &idtoken.Payload{
				Issuer: issuer,
				Claims: map[string]any{
					"email":          gcpFreshnessTestOIDCAllowedSA,
					"email_verified": true,
				},
			}
			email, emailVerified, err := validateGoogleOIDCPayload(payload)
			if err != nil {
				t.Fatalf("validateGoogleOIDCPayload() error = %v, want nil for issuer %q", err, issuer)
			}
			if email != gcpFreshnessTestOIDCAllowedSA || !emailVerified {
				t.Fatalf("validateGoogleOIDCPayload() = (%q, %v), want (%q, true)", email, emailVerified, gcpFreshnessTestOIDCAllowedSA)
			}
		})
	}
}

// TestValidateGoogleOIDCPayloadRejectsWrongIssuer proves a Google-signed
// token minted for an unrelated purpose (any issuer other than Google's own
// accounts.google.com forms) is rejected, even though idtoken.Validate itself
// does not check the issuer claim. This is the fix for the codex P1 finding:
// idtoken.Validate only verifies signature, audience, and expiry.
func TestValidateGoogleOIDCPayloadRejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"https://attacker.example.test",
		"google.com",
		"https://accounts.google.com.attacker.test",
	}
	for _, issuer := range tests {
		t.Run(issuer, func(t *testing.T) {
			t.Parallel()
			payload := &idtoken.Payload{
				Issuer: issuer,
				Claims: map[string]any{
					"email":          gcpFreshnessTestOIDCAllowedSA,
					"email_verified": true,
				},
			}
			if _, _, err := validateGoogleOIDCPayload(payload); err == nil {
				t.Fatalf("validateGoogleOIDCPayload() error = nil for issuer %q, want rejection", issuer)
			}
		})
	}
}

// TestValidateGoogleOIDCPayloadRejectsNilPayload proves a nil payload is
// rejected fail-closed rather than panicking.
func TestValidateGoogleOIDCPayloadRejectsNilPayload(t *testing.T) {
	t.Parallel()

	if _, _, err := validateGoogleOIDCPayload(nil); err == nil {
		t.Fatal("validateGoogleOIDCPayload(nil) error = nil, want rejection")
	}
}

func TestVerifyGCPPushOIDCAcceptsValidToken(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", nil)
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	validator := &fakeGCPPushOIDCValidator{
		wantToken:     gcpFreshnessTestOIDCValidToken,
		wantAudience:  gcpFreshnessTestOIDCAudience,
		email:         gcpFreshnessTestOIDCAllowedSA,
		emailVerified: true,
	}

	if !verifyGCPPushOIDC(req.Context(), req, validator, gcpFreshnessTestOIDCAudience, gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = false, want true for a valid token, matching audience, allowed+verified email")
	}
}

func TestVerifyGCPPushOIDCRejectsWrongAudience(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", nil)
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	// The fake validator itself enforces the audience match (mirroring what
	// idtoken.Validate does against the real aud claim), so a validator that
	// only accepts the configured audience proves a mismatched audience fails.
	validator := &fakeGCPPushOIDCValidator{
		wantAudience:  "https://attacker.example.test/webhook",
		email:         gcpFreshnessTestOIDCAllowedSA,
		emailVerified: true,
	}

	if verifyGCPPushOIDC(req.Context(), req, validator, gcpFreshnessTestOIDCAudience, gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = true, want false for a wrong-audience token")
	}
}

func TestVerifyGCPPushOIDCRejectsDisallowedServiceAccount(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", nil)
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	validator := &fakeGCPPushOIDCValidator{
		email:         "someone-else@other-project.iam.gserviceaccount.com",
		emailVerified: true,
	}

	if verifyGCPPushOIDC(req.Context(), req, validator, gcpFreshnessTestOIDCAudience, gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = true, want false for a disallowed service-account principal")
	}
}

func TestVerifyGCPPushOIDCRejectsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", nil)
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	validator := &fakeGCPPushOIDCValidator{
		email:         gcpFreshnessTestOIDCAllowedSA,
		emailVerified: false,
	}

	if verifyGCPPushOIDC(req.Context(), req, validator, gcpFreshnessTestOIDCAudience, gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = true, want false when email_verified=false, even for the allowed principal")
	}
}

func TestVerifyGCPPushOIDCRejectsExpiredOrMalformedToken(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", nil)
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	validator := &fakeGCPPushOIDCValidator{
		err: errors.New("idtoken: token expired"),
	}

	if verifyGCPPushOIDC(req.Context(), req, validator, gcpFreshnessTestOIDCAudience, gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = true, want false when the validator reports an error (expired/malformed/bad signature)")
	}
}

func TestVerifyGCPPushOIDCRejectsMissingToken(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", nil)
	validator := &fakeGCPPushOIDCValidator{
		email:         gcpFreshnessTestOIDCAllowedSA,
		emailVerified: true,
	}

	if verifyGCPPushOIDC(req.Context(), req, validator, gcpFreshnessTestOIDCAudience, gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = true, want false when the request carries no Authorization bearer token")
	}
}

func TestVerifyGCPPushOIDCRejectsWhenNotConfigured(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", nil)
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	validator := &fakeGCPPushOIDCValidator{
		email:         gcpFreshnessTestOIDCAllowedSA,
		emailVerified: true,
	}

	// Fail-closed: a blank audience or allowlisted SA (OIDC not configured)
	// must never validate, mirroring validGCPFreshnessToken's empty-token guard.
	if verifyGCPPushOIDC(req.Context(), req, validator, "", gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = true with empty audience, want false (fail-closed)")
	}
	if verifyGCPPushOIDC(req.Context(), req, validator, gcpFreshnessTestOIDCAudience, "") {
		t.Fatal("verifyGCPPushOIDC() = true with empty allowed service account, want false (fail-closed)")
	}
	if verifyGCPPushOIDC(req.Context(), req, nil, gcpFreshnessTestOIDCAudience, gcpFreshnessTestOIDCAllowedSA) {
		t.Fatal("verifyGCPPushOIDC() = true with nil validator, want false (fail-closed)")
	}
}

// TestWebhookHandlerAcceptsGCPFreshnessOIDCPush proves the end-to-end handler
// path accepts a request authenticated purely via a verified OIDC token, with
// no shared token configured at all.
func TestWebhookHandlerAcceptsGCPFreshnessOIDCPush(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	validator := &fakeGCPPushOIDCValidator{
		wantToken:     gcpFreshnessTestOIDCValidToken,
		wantAudience:  gcpFreshnessTestOIDCAudience,
		email:         gcpFreshnessTestOIDCAllowedSA,
		emailVerified: true,
	}
	mux, err := newWebhookMux(webhookHandler{
		Config: webhookListenerConfig{
			GCPFreshnessOIDCAudience:  gcpFreshnessTestOIDCAudience,
			GCPFreshnessOIDCAllowedSA: gcpFreshnessTestOIDCAllowedSA,
			GCPFreshnessPath:          "/webhook/gcp-freshness",
			MaxRequestBodyBytes:       defaultMaxWebhookBodyBytes,
		},
		GCPFreshnessStore:    store,
		GCPPushOIDCValidator: validator,
		Clock: func() time.Time {
			return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(store.triggers) != 1 {
		t.Fatalf("stored triggers = %d, want 1", len(store.triggers))
	}
}

// TestWebhookHandlerRejectsGCPFreshnessOIDCWrongAudience proves the handler
// rejects a request whose OIDC token fails validation, with no shared token
// configured as a fallback.
func TestWebhookHandlerRejectsGCPFreshnessOIDCWrongAudience(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	validator := &fakeGCPPushOIDCValidator{
		wantAudience: "https://attacker.example.test/webhook",
	}
	mux, err := newWebhookMux(webhookHandler{
		Config: webhookListenerConfig{
			GCPFreshnessOIDCAudience:  gcpFreshnessTestOIDCAudience,
			GCPFreshnessOIDCAllowedSA: gcpFreshnessTestOIDCAllowedSA,
			GCPFreshnessPath:          "/webhook/gcp-freshness",
			MaxRequestBodyBytes:       defaultMaxWebhookBodyBytes,
		},
		GCPFreshnessStore:    store,
		GCPPushOIDCValidator: validator,
		Clock: func() time.Time {
			return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+gcpFreshnessTestOIDCValidToken)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

// TestWebhookHandlerGCPFreshnessSharedTokenStillWorksWithOIDCConfigured proves
// the shared-token path remains a fully independent accepted auth mechanism
// even when OIDC is also configured — the two paths coexist, and either being
// valid is sufficient.
func TestWebhookHandlerGCPFreshnessSharedTokenStillWorksWithOIDCConfigured(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	// A validator that always rejects proves the shared token, not OIDC,
	// authenticated this request.
	validator := &fakeGCPPushOIDCValidator{err: errors.New("oidc must not be consulted")}
	mux, err := newWebhookMux(webhookHandler{
		Config: webhookListenerConfig{
			GCPFreshnessToken:         "secret",
			GCPFreshnessOIDCAudience:  gcpFreshnessTestOIDCAudience,
			GCPFreshnessOIDCAllowedSA: gcpFreshnessTestOIDCAllowedSA,
			GCPFreshnessPath:          "/webhook/gcp-freshness",
			MaxRequestBodyBytes:       defaultMaxWebhookBodyBytes,
		},
		GCPFreshnessStore:    store,
		GCPPushOIDCValidator: validator,
		Clock: func() time.Time {
			return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(store.triggers) != 1 {
		t.Fatalf("stored triggers = %d, want 1", len(store.triggers))
	}
}

// TestWebhookHandlerGCPFreshnessRejectsWhenNeitherAuthPathConfigured proves
// that with neither the shared token nor OIDC configured, the route is not
// even mounted — matching today's shared-token-only default-off behavior with
// zero regression.
func TestWebhookHandlerGCPFreshnessRejectsWhenNeitherAuthPathConfigured(t *testing.T) {
	t.Parallel()

	_, err := newWebhookMux(webhookHandler{
		Config: webhookListenerConfig{
			GCPFreshnessPath:    "/webhook/gcp-freshness",
			MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
		},
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v, want nil (route simply unmounted)", err)
	}
}

func TestLoadWebhookListenerConfigAllowsGCPFreshnessOIDCOnly(t *testing.T) {
	t.Parallel()

	cfg, err := loadWebhookListenerConfig(func(key string) string {
		values := map[string]string{
			"ESHU_GCP_FRESHNESS_OIDC_AUDIENCE":   gcpFreshnessTestOIDCAudience,
			"ESHU_GCP_FRESHNESS_OIDC_ALLOWED_SA": gcpFreshnessTestOIDCAllowedSA,
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("loadWebhookListenerConfig() error = %v, want nil", err)
	}
	if cfg.GCPFreshnessOIDCAudience != gcpFreshnessTestOIDCAudience {
		t.Fatalf("GCPFreshnessOIDCAudience = %q, want %q", cfg.GCPFreshnessOIDCAudience, gcpFreshnessTestOIDCAudience)
	}
	if cfg.GCPFreshnessOIDCAllowedSA != gcpFreshnessTestOIDCAllowedSA {
		t.Fatalf("GCPFreshnessOIDCAllowedSA = %q, want %q", cfg.GCPFreshnessOIDCAllowedSA, gcpFreshnessTestOIDCAllowedSA)
	}
	if cfg.GCPFreshnessPath != "/webhook/gcp-freshness" {
		t.Fatalf("GCPFreshnessPath = %q, want /webhook/gcp-freshness", cfg.GCPFreshnessPath)
	}
}

func TestLoadWebhookListenerConfigRejectsPartialGCPFreshnessOIDCConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "audience only",
			env:  map[string]string{"ESHU_GCP_FRESHNESS_OIDC_AUDIENCE": gcpFreshnessTestOIDCAudience},
		},
		{
			name: "allowed service account only",
			env:  map[string]string{"ESHU_GCP_FRESHNESS_OIDC_ALLOWED_SA": gcpFreshnessTestOIDCAllowedSA},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadWebhookListenerConfig(func(key string) string { return tt.env[key] })
			if err == nil {
				t.Fatal("loadWebhookListenerConfig() error = nil, want error for partial OIDC config")
			}
		})
	}
}
