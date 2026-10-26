// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package loki parity pin for #8860 (AC3, in-tree side).
//
// These tests prove the CURRENT in-tree source behavior that the #4047
// cutover gate must reproduce exactly: the same scopes, kinds, stable keys,
// fencing tokens, and in-band failure signals, whether the facts come from a
// live claim or from the checked-in parity pack
// (testdata/8860-parity-loki.json, cut from the same fixed generations).
// The #6710 compatibility harness reuses this pack: the live side is the
// oracle, the recorded expectation is the cutover comparison.
//
// The live side drives the real ClaimedSource.NextClaimed path (claim
// binding, envelope construction, generation-scoped commit shape) against a
// stub EvidenceClient, so provider HTTP semantics stay under the existing
// http_client tests while emission-half identity stays pinned here.
//
// Out of scope here (core-owned, recorded for the external producer):
// grant revocation enforcement (see PRODUCER_GRANT_DRAFT.md) and durable
// commit/lease semantics owned by the ingestion store and workflow control
// store. The in-tree path pins the emission half; the external path must
// additionally fail closed on missing, expired, or revoked grants.
//
// No-Observability-Change: existing eshu_dp_loki_* signals cover this
// package; this test adds no metric, span, or pipeline stage.

package loki

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// parityObservedAt is the fixed generation timestamp recorded in the
// fixture scopes. Stub results carry it explicitly, so every
// identity-bearing field is deterministic across runs.
var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// paritySignal mirrors one recorded log-signal observation.
type paritySignal struct {
	ProviderObjectID string              `json:"provider_object_id"`
	SignalKind       string              `json:"signal_kind"`
	LabelKeys        []string            `json:"label_keys"`
	LabelValueCounts map[string]int      `json:"label_value_counts"`
	LabelValueHashes map[string][]string `json:"label_value_hashes"`
	SeriesFinger     string              `json:"series_fingerprint"`
	FreshnessState   string              `json:"freshness_state"`
	Outcome          string              `json:"outcome"`
}

// parityRule mirrors one recorded ruler-rule observation.
type parityRule struct {
	ProviderObjectID string   `json:"provider_object_id"`
	Namespace        string   `json:"namespace"`
	GroupName        string   `json:"group_name"`
	RuleName         string   `json:"rule_name"`
	RuleType         string   `json:"rule_type"`
	Query            string   `json:"query"`
	LabelKeys        []string `json:"label_keys"`
	AnnotationKeys   []string `json:"annotation_keys"`
	FreshnessState   string   `json:"freshness_state"`
	Outcome          string   `json:"outcome"`
}

// parityWarning mirrors one recorded coverage warning.
type parityWarning struct {
	ResourceClass string `json:"resource_class"`
	ResourceID    string `json:"resource_id"`
	Reason        string `json:"reason"`
}

// parityScope binds one claim to its recorded fixture scope.
type parityScope struct {
	Name             string          `json:"name"`
	ScopeID          string          `json:"scope_id"`
	InstanceID       string          `json:"instance_id"`
	GenerationID     string          `json:"generation_id"`
	FencingToken     int64           `json:"fencing_token"`
	TenantConfigured bool            `json:"tenant_configured"`
	SourceInstanceID string          `json:"source_instance_id"`
	Signals          []paritySignal  `json:"signals"`
	Rules            []parityRule    `json:"rules"`
	Warnings         []parityWarning `json:"warnings"`
	ExpectedKinds    map[string]int  `json:"expected_kinds"`
}

// parityPack is the recorded cutover comparison input.
type parityPack struct {
	Issue         int           `json:"issue"`
	CollectorKind string        `json:"collector_kind"`
	SchemaVersion string        `json:"schema_version"`
	Scopes        []parityScope `json:"scopes"`
}

func loadParityPack(t *testing.T) parityPack {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "8860-parity-loki.json"))
	if err != nil {
		t.Fatalf("ReadFile(8860-parity-loki.json) error = %v, want nil", err)
	}
	var pack parityPack
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatalf("Unmarshal parity pack error = %v, want nil", err)
	}
	if pack.CollectorKind != CollectorKind {
		t.Fatalf("pack collector_kind = %q, want %q", pack.CollectorKind, CollectorKind)
	}
	if pack.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
		t.Fatalf("pack schema_version = %q, want 1.0.0", pack.SchemaVersion)
	}
	if len(pack.Scopes) == 0 {
		t.Fatal("parity pack holds no scopes")
	}
	return pack
}

func parityCollectionResult(ps parityScope) CollectionResult {
	result := CollectionResult{
		Source: SourceInstance{
			Provider:         ProviderLoki,
			SourceInstanceID: ps.SourceInstanceID,
		},
		ObservedAt: parityObservedAt,
	}
	if ps.TenantConfigured {
		result.Source.TenantPresent = true
		result.Source.TenantFingerprint = fingerprint("tenant-prod")
		result.Source.TenantRedacted = true
	}
	for _, s := range ps.Signals {
		result.Signals = append(result.Signals, LogSignal{
			ProviderObjectID:  s.ProviderObjectID,
			SignalKind:        s.SignalKind,
			LabelKeys:         s.LabelKeys,
			LabelValueCounts:  s.LabelValueCounts,
			LabelValueHashes:  s.LabelValueHashes,
			SeriesFingerprint: s.SeriesFinger,
			LastSeenAt:        parityObservedAt,
			FreshnessState:    s.FreshnessState,
			Outcome:           s.Outcome,
		})
	}
	for _, r := range ps.Rules {
		result.Rules = append(result.Rules, Rule{
			ProviderObjectID: r.ProviderObjectID,
			Namespace:        r.Namespace,
			GroupName:        r.GroupName,
			RuleName:         r.RuleName,
			RuleType:         r.RuleType,
			Query:            r.Query,
			QueryRedacted:    r.Query != "",
			LabelKeys:        r.LabelKeys,
			AnnotationKeys:   r.AnnotationKeys,
			FreshnessState:   r.FreshnessState,
			Outcome:          r.Outcome,
		})
	}
	for _, w := range ps.Warnings {
		result.Warnings = append(result.Warnings, Warning(w))
	}
	return result
}

func parityClaimedSource(t *testing.T, ps parityScope, result CollectionResult) *ClaimedSource {
	t.Helper()

	client := &stubEvidenceClient{result: result}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-loki-parity",
		Targets: []TargetConfig{{
			ScopeID:       ps.ScopeID,
			InstanceID:    ps.InstanceID,
			BaseURL:       "https://loki.example.test",
			ResourceLimit: 100,
			Enabled:       true,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return parityObservedAt },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource(%s) error = %v, want nil", ps.Name, err)
	}
	return source
}

func parityWorkItem(ps parityScope) workflow.WorkItem {
	return workflow.WorkItem{
		ScopeID:             ps.ScopeID,
		GenerationID:        ps.GenerationID,
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-loki-parity",
		CurrentFencingToken: ps.FencingToken,
	}
}

func collectParityFacts(t *testing.T, ps parityScope, source *ClaimedSource) []facts.Envelope {
	t.Helper()

	collected, ok, err := source.NextClaimed(context.Background(), parityWorkItem(ps))
	if err != nil {
		t.Fatalf("NextClaimed(%s) error = %v, want nil", ps.Name, err)
	}
	if !ok {
		t.Fatalf("NextClaimed(%s) ok = false, want true", ps.Name)
	}
	envs := collectFacts(t, collected)
	if len(envs) == 0 {
		t.Fatalf("NextClaimed(%s) emitted no facts", ps.Name)
	}
	return envs
}

func TestParity8860KindCensusMatchesFixture(t *testing.T) {
	t.Parallel()

	pack := loadParityPack(t)
	for _, ps := range pack.Scopes {
		source := parityClaimedSource(t, ps, parityCollectionResult(ps))
		envs := collectParityFacts(t, ps, source)

		counts := countByFactKind(envs)
		for kind, want := range ps.ExpectedKinds {
			if got := counts[kind]; got != want {
				t.Errorf("scope %s kind %s count = %d, want %d", ps.Name, kind, got, want)
			}
			delete(counts, kind)
		}
		for kind, got := range counts {
			t.Errorf("scope %s emitted unexpected kind %s x%d", ps.Name, kind, got)
		}
		assertStableKeysUnique(t, envs)

		for _, env := range envs {
			if env.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
				t.Errorf("scope %s envelope %q SchemaVersion = %q, want 1.0.0", ps.Name, env.FactKind, env.SchemaVersion)
			}
			if env.CollectorKind != CollectorKind {
				t.Errorf("scope %s envelope %q CollectorKind = %q, want loki", ps.Name, env.FactKind, env.CollectorKind)
			}
			if env.SourceConfidence != facts.SourceConfidenceReported {
				t.Errorf("scope %s envelope %q SourceConfidence = %q, want reported", ps.Name, env.FactKind, env.SourceConfidence)
			}
			if env.GenerationID != ps.GenerationID {
				t.Errorf("scope %s envelope %q GenerationID = %q, want %q", ps.Name, env.FactKind, env.GenerationID, ps.GenerationID)
			}
			if env.FencingToken != ps.FencingToken {
				t.Errorf("scope %s envelope %q FencingToken = %d, want %d", ps.Name, env.FactKind, env.FencingToken, ps.FencingToken)
			}
		}
	}
}

func TestParity8860StableKeysDeterministicAndGenerationBound(t *testing.T) {
	t.Parallel()

	pack := loadParityPack(t)
	ps := pack.Scopes[0]

	first := collectParityFacts(t, ps, parityClaimedSource(t, ps, parityCollectionResult(ps)))
	second := collectParityFacts(t, ps, parityClaimedSource(t, ps, parityCollectionResult(ps)))
	if len(first) != len(second) {
		t.Fatalf("repeat emission count = %d, want %d", len(second), len(first))
	}
	byKind := map[string]string{}
	for _, env := range first {
		byKind[env.FactKind+env.StableFactKey] = env.StableFactKey
	}
	for _, env := range second {
		if byKind[env.FactKind+env.StableFactKey] == "" {
			t.Errorf("repeat emission lost stable key %q for %q", env.StableFactKey, env.FactKind)
		}
		if !strings.HasPrefix(env.StableFactKey, env.FactKind+":"+ps.GenerationID+":") {
			t.Errorf("stable key %q does not bind kind+generation", env.StableFactKey)
		}
	}

	rebound := ps
	rebound.GenerationID = ps.GenerationID + "-retry"
	reboundEnvs := collectParityFacts(t, rebound, parityClaimedSource(t, rebound, parityCollectionResult(rebound)))
	prior := map[string]struct{}{}
	for _, env := range first {
		prior[env.StableFactKey] = struct{}{}
	}
	for _, env := range reboundEnvs {
		if _, exists := prior[env.StableFactKey]; exists {
			t.Errorf("rebound generation reused stable key %q", env.StableFactKey)
		}
	}
}

func TestParity8860LabelValueHashesFlowThrough(t *testing.T) {
	t.Parallel()

	pack := loadParityPack(t)
	for _, ps := range pack.Scopes {
		envs := collectParityFacts(t, ps, parityClaimedSource(t, ps, parityCollectionResult(ps)))
		for _, s := range ps.Signals {
			if len(s.LabelValueHashes) == 0 {
				continue
			}
			found := false
			for _, env := range envs {
				if env.FactKind != facts.ObservabilityObservedLogSignalFactKind {
					continue
				}
				if env.Payload["provider_object_uid"] != s.ProviderObjectID {
					continue
				}
				found = true
				got, ok := env.Payload["label_value_hashes"].(map[string][]string)
				if !ok {
					t.Errorf("scope %s signal %s label_value_hashes missing: %#v", ps.Name, s.ProviderObjectID, env.Payload["label_value_hashes"])
					continue
				}
				if !reflect.DeepEqual(got, s.LabelValueHashes) {
					t.Errorf("scope %s signal %s label_value_hashes = %#v, want %#v", ps.Name, s.ProviderObjectID, got, s.LabelValueHashes)
				}
			}
			if !found {
				t.Errorf("scope %s signal %s: no observed_log_signal envelope", ps.Name, s.ProviderObjectID)
			}
		}
	}
}

func TestParity8860RedactionSentinel(t *testing.T) {
	t.Parallel()

	pack := loadParityPack(t)
	for _, ps := range pack.Scopes {
		envs := collectParityFacts(t, ps, parityClaimedSource(t, ps, parityCollectionResult(ps)))
		for _, env := range envs {
			assertPayload(t, env, "redaction_version", RedactionVersion)
			assertPayloadKeysAbsent(t, env, "token", "tenant_id", "query", "logql", "label_values")
			assertPayloadForbidden(t, env, "tenant-prod", "payment failed", "{app=\"checkout\"}")
			if ps.TenantConfigured && env.FactKind == facts.ObservabilitySourceInstanceFactKind {
				assertPayload(t, env, "tenant_redacted", true)
				assertPayload(t, env, "tenant_scope_state", "configured")
			}
		}
	}
}

func TestParity8860FailClosedHostileInputs(t *testing.T) {
	t.Parallel()

	pack := loadParityPack(t)
	ps := pack.Scopes[0]
	source := parityClaimedSource(t, ps, parityCollectionResult(ps))

	blank := parityWorkItem(ps)
	blank.GenerationID = ""
	if _, _, err := source.NextClaimed(context.Background(), blank); err == nil {
		t.Error("NextClaimed(blank generation) = nil, want generation-required rejection")
	}

	unknown := parityWorkItem(ps)
	unknown.ScopeID = "loki:tenant:missing"
	if _, ok, err := source.NextClaimed(context.Background(), unknown); err == nil || ok {
		t.Errorf("NextClaimed(unknown scope) = (%v, %v), want retryable failure without facts", ok, err)
	}

	foreign := parityWorkItem(ps)
	foreign.CollectorInstanceID = "collector-loki-foreign"
	if _, _, err := source.NextClaimed(context.Background(), foreign); err == nil {
		t.Error("NextClaimed(foreign instance) = nil, want instance-mismatch rejection")
	}

	emptyCtx := EnvelopeContext{
		ScopeID:             ps.ScopeID,
		GenerationID:        ps.GenerationID,
		CollectorInstanceID: "collector-loki-parity",
		SourceInstanceID:    "",
		ObservedAt:          parityObservedAt,
	}
	if _, err := NewObservedLogSignalEnvelope(emptyCtx, LogSignal{ProviderObjectID: "x"}); err == nil {
		t.Error("NewObservedLogSignalEnvelope(blank source_instance_id) = nil, want rejection")
	}
	if _, err := NewObservedRuleEnvelope(emptyCtx, Rule{ProviderObjectID: "x"}); err == nil {
		t.Error("NewObservedRuleEnvelope(blank source_instance_id) = nil, want rejection")
	}
	if _, err := NewCoverageWarningEnvelope(emptyCtx, Warning{Reason: WarningPartial}); err == nil {
		t.Error("NewCoverageWarningEnvelope(blank source_instance_id) = nil, want rejection")
	}
}
