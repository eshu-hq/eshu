// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// parityClaimItem builds the workflow claim for a parity scope.
func parityClaimItem(ps parityScope) workflow.WorkItem {
	return workflow.WorkItem{
		ScopeID:             ps.scopeID,
		GenerationID:        ps.generationID,
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-prometheus-parity",
		CurrentFencingToken: ps.fencingToken,
	}
}

// parityClaimSource wires one httptest server into a claim-driven source.
func parityClaimSource(t *testing.T, server *httptest.Server, ps parityScope) *ClaimedSource {
	t.Helper()
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-prometheus-parity",
		Targets: []TargetConfig{{
			Provider:         ps.provider,
			ScopeID:          ps.scopeID,
			InstanceID:       ps.instanceID,
			BaseURL:          server.URL,
			ResourceLimit:    ps.resourceLimit,
			StaleAfter:       ps.staleAfter,
			Enabled:          true,
			DeclaredIDs:      ps.declaredIDs,
			ObservedOnlyHint: ps.observedOnly,
			TenantID:         ps.tenantID,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return source
}

func parityWarningReasons(envelopes []facts.Envelope) map[string]int {
	reasons := map[string]int{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.ObservabilityCoverageWarningFactKind {
			continue
		}
		if reason, ok := envelope.Payload["warning_kind"].(string); ok {
			reasons[reason]++
		}
	}
	return reasons
}

// TestPrometheusMimir8864ParityTruncationIsInBand pins the resource_limit
// cut as an in-band signal: the truncated generation still commits with a
// coverage_warning, so the cutover gate can tell a capped provider read
// from a complete one.
func TestPrometheusMimir8864ParityTruncationIsInBand(t *testing.T) {
	t.Parallel()

	scopes := parityScopes()
	var truncated parityScope
	for _, ps := range scopes {
		if ps.name == "prometheus truncated" {
			truncated = ps
		}
	}
	collected, _ := parityLiveClaim(t, truncated)
	envelopes := paritySnapshot(collected)
	reasons := parityWarningReasons(envelopes)
	if reasons[WarningTruncated] == 0 {
		t.Fatalf("warning reasons = %v, want a truncated coverage_warning", reasons)
	}
	counts := parityKindMultiset(envelopes)
	if counts[facts.ObservabilityObservedTargetFactKind] != 1 {
		t.Fatalf("observed_target facts = %d, want 1 capped target", counts[facts.ObservabilityObservedTargetFactKind])
	}
}

// TestPrometheusMimir8864ParityPartialTargetsStayComplete pins the
// permission-hidden half of partial reads: a 403 on targets stays in-band
// as a coverage_warning while rules still commit, so the generation is
// partial but not lost.
func TestPrometheusMimir8864ParityPartialTargetsStayComplete(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/targets":
			w.WriteHeader(http.StatusForbidden)
		case "/api/v1/rules":
			writeJSON(t, w, parityPrometheusRules())
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	ps := parityScopes()[0]
	source := parityClaimSource(t, server, ps)
	collected, ok, err := source.NextClaimed(context.Background(), parityClaimItem(ps))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want partial-but-complete generation")
	}
	envelopes := paritySnapshot(collected)
	reasons := parityWarningReasons(envelopes)
	if reasons[WarningPermissionHidden] == 0 {
		t.Fatalf("warning reasons = %v, want a permission_hidden coverage_warning", reasons)
	}
	if counts := parityKindMultiset(envelopes); counts[facts.ObservabilityObservedRuleFactKind] != 2 {
		t.Fatalf("observed_rule facts = %d, want 2 committed rules", counts[facts.ObservabilityObservedRuleFactKind])
	}
}

// TestPrometheusMimir8864ParityRetryableTransportSkipsClaim pins the
// retryable half of failure classification: a 500 exhausts bounded retries
// and fails the claim retryable with no facts, so the dispatcher (not the
// producer) owns the retry.
func TestPrometheusMimir8864ParityRetryableTransportSkipsClaim(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ps := parityScopes()[0]
	source := parityClaimSource(t, server, ps)
	_, ok, err := source.NextClaimed(context.Background(), parityClaimItem(ps))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want retryable provider failure")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want no generation on transport failure")
	}
	var failure ProviderFailure
	if !errors.As(err, &failure) {
		t.Fatalf("NextClaimed() error = %T, want ProviderFailure", err)
	}
	if got := failure.FailureClass(); got != FailureRetryable {
		t.Fatalf("FailureClass() = %q, want %q", got, FailureRetryable)
	}
	if got, want := attempts, maxHTTPRetries+1; got != want {
		t.Fatalf("attempts = %d, want %d bounded retries", got, want)
	}
}

// TestPrometheusMimir8864ParityStaleClaimRejected pins claim scoping: a
// work item for an unconfigured scope fails retryable with no facts, so a
// revoked or reassigned scope never writes under a stale claim.
func TestPrometheusMimir8864ParityStaleClaimRejected(t *testing.T) {
	t.Parallel()

	ps := parityScopes()[0]
	mux := http.NewServeMux()
	ps.serve(t, mux, &paritySeen{})
	server := httptest.NewServer(mux)
	defer server.Close()

	source := parityClaimSource(t, server, ps)
	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID:             "prometheus:parity:revoked",
		GenerationID:        ps.generationID,
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-prometheus-parity",
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want stale-claim rejection")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want no generation for unconfigured scope")
	}
	var failure ProviderFailure
	if !errors.As(err, &failure) {
		t.Fatalf("NextClaimed() error = %T, want ProviderFailure", err)
	}
	if got := failure.FailureClass(); got != FailureRetryable {
		t.Fatalf("FailureClass() = %q, want %q", got, FailureRetryable)
	}
}

// TestPrometheusMimir8864ParityRedactionHolds pins the metadata-only
// contract at the envelope layer: no rendered fact may carry raw scrape
// URLs, label values, PromQL, annotations, or tenant values, even though
// the provider served all of them.
func TestPrometheusMimir8864ParityRedactionHolds(t *testing.T) {
	t.Parallel()

	var rendered []string
	for _, ps := range parityScopes() {
		collected, _ := parityLiveClaim(t, ps)
		for _, envelope := range paritySnapshot(collected) {
			rendered = append(rendered, strings.ToLower(anyJSON(t, envelope.Payload)))
		}
	}
	forbidden := []string{
		"10.0.0.1", "10.0.0.2", "node-1", "node-2",
		"sum(rate(secret_total", "secret_bytes", "up == 0",
		"parity alert", "parity record", "mimir alert",
		"tenant-acme", "connection refused",
	}
	for _, payload := range rendered {
		for _, value := range forbidden {
			if strings.Contains(payload, strings.ToLower(value)) {
				t.Fatalf("envelope payload leaked forbidden value %q", value)
			}
		}
	}
}

// TestPrometheusMimir8864ParityCollectorKindRejected pins kind scoping: a
// claim naming another collector kind never collects, so one producer
// cannot emit under a sibling's authority.
func TestPrometheusMimir8864ParityCollectorKindRejected(t *testing.T) {
	t.Parallel()

	ps := parityScopes()[0]
	mux := http.NewServeMux()
	ps.serve(t, mux, &paritySeen{})
	server := httptest.NewServer(mux)
	defer server.Close()

	source := parityClaimSource(t, server, ps)
	item := parityClaimItem(ps)
	item.CollectorKind = scope.CollectorKind("loki")
	_, _, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want collector-kind rejection")
	}
}
