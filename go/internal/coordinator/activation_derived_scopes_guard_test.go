// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const testVulnerabilityKind = string(scope.CollectorVulnerabilityIntelligence)

type recordedCounterAdd struct {
	value      int64
	attributes map[string]string
}

type fakeActivationDerivedScopeCounter struct {
	adds []recordedCounterAdd
}

func (f *fakeActivationDerivedScopeCounter) Add(_ context.Context, incr int64, options ...metric.AddOption) {
	set := metric.NewAddConfig(options).Attributes()
	attrs := map[string]string{}
	for _, kv := range set.ToSlice() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	f.adds = append(f.adds, recordedCounterAdd{value: incr, attributes: attrs})
}

func vulnerabilityActivationWithDerive(now time.Time, block string) workflow.CollectorInstance {
	return testVulnerabilityActivationInstance(now,
		testVulnerabilityActivationConfiguration(block, testVulnerabilityKind, testVulnerabilityKind))
}

func ownedViteReader() *fakeOwnedPackageTargetReader {
	return &fakeOwnedPackageTargetReader{targets: []workflow.OwnedPackageDependencyTarget{
		{Ecosystem: "npm", PackageName: "vite", Version: "5.4.21", Lockfile: true, RepositoryID: "repo-eshu"},
		{Ecosystem: "maven", PackageName: "org.apache.logging.log4j:log4j-core", Version: "2.17.1", RepositoryID: "repo-eshu"},
	}}
}

// TestActivationDerivedScopesHonorEgressDeny proves a denied activation gets
// no derived scopes and no owned-package read: the hook runs after the egress
// decision in the same loop iteration.
func TestActivationDerivedScopesHonorEgressDeny(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := vulnerabilityActivationWithDerive(now, `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reader := ownedViteReader()
	service := testVulnerabilityActivationService(t, now, instance, store, reader, "")

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0 under egress deny", got)
	}
	if got := len(reader.requests); got != 0 {
		t.Fatalf("owned target reads = %d, want 0 under egress deny", got)
	}
}

// TestActivationDerivedScopesRefuseHostScopeKindMismatch proves the guard
// refuses an activation whose host scope kind or source system is not
// vulnerability_intelligence, logs both values with the instance id, and reads
// nothing.
func TestActivationDerivedScopesRefuseHostScopeKindMismatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, source, kind string }{
		{"scope kind", testVulnerabilityKind, "repository"},
		{"source system", "osv", testVulnerabilityKind},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
			instance := testVulnerabilityActivationInstance(now, testVulnerabilityActivationConfiguration(
				`{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`, tc.source, tc.kind))
			store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
			reader := ownedViteReader()
			var logs bytes.Buffer
			service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

			// Service.Run refuses this host at Config.Validate now; the
			// hook's own refusal is the backstop for a stale durable row.
			err := service.scheduleComponentExtensionWork(context.Background(), now, []workflow.CollectorInstance{instance})
			if err == nil || !strings.Contains(err.Error(), "must both be \"vulnerability_intelligence\"") {
				t.Fatalf("scheduleComponentExtensionWork() error = %v, want host scope kind/source system refusal", err)
			}
			if runErr := runCancelledReconcile(t, service); runErr == nil ||
				!strings.Contains(runErr.Error(), "must both be \"vulnerability_intelligence\"") {
				t.Fatalf("Run() error = %v, want the startup refusal", runErr)
			}
			if got := len(reader.requests); got != 0 {
				t.Fatalf("owned target reads = %d, want 0 after refusal", got)
			}
			for _, want := range []string{
				`"collector_instance_id":"vuln-intel-activation"`,
				`"host_source_system":"` + tc.source + `"`,
				`"host_scope_kind":"` + tc.kind + `"`,
				`"level":"ERROR"`,
			} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("logs = %s, want %s", logs.String(), want)
				}
			}
		})
	}
}

// TestActivationDerivedScopesRotationAndLimit proves target_limit and the
// planning mode apply with the in-tree rotation contract: the read limit and
// rotation offset bucket on the activation's interval (the global reconcile
// interval), and the derived run's plan key follows the planning mode.
func TestActivationDerivedScopesRotationAndLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, block, wantPlanKey string
		wantBucket               int64
		wantSlotLimit            int
		wantLimit                int
	}{
		{
			name:          "rotating",
			block:         `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm","maven"],"target_limit":1000}}`,
			wantPlanKey:   "scheduled-20261027T220000Z",
			wantBucket:    schedule.DerivedTargetRotationBucket(now, time.Hour),
			wantSlotLimit: 1000,
			wantLimit:     1001,
		},
		{
			name:          "single pass with clamp",
			block:         `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"planning_mode":"single_pass","target_limit":5000}}`,
			wantPlanKey:   "scheduled-single-pass",
			wantBucket:    0,
			wantSlotLimit: 0,
			wantLimit:     5001,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instance := vulnerabilityActivationWithDerive(now, tc.block)
			store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
			reader := ownedViteReader()
			service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)

			if err := runCancelledReconcile(t, service); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if got, want := len(reader.requests), 1; got != want {
				t.Fatalf("owned target reads = %d, want %d", got, want)
			}
			if got := reader.requests[0].Limit; got != tc.wantLimit {
				t.Fatalf("read limit = %d, want %d", got, tc.wantLimit)
			}
			if got := reader.requests[0].RotationBucket; got != tc.wantBucket {
				t.Fatalf("rotation bucket = %d, want %d", got, tc.wantBucket)
			}
			if got := reader.requests[0].TargetLimit; got != tc.wantSlotLimit {
				t.Fatalf("slot target limit = %d, want %d", got, tc.wantSlotLimit)
			}
			wantRun := "vulnerability_intelligence:vuln-intel-activation:schedule:derived:" + tc.wantPlanKey
			found := false
			for _, run := range store.createdRuns {
				if run.RunID == wantRun {
					found = true
				}
				if strings.Contains(run.RequestedScopeSet, "component_config") || strings.Contains(run.RequestedScopeSet, "derive_from") {
					t.Fatalf("run %q RequestedScopeSet = %s, want no opaque config", run.RunID, run.RequestedScopeSet)
				}
			}
			if !found {
				t.Fatalf("created runs = %+v, want derived run %q", store.createdRuns, wantRun)
			}
		})
	}
}

// TestActivationDerivedScopesInstalledEvidence proves installed-evidence
// derivation runs through the shared installed-evidence helper for an
// activation, and records the per-ecosystem planned counter.
func TestActivationDerivedScopesInstalledEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := vulnerabilityActivationWithDerive(now,
		`{"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["alpine"],"target_limit":10}}`)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	osReader := &fakeOSPackageAdvisoryTargetReader{targets: []workflow.OSPackageAdvisoryTarget{{
		Ecosystem: "alpine", PackageName: "openssl", InstalledVersion: "3.1.4-r5", PackageManager: "apk",
		VendorAdvisorySource: "alpine", RepositoryClass: "vendor", FactID: "os-package-openssl", ScopeID: "image://sha256:4444",
	}}}
	ownedReader := ownedViteReader()
	counter := &fakeActivationDerivedScopeCounter{}
	service := testVulnerabilityActivationService(t, now, instance, store, ownedReader, `{"mode":"broad"}`)
	service.OSPackageAdvisoryTargetReader = osReader
	service.ActivationDerivedScopes = counter

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(ownedReader.requests); got != 0 {
		t.Fatalf("owned target reads = %d, want 0 with owned derivation off", got)
	}
	if got, want := len(osReader.requests), 1; got != want {
		t.Fatalf("OS package reads = %d, want %d", got, want)
	}
	if !hasEnqueuedScope(store, "vuln-intel://osv/alpine/openssl?version=3.1.4-r5") {
		t.Fatalf("enqueued items = %+v, want installed-evidence OSV scope", store.enqueuedItems)
	}
	if got, want := len(counter.adds), 1; got != want {
		t.Fatalf("counter adds = %d, want %d", got, want)
	}
	add := counter.adds[0]
	if add.value != 1 || add.attributes["ecosystem"] != "alpine" ||
		add.attributes["collector_instance_id"] != "vuln-intel-activation" ||
		add.attributes["collector_kind"] != testVulnerabilityKind {
		t.Fatalf("counter add = %+v, want 1 alpine scope for vuln-intel-activation", add)
	}
}

// TestActivationDerivedScopesNoReadForOtherKinds proves the hook reads
// nothing and plans nothing extra for an activation of another kind, even one
// whose stored configuration carried a derive-shaped block.
func TestActivationDerivedScopesNoReadForOtherKinds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := vulnerabilityActivationWithDerive(now, `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`)
	instance.CollectorKind = scope.CollectorKind("scorecard")
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reader := ownedViteReader()
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(reader.requests); got != 0 {
		t.Fatalf("owned target reads = %d, want 0 for a non-vulnerability activation", got)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d (host scope only)", got, want)
	}
}

// TestActivationDerivedScopesInvalidDeriveFailsWithInTreeText proves an
// invalid derive value fails the activation with the error text the in-tree
// path produces for the same value, and that unknown keys (including the
// satellite's own targets shape and derived_targets) are ignored as in-tree.
func TestActivationDerivedScopesInvalidDeriveFailsWithInTreeText(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	invalid := `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":6000}}`
	instance := vulnerabilityActivationWithDerive(now, invalid)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := testVulnerabilityActivationService(t, now, instance, store, ownedViteReader(), `{"mode":"broad"}`)
	err := runCancelledReconcile(t, service)
	const wantText = "derive_from_owned_packages.target_limit must be between 0 and 5000"
	if err == nil || !strings.Contains(err.Error(), wantText) {
		t.Fatalf("Run() error = %v, want %q", err, wantText)
	}
	// The in-tree decoder reads the same sections from an instance's
	// configuration; its error for the same value must carry the same text.
	if _, inTreeErr := vulnerability.DerivationFromConfig(invalid); inTreeErr == nil || !strings.Contains(inTreeErr.Error(), wantText) {
		t.Fatalf("in-tree error = %v, want the same %q", inTreeErr, wantText)
	}

	unknown := `{"collector_instance_id":"x","targets":[{"source":"osv","queries":[{"name":"qs","version":"6.5.2"}]}],` +
		`"derived_targets":{"enabled":true},"surprise":1,` +
		`"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"extra_key":true}}`
	instance = vulnerabilityActivationWithDerive(now, unknown)
	store = &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service = testVulnerabilityActivationService(t, now, instance, store, ownedViteReader(), `{"mode":"broad"}`)
	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() with unknown keys error = %v, want nil (parity: unknown keys ignored)", err)
	}
	if !hasEnqueuedScope(store, "vuln-intel://osv/npm/vite?version=5.4.21") {
		t.Fatalf("enqueued items = %+v, want derived scope despite unknown keys", store.enqueuedItems)
	}
}

func hasEnqueuedScope(store *fakeStore, scopeID string) bool {
	for _, item := range store.enqueuedItems {
		if item.ScopeID == scopeID {
			return true
		}
	}
	return false
}
