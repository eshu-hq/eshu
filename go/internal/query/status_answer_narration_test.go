package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestStatusHandlerAnswerNarrationDefaultStatus(t *testing.T) {
	t.Parallel()

	statusHandler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 14, 6, 30, 0, 0, time.UTC),
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/answer-narration", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != answerNarrationStatusCapability {
		t.Fatalf("truth = %#v, want answer narration status truth", envelope.Truth)
	}
	data := envelopeDataMap(t, envelope)
	if got, want := data["state"], statuspkg.AnswerNarrationUnavailable; got != want {
		t.Fatalf("data[state] = %#v, want %#v", got, want)
	}
	if got, want := data["reason"], statuspkg.AnswerNarrationReasonDisabledByDefault; got != want {
		t.Fatalf("data[reason] = %#v, want %#v", got, want)
	}
	if got, want := data["deterministic_fallback_available"], true; got != want {
		t.Fatalf("data[deterministic_fallback_available] = %#v, want %#v", got, want)
	}
	if got, want := data["canonical_truth_affected"], false; got != want {
		t.Fatalf("data[canonical_truth_affected] = %#v, want %#v", got, want)
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsAnswerNarrationStatusRoute(t *testing.T) {
	t.Parallel()

	const unsafeDetail = "raw prompt provider response https://private.invalid UNSAFE_NARRATION_CREDENTIAL_MARKER"
	statusHandler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 14, 6, 35, 0, 0, time.UTC),
				AnswerNarration: statuspkg.AnswerNarrationStatus{
					State:      statuspkg.AnswerNarrationProviderUnavailable,
					Reason:     statuspkg.AnswerNarrationReasonProviderUnavailable,
					Detail:     unsafeDetail,
					UpdatedAt:  time.Date(2026, 6, 14, 6, 34, 0, 0, time.UTC),
					PolicyHash: "sha256:abcdef1234567890",
				},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/answer-narration", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{
		unsafeDetail,
		"private.invalid",
		"UNSAFE_NARRATION_CREDENTIAL_MARKER",
		"raw prompt",
		"provider response",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("answer narration status leaked %q: %s", forbidden, body)
		}
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != answerNarrationStatusCapability {
		t.Fatalf("truth = %#v, want answer narration status truth", envelope.Truth)
	}
}

// TestStatusHandlerAnswerNarrationUsesInjectedPostureWhenSet verifies that
// when NarrationPosture is set on the StatusHandler the endpoint returns the
// governed posture rather than the DB-derived default.
func TestStatusHandlerAnswerNarrationUsesInjectedPostureWhenSet(t *testing.T) {
	t.Parallel()

	injected := statuspkg.AnswerNarrationStatus{
		State:                          statuspkg.AnswerNarrationAvailable,
		Reason:                         statuspkg.AnswerNarrationReasonAvailable,
		Detail:                         "all gates open",
		ProviderConfigured:             true,
		ProviderTrafficEnabled:         true,
		PolicyAllowed:                  true,
		BudgetAvailable:                true,
		PublishSafetyEnabled:           true,
		DeterministicFallbackAvailable: true,
		RetentionPosture:               statuspkg.AnswerNarrationRetentionMetadataOnly,
		UpdatedAt:                      time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
	}
	statusHandler := &StatusHandler{
		Profile:          ProfileProduction,
		NarrationPosture: func() statuspkg.AnswerNarrationStatus { return injected },
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/answer-narration", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelopeDataMap(t, envelope)
	if got, want := data["state"], statuspkg.AnswerNarrationAvailable; got != want {
		t.Fatalf("data[state] = %#v, want %#v (injected posture not used)", got, want)
	}
	if got, want := data["reason"], statuspkg.AnswerNarrationReasonAvailable; got != want {
		t.Fatalf("data[reason] = %#v, want %#v", got, want)
	}
	if got, want := data["provider_configured"], true; got != want {
		t.Fatalf("data[provider_configured] = %#v, want true", got)
	}
}

// TestStatusHandlerAnswerNarrationDefaultClosedWhenNilPosture verifies that
// when NarrationPosture is nil (default) the endpoint returns Unavailable.
// This is the default-closed invariant: nothing must be Available unless all
// gates are explicitly opened.
func TestStatusHandlerAnswerNarrationDefaultClosedWhenNilPosture(t *testing.T) {
	t.Parallel()

	// No NarrationPosture set, no StatusReader — bare handler.
	statusHandler := &StatusHandler{Profile: ProfileProduction}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/answer-narration", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelopeDataMap(t, envelope)
	if got := data["state"]; got == statuspkg.AnswerNarrationAvailable {
		t.Fatalf("data[state] = %#v, must NOT be Available when NarrationPosture is nil (default-closed violated)", got)
	}
}

func envelopeDataMap(t *testing.T, envelope ResponseEnvelope) map[string]any {
	t.Helper()

	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data type = %T, want map[string]any", envelope.Data)
	}
	return data
}
