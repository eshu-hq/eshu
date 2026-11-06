// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestActivationDerivedScopesRecordOwnedDerivedSelection is the #10188
// activation-path half: a vulnerability activation whose owned read selects
// targets must record the per-ecosystem selection on the owned-derived
// counters. The activation planner returns no skip evidence, so the
// activation records selected-only while skipped stays in-tree-only by
// contract. Before the fix the activation path performed the read and the
// window log but never recorded, so the counters stayed dark on instances
// that plan exclusively through activations.
func TestActivationDerivedScopesRecordOwnedDerivedSelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 10, 30, 0, 0, time.UTC)
	instance := vulnerabilityActivationWithDerive(now,
		`{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm","maven"]},`+
			`"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["alpine"]}}`)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reader := ownedViteReader()
	metrics := &fakeMetrics{}
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)
	service.Metrics = metrics
	service.OSPackageAdvisoryTargetReader = &fakeOSPackageAdvisoryTargetReader{}

	if err := service.scheduleComponentExtensionWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleComponentExtensionWork() error = %v, want nil", err)
	}
	if len(metrics.ownedDerivedObserved) != 1 {
		t.Fatalf("owned derived observations = %d, want 1", len(metrics.ownedDerivedObserved))
	}
	observation := metrics.ownedDerivedObserved[0]
	if observation.CollectorInstanceID != instance.InstanceID {
		t.Errorf("instance = %q, want %q", observation.CollectorInstanceID, instance.InstanceID)
	}
	if got := observation.Selected["npm"]; got != 1 {
		t.Errorf("selected npm = %d, want 1", got)
	}
	if got := observation.Selected["maven"]; got != 1 {
		t.Errorf("selected maven = %d, want 1", got)
	}
	if len(observation.Skipped) != 0 {
		t.Errorf("skipped = %v, want empty on the activation path", observation.Skipped)
	}
}
