// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Derived-target rotation for package-registry and vulnerability-intelligence
// instances must bucket on the same per-instance interval and the same
// truncated clock as the plan key. These cases pin that; the decode,
// validation, plan-key and scheduling cases live in scheduled_work_test.go.
// A vulnerability_intelligence instance reads three things per tick: the
// owned-package targets, the installed-evidence targets, and the plan key.
// All three must rotate and bucket on the same per-instance interval, else a
// widened instance keeps paging through a different slice of advisory targets
// every global tick under one frozen run ID and never stops admitting work
// (review finding F1 on #6720).
func TestServiceRunReconcileRotatesVulnerabilityTargetsOnInstanceScanInterval(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, time.June, 1, 17, 7, 0, 0, time.UTC)
	current := first
	instance := testServiceVulnerabilityIntelligenceInstance(first)
	instance.Configuration = `{
		"scan_interval": "1h",
		"derive_from_owned_packages": {"enabled": true, "sources": ["osv"], "ecosystems": ["npm"], "target_limit": 10},
		"derive_from_installed_evidence": {"enabled": true, "sources": ["osv"], "ecosystems": ["alpine", "npm"], "target_limit": 10}
	}`
	planner := &fakeVulnerabilityIntelligencePlanner{
		run: workflow.Run{
			RunID:              "vulnerability_intelligence:collector-vulnerability-intelligence:schedule:continuous-20260601T170000Z",
			TriggerKind:        workflow.TriggerKindSchedule,
			Status:             workflow.RunStatusCollectionPending,
			RequestedScopeSet:  "{}",
			RequestedCollector: string(scope.CollectorVulnerabilityIntelligence),
			CreatedAt:          first,
			UpdatedAt:          first,
		},
	}
	ownedReader := &rotatingOwnedPackageTargetReader{}
	osReader := &fakeOSPackageAdvisoryTargetReader{}
	sbomReader := &fakeSBOMComponentAdvisoryTargetReader{}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        5 * time.Minute,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: scope.CollectorVulnerabilityIntelligence,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                             &fakeStore{instances: []workflow.CollectorInstance{instance}},
		VulnerabilityIntelligencePlanner:  planner,
		OwnedPackageTargetReader:          ownedReader,
		OSPackageAdvisoryTargetReader:     osReader,
		SBOMComponentAdvisoryTargetReader: sbomReader,
		Clock:                             func() time.Time { return current },
	}

	for tick := 0; tick < 2; tick++ {
		current = first.Add(time.Duration(tick) * 5 * time.Minute)
		if err := service.runReconcile(context.Background()); err != nil {
			t.Fatalf("tick %d runReconcile() error = %v, want nil", tick, err)
		}
	}

	if len(ownedReader.requests) != 2 || len(osReader.requests) != 2 || len(sbomReader.requests) != 2 || len(planner.requests) != 2 {
		t.Fatalf("reader/planner requests = owned %d, os %d, sbom %d, planner %d; want 2 each",
			len(ownedReader.requests), len(osReader.requests), len(sbomReader.requests), len(planner.requests))
	}
	wantOffset := schedule.DerivedTargetRotationOffset(first, time.Hour, 10)
	for tick := 0; tick < 2; tick++ {
		if got := ownedReader.requests[tick].RotationOffset; got != wantOffset {
			t.Fatalf("tick %d owned rotation offset = %d, want %d (1h bucket)", tick, got, wantOffset)
		}
		if got := osReader.requests[tick].RotationOffset; got != wantOffset {
			t.Fatalf("tick %d OS package rotation offset = %d, want %d (1h bucket)", tick, got, wantOffset)
		}
		if got := sbomReader.requests[tick].RotationOffset; got != wantOffset {
			t.Fatalf("tick %d SBOM component rotation offset = %d, want %d (1h bucket)", tick, got, wantOffset)
		}
		if got, want := planner.requests[tick].PlanKey, "continuous-20260601T170000Z"; got != want {
			t.Fatalf("tick %d plan key = %q, want %q", tick, got, want)
		}
	}
}

// The plan key truncates the clock with time.Truncate, which rounds from Go's
// zero time, while the derived-target rotation offset used to divide UnixNano,
// which rounds from the Unix epoch. For an interval that does not divide the
// offset between the two (5h does not; 12h, 1h and 30s do) the two flipped at
// different moments, so a rotating package-registry or vulnerability instance
// could page to new targets under the old run ID. Both must change together
// (Codex review on #6724).
func TestDerivedTargetRotationOffsetFlipsWithThePlanKeyBucket(t *testing.T) {
	t.Parallel()

	const interval = 5 * time.Hour
	instance := workflow.CollectorInstance{Mode: workflow.CollectorModeContinuous}
	bucketStart := time.Date(2026, time.September, 17, 0, 0, 0, 0, time.UTC)
	if got := bucketStart.Truncate(interval); !got.Equal(bucketStart) {
		t.Fatalf("test premise: %s is not a %s truncate boundary (got %s)", bucketStart, interval, got)
	}
	if bucketStart.UnixNano()%int64(interval) == 0 {
		t.Fatalf("test premise: %s is epoch-aligned for %s; pick a boundary where the two epochs disagree", bucketStart, interval)
	}
	// The epoch-aligned 5h boundary that falls inside this truncate bucket.
	epochBoundary := time.Unix(0, (bucketStart.UnixNano()/int64(interval)+1)*int64(interval)).UTC()
	if !epochBoundary.After(bucketStart) || !epochBoundary.Before(bucketStart.Add(interval)) {
		t.Fatalf("test premise: epoch boundary %s not inside bucket [%s, %s)", epochBoundary, bucketStart, bucketStart.Add(interval))
	}

	before := bucketStart.Add(-time.Minute)
	inside := bucketStart.Add(time.Minute)
	afterEpochBoundary := epochBoundary.Add(time.Minute)

	// Crossing the truncate boundary: plan key and rotation offset both change.
	if scheduledPlanKey(instance, before, interval) == scheduledPlanKey(instance, inside, interval) {
		t.Fatalf("plan key did not change across the truncate boundary at %s", bucketStart)
	}
	if schedule.DerivedTargetRotationOffset(before, interval, 10) == schedule.DerivedTargetRotationOffset(inside, interval, 10) {
		t.Fatalf("rotation offset did not change across the truncate boundary at %s", bucketStart)
	}
	// Crossing only the epoch boundary inside one bucket: neither changes.
	if a, b := scheduledPlanKey(instance, inside, interval), scheduledPlanKey(instance, afterEpochBoundary, interval); a != b {
		t.Fatalf("plan key changed inside one bucket: %q -> %q", a, b)
	}
	if a, b := schedule.DerivedTargetRotationOffset(inside, interval, 10), schedule.DerivedTargetRotationOffset(afterEpochBoundary, interval, 10); a != b {
		t.Fatalf("rotation offset changed inside one plan-key bucket at the epoch boundary %s: %d -> %d", epochBoundary, a, b)
	}
}

// A bootstrap package-registry instance plans under the fixed "bootstrap" key,
// so its derived-target rotation must stay on the global reconcile cadence even
// when scan_interval is set; otherwise one page of targets would be held for
// the whole override while the bootstrap run is non-terminal (Codex review on
// #6724).
func TestServiceRunReconcileBootstrapRotationIgnoresScanInterval(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, time.June, 1, 17, 7, 0, 0, time.UTC)
	current := first
	instance := testServicePackageRegistryInstance(first)
	instance.Bootstrap = true
	instance.Configuration = `{"scan_interval": "1h", "derive_from_owned_packages": {"enabled": true, "ecosystems": ["npm"], "target_limit": 10}}`
	planner := &fakePackageRegistryPlanner{
		run: workflow.Run{
			RunID:              "package_registry:collector-package-registry:schedule:bootstrap",
			TriggerKind:        workflow.TriggerKindSchedule,
			Status:             workflow.RunStatusCollectionPending,
			RequestedScopeSet:  "{}",
			RequestedCollector: string(scope.CollectorPackageRegistry),
			CreatedAt:          first,
			UpdatedAt:          first,
		},
	}
	targetReader := &rotatingOwnedPackageTargetReader{}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        5 * time.Minute,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: scope.CollectorPackageRegistry,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				Bootstrap:     true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                    &fakeStore{instances: []workflow.CollectorInstance{instance}},
		PackageRegistryPlanner:   planner,
		OwnedPackageTargetReader: targetReader,
		Clock:                    func() time.Time { return current },
	}

	for tick := 0; tick < 2; tick++ {
		current = first.Add(time.Duration(tick) * 5 * time.Minute)
		if err := service.runReconcile(context.Background()); err != nil {
			t.Fatalf("tick %d runReconcile() error = %v, want nil", tick, err)
		}
	}
	if len(targetReader.requests) != 2 || len(planner.requests) != 2 {
		t.Fatalf("requests = reader %d, planner %d; want 2 each", len(targetReader.requests), len(planner.requests))
	}
	for tick := 0; tick < 2; tick++ {
		if got, want := planner.requests[tick].PlanKey, "bootstrap"; got != want {
			t.Fatalf("tick %d plan key = %q, want %q", tick, got, want)
		}
		want := schedule.DerivedTargetRotationOffset(first.Add(time.Duration(tick)*5*time.Minute), 5*time.Minute, 10)
		if got := targetReader.requests[tick].RotationOffset; got != want {
			t.Fatalf("tick %d rotation offset = %d, want %d (global 5m cadence, not the 1h override)", tick, got, want)
		}
	}
}
