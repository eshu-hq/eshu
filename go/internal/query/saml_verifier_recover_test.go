// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/crewjam/saml"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func resetSAMLVerifierInstrumentsForTest() {
	samlVerifierInstrumentsOnce = sync.Once{}
	samlVerifierPanics = nil
}

func samlVerifierPanicCount(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != samlVerifierPanicsMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

func panickingParseResponse(*saml.ServiceProvider, *http.Request, []string) (*saml.Assertion, error) {
	var assertion *saml.Assertion
	_ = assertion.Subject // nil dereference, the GO-2026-6909 panic shape
	return nil, nil
}

// TestVerifySAMLResponseRecoversCrewjamPanic proves the verifier turns a panic
// inside crewjam into ErrSAMLVerifierPanic, logs the stack, and counts it.
func TestVerifySAMLResponseRecoversCrewjamPanic(t *testing.T) {
	reader := withPackageMetricReader(t, resetSAMLVerifierInstrumentsForTest)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	now := time.Now().UTC()
	provider, encoded, requestIDHash := samlRecoverFixture(t, now)
	verifier := CrewjamSAMLVerifier{Now: func() time.Time { return now }, parseResponse: panickingParseResponse}

	got, err := verifier.VerifySAMLResponse(context.Background(), provider, encoded, []string{requestIDHash})
	if !errors.Is(err, ErrSAMLVerifierPanic) {
		t.Fatalf("error = %v, want ErrSAMLVerifierPanic", err)
	}
	if got.AssertionID != "" || got.ResponseID != "" || got.Claims.NameID != "" {
		t.Fatalf("assertion = %#v, want zero value after a panic", got)
	}
	logged := logs.String()
	for _, want := range []string{"recovered panic in crewjam/saml", "provider_config_id=provider_a", "nil pointer dereference", "saml_verifier_recover_test.go"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %q:\n%s", want, logged)
		}
	}
	if count := samlVerifierPanicCount(t, reader); count != 1 {
		t.Fatalf("%s = %d, want 1", samlVerifierPanicsMetric, count)
	}
}

// TestSAMLHandlerACSAuditsVerifierPanicAs401 drives the real ACS handler with a
// panicking crewjam call: the browser sees a 401, no session is created, and the
// governance audit records assertion_invalid, the same path every other
// verifier failure takes.
func TestSAMLHandlerACSAuditsVerifierPanicAs401(t *testing.T) {
	withPackageMetricReader(t, resetSAMLVerifierInstrumentsForTest)

	now := time.Now().UTC()
	provider, encoded, _ := samlRecoverFixture(t, now)
	store := &fakeSAMLStore{provider: provider, requestOK: true, resolveOK: true, replayOK: true}
	audit := &fakeGovernanceAuditAppender{}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  CrewjamSAMLVerifier{Now: func() time.Time { return now }, parseResponse: panickingParseResponse},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
		Audit:     audit,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{"RelayState": {"relay-secret"}, "SAMLResponse": {encoded}}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.created) != 0 {
		t.Fatalf("created sessions = %d, want none after a verifier panic", len(store.created))
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != "assertion_invalid" {
		t.Fatalf("audit events = %#v, want one assertion_invalid denial", audit.events)
	}
}
