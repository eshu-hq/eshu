// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package prometheus parity pin for #8864 (AC3, in-tree side).
//
// These tests prove the CURRENT in-tree source behavior that the #4047
// cutover gate must reproduce exactly: the same scopes, kinds, stable keys,
// fencing tokens, and in-band failure signals, whether the facts come from a
// live claim or from the checked-in cassette
// (testdata/8864-parity-prometheus-mimir.json, cut from a live HTTP run
// with the same fixed generations). The #6710 compatibility harness reuses
// this pack: the live side is the oracle, the cassette is the recorded
// expectation.
//
// The live side drives the real HTTPClient against httptest servers, not a
// stub EvidenceClient, so provider HTTP semantics (Mimir skipping targets,
// truncation caps, stale classification) stay under test.
//
// Out of scope here (core-owned, recorded for the external producer):
// grant revocation enforcement (see PRODUCER_GRANT_DRAFT.md) and durable
// commit/lease semantics owned by the ingestion store and workflow control
// store. The in-tree path pins the emission half; the external path must
// additionally fail closed on missing, expired, or revoked grants.

package prometheus

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// parityObservedAt is the fixed generation timestamp recorded in the
// cassette scopes. Live runs use the real clock for observed_at, which the
// comparison excludes; everything identity-bearing is deterministic.
var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// parityScope binds one live claim to its recorded cassette scope.
type parityScope struct {
	name           string
	provider       string
	scopeID        string
	instanceID     string
	generationID   string
	fencingToken   int64
	resourceLimit  int
	staleAfter     time.Duration
	declaredIDs    map[string]struct{}
	observedOnly   bool
	tenantID       string
	serve          func(t *testing.T, mux *http.ServeMux, seen *paritySeen)
	expectNoTarget bool
}

// paritySeen records cross-cutting server observations.
type paritySeen struct {
	targetPaths []string
	tenant      []string
}

func parityPrometheusTargets() map[string]any {
	return map[string]any{
		"status": "success",
		"data": map[string]any{
			"activeTargets": []map[string]any{
				{
					"discoveredLabels": map[string]any{
						"__address__":      "10.0.0.1:9100",
						"__metrics_path__": "/metrics",
					},
					"labels": map[string]any{
						"instance": "node-1",
						"job":      "node",
					},
					"scrapePool": "nodes",
					"scrapeUrl":  "http://10.0.0.1:9100/metrics",
					"health":     "up",
					"lastScrape": "2026-09-30T12:00:00Z",
				},
				{
					"discoveredLabels": map[string]any{
						"__address__": "10.0.0.2:9100",
					},
					"labels": map[string]any{
						"instance": "node-2",
						"job":      "node",
					},
					"scrapePool": "nodes",
					"scrapeUrl":  "http://10.0.0.2:9100/metrics",
					"health":     "down",
					"lastScrape": "2026-01-01T00:00:00Z",
					"lastError":  "connection refused",
				},
			},
		},
	}
}

func parityPrometheusRules() map[string]any {
	return map[string]any{
		"status": "success",
		"data": map[string]any{
			"groups": []map[string]any{
				{
					"name": "parity.rules",
					"file": "rules/parity.yaml",
					"rules": []map[string]any{
						{
							"name":           "ParityBurn",
							"type":           "alerting",
							"health":         "ok",
							"query":          "sum(rate(secret_total[5m])) > 7",
							"lastEvaluation": "2026-09-30T12:00:00Z",
							"labels":         map[string]any{"service": "parity"},
							"annotations":    map[string]any{"summary": "parity alert"},
						},
						{
							"name":           "ParityRecord",
							"type":           "recording",
							"health":         "ok",
							"query":          "sum(secret_bytes)",
							"lastEvaluation": "2026-01-01T00:00:00Z",
							"labels":         map[string]any{"service": "parity"},
							"annotations":    map[string]any{"summary": "parity record"},
						},
					},
				},
			},
		},
	}
}

// parityScopes mirrors the three recorded cassette scopes: a full
// Prometheus scope (targets, rules, stale and manual signals), a Mimir
// tenant scope (rules only, tenant redacted), and a truncated Prometheus
// scope (resource_limit cut with an in-band warning).
func parityScopes() []parityScope {
	return []parityScope{
		{
			name:          "prometheus full",
			provider:      ProviderPrometheus,
			scopeID:       "prometheus:parity:prod",
			instanceID:    "prometheus-parity",
			generationID:  "prometheus-mimir:8864:prod:1",
			fencingToken:  7,
			resourceLimit: 50,
			staleAfter:    time.Hour,
			declaredIDs:   map[string]struct{}{"parity.rules:ParityBurn": {}},
			observedOnly:  true,
			serve: func(t *testing.T, mux *http.ServeMux, seen *paritySeen) {
				t.Helper()
				mux.HandleFunc("/api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
					seen.targetPaths = append(seen.targetPaths, r.URL.String())
					writeJSON(t, w, parityPrometheusTargets())
				})
				mux.HandleFunc("/api/v1/rules", func(w http.ResponseWriter, r *http.Request) {
					writeJSON(t, w, parityPrometheusRules())
				})
			},
		},
		{
			name:          "mimir tenant",
			provider:      ProviderMimir,
			scopeID:       "mimir:parity:tenant",
			instanceID:    "mimir-parity",
			generationID:  "prometheus-mimir:8864:mimir:1",
			fencingToken:  9,
			resourceLimit: 50,
			tenantID:      "tenant-acme",
			serve: func(t *testing.T, mux *http.ServeMux, seen *paritySeen) {
				t.Helper()
				mux.HandleFunc("/api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
					seen.targetPaths = append(seen.targetPaths, r.URL.String())
					t.Error("mimir must not request targets")
				})
				mux.HandleFunc("/api/v1/rules", func(w http.ResponseWriter, r *http.Request) {
					seen.tenant = append(seen.tenant, r.Header.Get("X-Scope-OrgID"))
					writeJSON(t, w, map[string]any{
						"status": "success",
						"data": map[string]any{
							"groups": []map[string]any{
								{
									"name": "mimir.rules",
									"file": "rules/mimir.yaml",
									"rules": []map[string]any{
										{
											"name":           "MimirBurn",
											"type":           "alerting",
											"health":         "ok",
											"query":          "sum(rate(secret_total[5m])) > 3",
											"lastEvaluation": "2026-09-30T12:00:00Z",
											"labels":         map[string]any{"service": "mimir"},
											"annotations":    map[string]any{"summary": "mimir alert"},
										},
									},
								},
							},
						},
					})
				})
			},
			expectNoTarget: true,
		},
		{
			name:          "prometheus truncated",
			provider:      ProviderPrometheus,
			scopeID:       "prometheus:parity:truncated",
			instanceID:    "prometheus-parity-trunc",
			generationID:  "prometheus-mimir:8864:trunc:1",
			fencingToken:  11,
			resourceLimit: 1,
			staleAfter:    time.Hour,
			serve: func(t *testing.T, mux *http.ServeMux, seen *paritySeen) {
				t.Helper()
				mux.HandleFunc("/api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
					writeJSON(t, w, parityPrometheusTargets())
				})
				mux.HandleFunc("/api/v1/rules", func(w http.ResponseWriter, r *http.Request) {
					writeJSON(t, w, map[string]any{
						"status": "success",
						"data": map[string]any{
							"groups": []map[string]any{
								{
									"name": "trunc.rules",
									"file": "rules/trunc.yaml",
									"rules": []map[string]any{
										{
											"name":           "TruncBurn",
											"type":           "alerting",
											"health":         "ok",
											"query":          "up == 0",
											"lastEvaluation": "2026-09-30T12:00:00Z",
										},
									},
								},
							},
						},
					})
				})
			},
		},
	}
}

// parityLiveClaim runs one scope through the real HTTPClient against its
// httptest server and returns the claimed generation plus server
// observations.
func parityLiveClaim(t *testing.T, ps parityScope) (collector.CollectedGeneration, *paritySeen) {
	t.Helper()
	seen := &paritySeen{}
	mux := http.NewServeMux()
	ps.serve(t, mux, seen)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

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
		t.Fatalf("NewClaimedSource(%s) error = %v, want nil", ps.name, err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID:             ps.scopeID,
		GenerationID:        ps.generationID,
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-prometheus-parity",
		CurrentFencingToken: ps.fencingToken,
	})
	if err != nil {
		t.Fatalf("NextClaimed(%s) error = %v, want nil", ps.name, err)
	}
	if !ok {
		t.Fatalf("NextClaimed(%s) ok = false, want true", ps.name)
	}
	return collected, seen
}

func paritySnapshot(collected collector.CollectedGeneration) []facts.Envelope {
	var envelopes []facts.Envelope
	for envelope := range collected.Facts {
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func parityKindMultiset(envelopes []facts.Envelope) map[string]int {
	counts := map[string]int{}
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func parityStableKeys(envelopes []facts.Envelope) []string {
	keys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		keys = append(keys, envelope.StableFactKey)
	}
	slices.Sort(keys)
	return keys
}

// parityCanonicalPayload renders a payload with clock-owned fields removed
// so live runs and the recorded cassette compare on identity-bearing
// content only.
func parityCanonicalPayload(t *testing.T, payload map[string]any) string {
	t.Helper()
	clean := maps.Clone(payload)
	delete(clean, "observed_at")
	rendered, err := json.Marshal(clean)
	if err != nil {
		t.Fatalf("marshal payload error = %v, want nil", err)
	}
	return string(rendered)
}

// TestPrometheusMimir8864ParityLiveAgreesWithCassette is the AC3
// source-parity pin: the live claim and the checked-in cassette agree scope
// by scope on scope ID, generation ID, fact-kind multiset, stable keys,
// boundary attributes, and payloads. A cutover candidate must reproduce this
// same agreement against the same fixture.
func TestPrometheusMimir8864ParityLiveAgreesWithCassette(t *testing.T) {
	t.Parallel()

	recorded, err := cassette.NewSource("testdata/8864-parity-prometheus-mimir.json")
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	type snapshot struct {
		scopeID      string
		generationID string
		kinds        map[string]int
		keys         []string
		attrs        map[string]string
		payloads     map[string]string
		envelopes    []facts.Envelope
	}

	// attrKey binds the host-enforced boundary attributes to the stable key
	// so the cassette pins what admission and grant checks key on.
	attrKey := func(envelope facts.Envelope) string {
		return envelope.StableFactKey + "\x00" + envelope.FactKind + "\x00" +
			envelope.SchemaVersion + "\x00" + strconv.FormatInt(envelope.FencingToken, 10) +
			"\x00" + envelope.SourceRef.SourceRecordID
	}
	var want []snapshot
	for {
		collected, ok, err := recorded.Next(context.Background())
		if err != nil {
			t.Fatalf("cassette Next() error = %v", err)
		}
		if !ok {
			break
		}
		envelopes := paritySnapshot(collected)
		if len(envelopes) == 0 {
			t.Fatalf("cassette scope %q has no facts", collected.Scope.ScopeID)
		}
		attrs := map[string]string{}
		payloads := map[string]string{}
		for _, envelope := range envelopes {
			attrs[envelope.StableFactKey] = attrKey(envelope)
			payloads[envelope.StableFactKey] = parityCanonicalPayload(t, envelope.Payload)
		}
		want = append(want, snapshot{
			scopeID:      collected.Scope.ScopeID,
			generationID: collected.Generation.GenerationID,
			kinds:        parityKindMultiset(envelopes),
			keys:         parityStableKeys(envelopes),
			attrs:        attrs,
			payloads:     payloads,
			envelopes:    envelopes,
		})
	}
	if len(want) != 3 {
		t.Fatalf("cassette scopes = %d, want 3", len(want))
	}

	scopes := parityScopes()
	for i, expected := range want {
		ps := scopes[i]
		if expected.scopeID != ps.scopeID {
			t.Fatalf("cassette scope[%d] = %q, want %q", i, expected.scopeID, ps.scopeID)
		}
		collected, seen := parityLiveClaim(t, ps)
		if collected.Scope.ScopeID != expected.scopeID {
			t.Fatalf("live ScopeID = %q, want %q", collected.Scope.ScopeID, expected.scopeID)
		}
		if collected.Generation.GenerationID != expected.generationID {
			t.Fatalf("live GenerationID = %q, want %q", collected.Generation.GenerationID, expected.generationID)
		}
		if got := collected.Scope.PartitionKey; got != ps.instanceID {
			t.Fatalf("live PartitionKey = %q, want %q", got, ps.instanceID)
		}
		envelopes := paritySnapshot(collected)
		if !maps.Equal(parityKindMultiset(envelopes), expected.kinds) {
			t.Fatalf("live kinds = %v, want %v", parityKindMultiset(envelopes), expected.kinds)
		}
		if !slices.Equal(parityStableKeys(envelopes), expected.keys) {
			t.Fatalf("live stable keys differ from cassette for scope %q", expected.scopeID)
		}
		gotAttrs := map[string]string{}
		gotPayloads := map[string]string{}
		for _, envelope := range envelopes {
			gotAttrs[envelope.StableFactKey] = attrKey(envelope)
			gotPayloads[envelope.StableFactKey] = parityCanonicalPayload(t, envelope.Payload)
		}
		if !maps.Equal(gotAttrs, expected.attrs) {
			t.Fatalf("live boundary attributes differ from cassette for scope %q", expected.scopeID)
		}
		if !maps.Equal(gotPayloads, expected.payloads) {
			t.Fatalf("live payloads differ from cassette for scope %q", expected.scopeID)
		}
		for _, envelope := range envelopes {
			if envelope.SchemaVersion != "1.0.0" {
				t.Fatalf("SchemaVersion = %q, want 1.0.0 for kind %q", envelope.SchemaVersion, envelope.FactKind)
			}
			if envelope.CollectorKind != CollectorKind {
				t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
			}
			if envelope.SourceConfidence != facts.SourceConfidenceReported {
				t.Fatalf("SourceConfidence = %q, want reported", envelope.SourceConfidence)
			}
		}
		if ps.expectNoTarget && len(seen.targetPaths) != 0 {
			t.Fatalf("mimir requested targets %v, want no target calls", seen.targetPaths)
		}
		if ps.tenantID != "" {
			if len(seen.tenant) == 0 {
				t.Fatal("mimir rules request is missing, want tenant header observation")
			}
			for _, got := range seen.tenant {
				if got == "" {
					t.Fatal("mimir rules request omitted X-Scope-OrgID, want tenant header")
				}
			}
		}
	}

	union := map[string]bool{}
	for _, s := range want {
		for kind := range s.kinds {
			union[kind] = true
		}
	}
	for _, kind := range []string{
		facts.ObservabilitySourceInstanceFactKind,
		facts.ObservabilityObservedTargetFactKind,
		facts.ObservabilityObservedRuleFactKind,
		facts.ObservabilityCoverageWarningFactKind,
	} {
		if !union[kind] {
			t.Fatalf("cassette covers no %q fact; parity pack must span all 4 live kinds", kind)
		}
	}
}

// TestPrometheusMimir8864ParityDuplicateDeliveryIsStable pins duplicate
// safety: two identical claims emit identical stable-key sets, so a
// redelivered generation commits idempotently at the core boundary.
func TestPrometheusMimir8864ParityDuplicateDeliveryIsStable(t *testing.T) {
	t.Parallel()

	scopes := parityScopes()
	first, _ := parityLiveClaim(t, scopes[0])
	second, _ := parityLiveClaim(t, scopes[0])
	if !slices.Equal(parityStableKeys(paritySnapshot(first)), parityStableKeys(paritySnapshot(second))) {
		t.Fatal("duplicate claims emit different stable keys; redelivery would not be idempotent")
	}
}

// TestPrometheusMimir8864ParityInTreeNeedsNoGrant documents the deliberate
// delegation delta: the in-tree first-party path emits with no grant
// lookup, so this asserts emission succeeds grant-free. The extracted
// producer must instead fail closed on a missing, expired, or revoked grant
// (PRODUCER_GRANT_DRAFT.md); the fencing token and collector kind on every
// envelope are the host-side inputs that enforcement keys on.
func TestPrometheusMimir8864ParityInTreeNeedsNoGrant(t *testing.T) {
	t.Parallel()

	collected, _ := parityLiveClaim(t, parityScopes()[1])
	envelopes := paritySnapshot(collected)
	if len(envelopes) == 0 {
		t.Fatal("NextClaimed() emitted no facts, want grant-free emission")
	}
	for _, envelope := range envelopes {
		if envelope.FencingToken != 9 {
			t.Fatalf("FencingToken = %d, want 9", envelope.FencingToken)
		}
		if envelope.CollectorKind != CollectorKind {
			t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
		}
		if strings.TrimSpace(envelope.StableFactKey) == "" {
			t.Fatalf("%s StableFactKey is blank", envelope.FactKind)
		}
	}
}
