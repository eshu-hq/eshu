// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tempo parity pin for #8867 (AC3, in-tree side).
//
// These tests prove the CURRENT in-tree source behavior that the #4047
// cutover gate must reproduce exactly: the same scopes, kinds, stable keys,
// fencing tokens, and in-band failure signals, whether the facts come from a
// live claim or from the checked-in golden
// (testdata/8867-parity-tempo.json, cut from a live HTTP run against
// httptest servers with a fixed generation). The #6710 compatibility
// harness reuses this pack: the live side is the oracle, the golden is the
// recorded expectation.
//
// The live side drives the real HTTPClient against httptest servers, not a
// stub EvidenceClient, so provider HTTP semantics (tag/values normalization,
// high-cardinality caps, permission-hidden degradation) stay under test.
//
// Out of scope here (core-owned, recorded for the external producer):
// grant revocation enforcement (see PRODUCER_GRANT_DRAFT) and durable
// commit/lease semantics owned by the ingestion store and workflow control
// store. The in-tree path pins the emission half; the external path must
// additionally fail closed on missing, expired, or revoked grants.
//
// No-Observability-Change: existing eshu_dp_tempo_* signals cover this
// package; this test adds no metric, span, or pipeline stage.

package tempo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// parityObservedAt is the fixed generation timestamp recorded in the
// golden. Live runs use target.Now, which the comparison fixes; everything
// identity-bearing is deterministic.
var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func parityTempoServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/echo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v2/search/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagNames":["resource.service.name","span.http.route"],` +
			`"scopes":[{"name":"resource","tags":["resource.service.name"]}]}`))
	})
	mux.HandleFunc("/api/v2/search/tag/resource.service.name/values", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tagValues":[` +
			`{"type":"string","value":"checkout"},` +
			`{"type":"string","value":"billing"}]}`))
	})
	return httptest.NewServer(mux)
}

func parityTempoSource(t *testing.T, serverURL string) *ClaimedSource {
	t.Helper()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-tempo-parity",
		Now:                 func() time.Time { return parityObservedAt },
		Targets: []TargetConfig{{
			ScopeID:              "tempo:parity:prod",
			InstanceID:           "tempo-parity",
			BaseURL:              serverURL,
			ResourceLimit:        100,
			TagValueNames:        []string{"resource.service.name"},
			MaxTagValuesPerTag:   20,
			Enabled:              true,
			FreshnessProbeEnable: true,
			Lookback:             time.Hour,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return source
}

func parityTempoItem() workflow.WorkItem {
	return workflow.WorkItem{
		CollectorInstanceID: "collector-tempo-parity",
		CollectorKind:       CollectorKind,
		ScopeID:             "tempo:parity:prod",
		GenerationID:        "tempo:8867:prod:1",
		CurrentFencingToken: 7,
	}
}

func collectParityEnvelopes(t *testing.T, source *ClaimedSource) []facts.Envelope {
	t.Helper()

	generation, ok, err := source.NextClaimed(context.Background(), parityTempoItem())
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	var envs []facts.Envelope
	for env := range generation.Facts {
		envs = append(envs, env)
	}
	return envs
}

func canonicalParityJSON(t *testing.T, envs []facts.Envelope) []byte {
	t.Helper()

	raw, err := json.MarshalIndent(envs, "", "  ")
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	return append(raw, '\n')
}

func TestParity8867GoldenByteIdentical(t *testing.T) {
	t.Parallel()

	server := parityTempoServer(t)
	defer server.Close()

	first := canonicalParityJSON(t, collectParityEnvelopes(t, parityTempoSource(t, server.URL)))
	second := canonicalParityJSON(t, collectParityEnvelopes(t, parityTempoSource(t, server.URL)))
	if !bytes.Equal(first, second) {
		t.Fatal("live parity run not deterministic across identical claims")
	}

	golden, err := os.ReadFile(filepath.Join("testdata", "8867-parity-tempo.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(first, golden) {
		t.Errorf("live envelopes differ from testdata/8867-parity-tempo.json (%d live vs %d golden bytes); the extracted producer must reproduce these bytes exactly", len(first), len(golden))
	}

	var envs []facts.Envelope
	if err := json.Unmarshal(first, &envs); err != nil {
		t.Fatalf("unmarshal live envelopes: %v", err)
	}
	if len(envs) != 3 {
		t.Fatalf("len(envelopes) = %d, want 3 (source_instance + 2 trace_signals)", len(envs))
	}
	wantKinds := []string{
		facts.ObservabilitySourceInstanceFactKind,
		facts.ObservabilityObservedTraceSignalFactKind,
		facts.ObservabilityObservedTraceSignalFactKind,
	}
	for i, want := range wantKinds {
		if envs[i].FactKind != want {
			t.Errorf("envelopes[%d].FactKind = %q, want %q", i, envs[i].FactKind, want)
		}
		if envs[i].SchemaVersion != facts.ObservabilitySchemaVersionV1 {
			t.Errorf("envelopes[%d].SchemaVersion = %q, want 1.0.0", i, envs[i].SchemaVersion)
		}
		if envs[i].GenerationID != "tempo:8867:prod:1" || envs[i].FencingToken != 7 {
			t.Errorf("envelopes[%d] lost generation binding: %+v", i, envs[i])
		}
		if envs[i].CollectorKind != CollectorKind {
			t.Errorf("envelopes[%d].CollectorKind = %q, want %q", i, envs[i].CollectorKind, CollectorKind)
		}
	}
}

func TestParity8867PermissionHiddenDegrades(t *testing.T) {
	t.Parallel()

	// A 403 on the tags endpoint degrades to a permission_hidden coverage
	// warning on the in-tree path; the claim still completes with the
	// source_instance fact. The extracted producer must reproduce this
	// degraded shape, and additionally fail closed without a live grant.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	envs := collectParityEnvelopes(t, parityTempoSource(t, server.URL))
	// The freshness probe and the tags fetch each degrade independently:
	// one permission_hidden warning per denied endpoint, claim still
	// completes with the source_instance fact.
	if len(envs) != 3 {
		t.Fatalf("len(envelopes) = %d, want 3 (source_instance + 2 coverage_warnings)", len(envs))
	}
	if envs[0].FactKind != facts.ObservabilitySourceInstanceFactKind {
		t.Errorf("envelopes[0].FactKind = %q, want source_instance", envs[0].FactKind)
	}
	if envs[0].Payload["partial"] != true {
		t.Errorf("source partial = %v, want true after denied endpoints", envs[0].Payload["partial"])
	}
	for i, env := range envs[1:] {
		if env.FactKind != facts.ObservabilityCoverageWarningFactKind {
			t.Fatalf("envelopes[%d].FactKind = %q, want coverage_warning", i+1, env.FactKind)
		}
		if env.Payload["warning_kind"] != WarningPermissionHidden {
			t.Errorf("envelopes[%d] warning_kind = %v, want %q", i+1, env.Payload["warning_kind"], WarningPermissionHidden)
		}
	}
}
