// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"sync"
	"testing"
	"time"
)

// grantDecisionRecorder collects observed decisions for assertions.
type grantDecisionRecorder struct {
	mu        sync.Mutex
	decisions []GrantDecision
}

func (r *grantDecisionRecorder) ObserveGrantDecision(_ context.Context, d GrantDecision) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisions = append(r.decisions, d)
}

func (r *grantDecisionRecorder) snapshot() []GrantDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]GrantDecision(nil), r.decisions...)
}

func baseGrantForDecisions() ProducerGrant {
	return ProducerGrant{
		ProducerID:     "dev.example.collector.pick",
		Version:        "0.1.0",
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "aws",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
}

// TestClassifyEmissionReasons pins the closed deny-reason enum and proves
// ClassifyEmission never disagrees with the fail-closed AuthorizesEmission.
func TestClassifyEmissionReasons(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	cases := []struct {
		name   string
		mutate func(*ProducerGrant)
		want   GrantReason
	}{
		{"live grant", func(*ProducerGrant) {}, GrantReasonGranted},
		{"revoked", func(g *ProducerGrant) { g.Revoked = true }, GrantReasonRevoked},
		{"expired", func(g *ProducerGrant) { g.ExpiresAt = now.Add(-time.Hour) }, GrantReasonExpired},
		{"wrong producer", func(g *ProducerGrant) { g.ProducerID = "dev.example.collector.other" }, GrantReasonNoMatchingGrant},
		{"wrong version", func(g *ProducerGrant) { g.Version = "0.2.0" }, GrantReasonNoMatchingGrant},
		{"wrong kind", func(g *ProducerGrant) { g.Kind = "vpc" }, GrantReasonNoMatchingGrant},
		{"wrong scope", func(g *ProducerGrant) { g.Scope = "gcp" }, GrantReasonScopeMismatch},
		{"narrower schema", func(g *ProducerGrant) { g.SchemaVersions = []string{"2.0.0"} }, GrantReasonSchemaNotCovered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			grant := baseGrantForDecisions()
			tc.mutate(&grant)
			grants := []ProducerGrant{grant}
			got := ClassifyEmission(grants, "dev.example.collector.pick", "0.1.0", "aws_resource", "1.0.0", []string{"aws"}, now)
			if got != tc.want {
				t.Fatalf("ClassifyEmission() = %q, want %q", got, tc.want)
			}
			authorized := AuthorizesEmission(grants, "dev.example.collector.pick", "0.1.0", "aws_resource", "1.0.0", []string{"aws"}, now)
			if authorized != (got == GrantReasonGranted) {
				t.Fatalf("AuthorizesEmission() = %v disagrees with ClassifyEmission() = %q", authorized, got)
			}
		})
	}
	if got := ClassifyEmission(nil, "dev.example.collector.pick", "0.1.0", "aws_resource", "1.0.0", []string{"aws"}, now); got != GrantReasonNoMatchingGrant {
		t.Fatalf("ClassifyEmission(no grants) = %q, want %q", got, GrantReasonNoMatchingGrant)
	}
}

// TestRegistryInstallObservesDenyAndStaysFailClosed is the mandatory negative
// authorization proof: a denied install both fails closed (nothing is
// installed) and records decision=deny with the stage and reason.
func TestRegistryInstallObservesDenyAndStaysFailClosed(t *testing.T) {
	t.Parallel()

	recorder := &grantDecisionRecorder{}
	registry := NewRegistry(t.TempDir()).WithGrantObserver(recorder)
	grant := baseGrantForDecisions()
	grant.Revoked = true
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	manifestYAML := componentManifestForFactKind(grant.ProducerID, "0.1.0", "aws_resource", []string{"1.0.0"})
	if _, err := registry.Install(writeManifest(t, manifestYAML), verificationFor(grant.ProducerID, "0.1.0")); err == nil {
		t.Fatal("Install(revoked grant) error = nil, want fail-closed rejection")
	}
	installed, err := registry.List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(installed) != 0 {
		t.Fatalf("installed components = %d after denied install, want 0", len(installed))
	}
	got := recorder.snapshot()
	want := GrantDecision{
		Stage: GrantStageInstall, Allowed: false, Reason: GrantReasonRevoked,
		ProducerID: grant.ProducerID, Version: "0.1.0", Kind: "aws_resource",
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("decisions = %+v, want exactly [%+v]", got, want)
	}
}

// TestRegistryInstallObservesAllow proves an approved install leaves a trace.
func TestRegistryInstallObservesAllow(t *testing.T) {
	t.Parallel()

	recorder := &grantDecisionRecorder{}
	registry := NewRegistry(t.TempDir()).WithGrantObserver(recorder)
	grant := baseGrantForDecisions()
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	manifestYAML := componentManifestForFactKind(grant.ProducerID, "0.1.0", "aws_resource", []string{"1.0.0"})
	if _, err := registry.Install(writeManifest(t, manifestYAML), verificationFor(grant.ProducerID, "0.1.0")); err != nil {
		t.Fatalf("Install(granted) error = %v, want nil", err)
	}
	got := recorder.snapshot()
	want := GrantDecision{
		Stage: GrantStageInstall, Allowed: true, Reason: GrantReasonGranted,
		ProducerID: grant.ProducerID, Version: "0.1.0", Kind: "aws_resource",
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("decisions = %+v, want exactly [%+v]", got, want)
	}
}

// TestRegistryInstallNonCoreKindRecordsNoDecision proves the signal covers
// only grant-governed kinds: a namespaced non-core kind needs no grant and
// leaves no allow noise.
func TestRegistryInstallNonCoreKindRecordsNoDecision(t *testing.T) {
	t.Parallel()

	recorder := &grantDecisionRecorder{}
	registry := NewRegistry(t.TempDir()).WithGrantObserver(recorder)
	manifestYAML := componentManifestForFactKind(
		"dev.example.collector.plain", "0.1.0", "dev.example.plain.check", []string{"1.0.0"},
	)
	if _, err := registry.Install(writeManifest(t, manifestYAML), verificationFor("dev.example.collector.plain", "0.1.0")); err != nil {
		t.Fatalf("Install(non-core) error = %v, want nil", err)
	}
	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("decisions = %+v, want none for a non-core kind", got)
	}
}

// TestRegistryReadbackAndEnableObserveOncePerDecision proves readback and
// enable each record their own stage once, PlanEnable records nothing, and
// enable does not re-report decisions for other installed components.
func TestRegistryReadbackAndEnableObserveOncePerDecision(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	grant := baseGrantForDecisions()
	setup := NewRegistry(home)
	if err := setup.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	for _, m := range []struct{ id, kind string }{
		{grant.ProducerID, "aws_resource"},
		{"dev.example.collector.plain", "dev.example.plain.check"},
	} {
		if _, err := setup.Install(
			writeManifest(t, componentManifestForFactKind(m.id, "0.1.0", m.kind, []string{"1.0.0"})),
			verificationFor(m.id, "0.1.0"),
		); err != nil {
			t.Fatalf("Install(%s) error = %v, want nil", m.id, err)
		}
	}

	recorder := &grantDecisionRecorder{}
	registry := NewRegistry(home).WithGrantObserver(recorder)
	if _, err := registry.Readback(Policy{}); err != nil {
		t.Fatalf("Readback() error = %v, want nil", err)
	}
	got := recorder.snapshot()
	if len(got) != 1 || got[0].Stage != GrantStageReadback || !got[0].Allowed || got[0].Reason != GrantReasonGranted {
		t.Fatalf("readback decisions = %+v, want one allow at stage readback", got)
	}

	recorder = &grantDecisionRecorder{}
	registry = NewRegistry(home).WithGrantObserver(recorder)
	activation := Activation{InstanceID: "instance-a"}
	if _, err := registry.PlanEnable(grant.ProducerID, activation); err != nil {
		t.Fatalf("PlanEnable() error = %v, want nil", err)
	}
	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("PlanEnable decisions = %+v, want none (dry run)", got)
	}
	if _, err := registry.Enable(grant.ProducerID, activation); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	got = recorder.snapshot()
	if len(got) != 1 || got[0].Stage != GrantStageActivation || !got[0].Allowed || got[0].ProducerID != grant.ProducerID {
		t.Fatalf("enable decisions = %+v, want one allow at stage activation for the candidate only", got)
	}
}

// TestRegistryReadbackObservesRevokedGrantDeny proves a revoked grant found
// at readback records a deny (and the entry surfaces the error as before).
func TestRegistryReadbackObservesRevokedGrantDeny(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	grant := baseGrantForDecisions()
	setup := NewRegistry(home)
	if err := setup.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	if _, err := setup.Install(
		writeManifest(t, componentManifestForFactKind(grant.ProducerID, "0.1.0", "aws_resource", []string{"1.0.0"})),
		verificationFor(grant.ProducerID, "0.1.0"),
	); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if err := setup.RevokeGrant(grant.ProducerID, "0.1.0", "aws_resource", "aws"); err != nil {
		t.Fatalf("RevokeGrant() error = %v, want nil", err)
	}
	recorder := &grantDecisionRecorder{}
	readback, err := NewRegistry(home).WithGrantObserver(recorder).Readback(Policy{})
	if err != nil {
		t.Fatalf("Readback() error = %v, want nil", err)
	}
	if len(readback) != 1 || readback[0].Error == nil {
		t.Fatalf("readback = %+v, want one entry carrying the fail-closed error", readback)
	}
	got := recorder.snapshot()
	if len(got) != 1 || got[0].Allowed || got[0].Reason != GrantReasonRevoked || got[0].Stage != GrantStageReadback {
		t.Fatalf("decisions = %+v, want one deny/revoked at stage readback", got)
	}
}
