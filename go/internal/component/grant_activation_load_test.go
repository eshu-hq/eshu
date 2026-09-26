// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import "testing"

// TestLoadInstalledManifestForActivationObservesAllow proves the activation
// loader reports one activation-stage allow for a granted core-owned family,
// and that the plain loader stays silent so callers that do not run the
// component add no decisions.
func TestLoadInstalledManifestForActivationObservesAllow(t *testing.T) {
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

	recorder := &grantDecisionRecorder{}
	registry := NewRegistry(home).WithGrantObserver(recorder)
	if _, err := registry.LoadInstalledManifest(grant.ProducerID, "0.1.0"); err != nil {
		t.Fatalf("LoadInstalledManifest() error = %v, want nil", err)
	}
	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("LoadInstalledManifest decisions = %+v, want none", got)
	}
	if _, err := registry.LoadInstalledManifestForActivation(grant.ProducerID, "0.1.0"); err != nil {
		t.Fatalf("LoadInstalledManifestForActivation() error = %v, want nil", err)
	}
	got := recorder.snapshot()
	if len(got) != 1 || got[0].Stage != GrantStageActivation || !got[0].Allowed || got[0].Reason != GrantReasonGranted {
		t.Fatalf("decisions = %+v, want one allow/granted at stage activation", got)
	}
}

// TestLoadInstalledManifestForActivationObservesRevokedDeny proves a grant
// revoked between readback and the activation reload is reported as a deny and
// fails the load closed, never as an allow.
func TestLoadInstalledManifestForActivationObservesRevokedDeny(t *testing.T) {
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
	_, err := NewRegistry(home).WithGrantObserver(recorder).LoadInstalledManifestForActivation(grant.ProducerID, "0.1.0")
	if err == nil {
		t.Fatal("LoadInstalledManifestForActivation() error = nil, want fail-closed error")
	}
	got := recorder.snapshot()
	if len(got) != 1 || got[0].Allowed || got[0].Reason != GrantReasonRevoked || got[0].Stage != GrantStageActivation {
		t.Fatalf("decisions = %+v, want one deny/revoked at stage activation", got)
	}
}
