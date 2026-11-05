// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestGoModulesDerivationDefaultsToADailyCadence is the regression for an
// activation having no top-level scan_interval: the instance's interval is the
// global reconcile interval, 30s by default, which would rotate a new page every
// tick and rescan repositories almost continuously. With nothing set the
// derivation buckets on 24h.
func TestGoModulesDerivationDefaultsToADailyCadence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(1)}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	service.Config.ReconcileInterval = 30 * time.Second
	instance := goModulesInstance(now, goModulesActivationConfiguration(
		`{"derive_from_go_modules":{"enabled":true}}`, "scanner_worker", "scanner_worker"))
	for _, offset := range []time.Duration{0, 30 * time.Second, 11 * time.Hour} {
		if err := scheduleGoModules(t, service, now.Add(offset), instance); err != nil {
			t.Fatal(err)
		}
	}
	if err := scheduleGoModules(t, service, now.Add(24*time.Hour), instance); err != nil {
		t.Fatal(err)
	}
	first := reader.filters[0]
	for i, filter := range reader.filters[1:3] {
		if filter.RunID != first.RunID || filter.RotationBucket != first.RotationBucket {
			t.Fatalf("pass %d left the 24h bucket: %+v vs %+v", i+1, filter, first)
		}
	}
	if next := reader.filters[3]; next.RunID == first.RunID || next.RotationBucket != first.RotationBucket+1 {
		t.Fatalf("next day = %+v, want the next bucket after %+v", next, first)
	}
}

// TestGoModulesStartupRefusesAnUnusableInterval proves a cadence the reconcile
// ticker cannot honor fails at startup. A bucket narrower than the tick would skip
// rotation slots and starve repositories.
func TestGoModulesStartupRefusesAnUnusableInterval(t *testing.T) {
	t.Parallel()

	for name, interval := range map[string]string{
		"not a duration":   "soon",
		"zero":             "0s",
		"negative":         "-1h",
		"below the ticker": "10s",
		"not a multiple":   "45s",
	} {
		block := `{"derive_from_go_modules":{"enabled":true,"scan_interval":"` + interval + `"}}`
		instance := workflow.DesiredCollectorInstance{
			InstanceID: testGoInstanceID, CollectorKind: scope.CollectorScannerWorker, Mode: workflow.CollectorModeScheduled,
			Enabled: true, ClaimsEnabled: true,
			Configuration: goModulesActivationConfiguration(block, "scanner_worker", "scanner_worker"),
		}
		err := validateActivationDerivationSchedule(instance.CollectorKind, instance.Configuration, 30*time.Second)
		if err == nil {
			t.Errorf("%s: validate = nil, want refusal", name)
		}
	}
	good := goModulesActivationConfiguration(
		`{"derive_from_go_modules":{"enabled":true,"scan_interval":"6h"}}`, "scanner_worker", "scanner_worker")
	if err := validateActivationDerivationSchedule(scope.CollectorScannerWorker, good, 30*time.Second); err != nil {
		t.Fatalf("6h: %v", err)
	}
	disabled := goModulesActivationConfiguration(
		`{"derive_from_go_modules":{"enabled":false,"scan_interval":"10s"}}`, "scanner_worker", "scanner_worker")
	if err := validateActivationDerivationSchedule(scope.CollectorScannerWorker, disabled, 30*time.Second); err == nil {
		t.Fatal("a disabled section with a bad interval = nil, want refusal, like a disabled in-tree block")
	}
}

// TestGoModulesDerivationBootstrapInstanceStillRotates proves the fixed bootstrap
// plan key does not pin an instance to the first page of the fleet.
func TestGoModulesDerivationBootstrapInstanceStillRotates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(1)}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))
	instance.Bootstrap = true
	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	if err := scheduleGoModules(t, service, later, instance); err != nil {
		t.Fatal(err)
	}
	if reader.filters[0].RunID == reader.filters[1].RunID {
		t.Fatalf("bootstrap run id %q did not change across buckets", reader.filters[0].RunID)
	}
}

// TestDesiredInstancesCarryOnlyTheGoReachabilityConfigBlock proves the readback
// keeps the block for this component alone: the three published scanners' stored
// configuration is unchanged.
func TestDesiredInstancesCarryOnlyTheGoReachabilityConfigBlock(t *testing.T) {
	t.Parallel()

	path := writeActivationFile(t, `host:
  sourceSystem: scanner_worker
  scope:
    id: go-reachability://fleet
    kind: scanner_worker
config:
  derive_from_go_modules:
    enabled: true
`)
	for _, tc := range []struct {
		id    string
		carry bool
	}{
		{scannercomponent.IDGoReachability, true},
		{scannercomponent.IDImage, false},
	} {
		manifest := testActivationManifest(string(scope.CollectorScannerWorker))
		manifest.Metadata.ID = tc.id
		instances, _, err := desiredInstancesForComponent(testActivationReadbackEntry(path), manifest)
		if err != nil {
			t.Fatalf("%s: error = %v", tc.id, err)
		}
		if got := strings.Contains(instances[0].Configuration, `"derive_from_go_modules"`); got != tc.carry {
			t.Fatalf("%s: configuration = %s, want carried %v", tc.id, instances[0].Configuration, tc.carry)
		}
	}
}

// TestGoModulesStartupRefusesABadBlock proves a static or registry activation
// fails at load, before any durable write, as the vulnerability block does.
func TestGoModulesStartupRefusesABadBlock(t *testing.T) {
	t.Parallel()

	for name, configuration := range map[string]string{
		"host":  goModulesActivationConfiguration(testGoModulesBlock, "git", "scanner_worker"),
		"shape": goModulesActivationConfiguration(`{"derive_from_go_modules":true}`, "scanner_worker", "scanner_worker"),
	} {
		if err := checkActivationDocumentDerivation(scope.CollectorScannerWorker, configuration); err == nil {
			t.Errorf("%s: check = nil, want refusal", name)
		}
	}
	good := goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker")
	if err := checkActivationDocumentDerivation(scope.CollectorScannerWorker, good); err != nil {
		t.Fatalf("good block: %v", err)
	}
}
