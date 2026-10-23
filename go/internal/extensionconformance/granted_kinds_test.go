// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	grantedComponentID = "dev.example.collector.incidents"
	grantedKind        = "incident.record"
	grantedScope       = "incidents"
)

// grantedKindManifest declares one core-owned fact kind. The kind carries no
// payloadSchemaRef so the test isolates the grant decision from payload
// validation.
const grantedKindManifest = `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.example.collector.incidents
  name: Incidents collector
  publisher: example
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - incidents
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/example/incidents@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: incident.record
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
  telemetry:
    metricsPrefix: eshu_dp_example_incidents_
`

func writeGrantedKindManifest(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(grantedKindManifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// grantedKindResult is a valid complete result whose single fact is the
// core-owned incident.record kind, claimed by the incidents collector.
func grantedKindResult() sdkcollector.Result {
	result := validConformanceResult()
	result.Claim.ComponentID = grantedComponentID
	result.Claim.InstanceID = "incidents-primary"
	result.Claim.CollectorKind = "incidents"
	result.Claim.SourceSystem = grantedComponentID
	result.Claim.Scope = sdkcollector.Scope{ID: "component:incidents-primary", Kind: "component"}
	result.Claim.ConfigHandle = "component-config:incidents"
	fact := result.Facts[0]
	fact.Kind = grantedKind
	fact.StableKey = "incidents:record:1"
	fact.SourceRef.SourceSystem = grantedComponentID
	fact.SourceRef.ScopeID = "component:incidents-primary"
	fact.SourceRef.FactKey = "incidents:record:1"
	fact.SourceRef.URI = "component://incidents/record/1"
	fact.SourceRef.RecordID = "record-1"
	result.Facts = []sdkcollector.Fact{fact}
	return result
}

// liveGrant returns a grant that covers the manifest in
// grantedKindManifest; each test case mutates one field to break it.
func liveGrant() component.ProducerGrant {
	return component.ProducerGrant{
		ProducerID:     grantedComponentID,
		Version:        "0.1.0",
		Kind:           grantedKind,
		SchemaVersions: []string{"1.0.0"},
		Scope:          grantedScope,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
	}
}

func recordGrants(t *testing.T, home string, grants ...component.ProducerGrant) {
	t.Helper()

	registry := component.NewRegistry(home)
	for _, grant := range grants {
		if err := registry.RecordGrant(grant); err != nil {
			t.Fatalf("RecordGrant() error = %v, want nil", err)
		}
	}
}

// TestRunHonorsProducerGrantsInComponentHome is the issue #8909 acceptance
// proof: a manifest that declares a granted core-owned fact kind passes
// conformance when the component home registry carries the matching grant.
func TestRunHonorsProducerGrantsInComponentHome(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	recordGrants(t, home, liveGrant())

	report, err := Run(context.Background(), Request{
		ManifestPath:  writeGrantedKindManifest(t),
		FixturePaths:  []string{writeConformanceFixture(t, grantedKindResult())},
		Mode:          ModeFixture,
		ComponentHome: home,
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil; findings=%#v", err, report.Findings)
	}
	if got, want := report.Status, StatusPassed; got != want {
		t.Fatalf("Status = %q, want %q; findings=%#v", got, want, report.Findings)
	}
	if got, want := report.Summary.FactCount, 1; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
}

// TestRunFailsClosedWithoutMatchingGrant proves a core-owned kind still fails
// closed with the existing manifest error unless a live grant names this
// producer, version, kind, schema version, and declared collector scope.
func TestRunFailsClosedWithoutMatchingGrant(t *testing.T) {
	t.Parallel()

	expired := liveGrant()
	expired.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	revoked := liveGrant()
	revoked.Revoked = true
	otherScope := liveGrant()
	otherScope.Scope = "other"
	otherKind := liveGrant()
	otherKind.Kind = "incident.lifecycle_event"
	otherVersion := liveGrant()
	otherVersion.Version = "0.2.0"
	otherSchema := liveGrant()
	otherSchema.SchemaVersions = []string{"2.0.0"}
	otherProducer := liveGrant()
	otherProducer.ProducerID = "dev.example.collector.someone-else"

	tests := []struct {
		name   string
		grants []component.ProducerGrant
		home   func(t *testing.T) string
	}{
		{name: "no grants recorded", home: func(t *testing.T) string { return t.TempDir() }},
		{name: "no component home", home: func(t *testing.T) string { return "" }},
		{name: "expired grant", grants: []component.ProducerGrant{expired}},
		{name: "revoked grant", grants: []component.ProducerGrant{revoked}},
		{name: "grant for a different scope", grants: []component.ProducerGrant{otherScope}},
		{name: "grant for a different kind", grants: []component.ProducerGrant{otherKind}},
		{name: "grant for a different version", grants: []component.ProducerGrant{otherVersion}},
		{name: "grant for a different schema version", grants: []component.ProducerGrant{otherSchema}},
		{name: "grant for a different producer", grants: []component.ProducerGrant{otherProducer}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if tt.home != nil {
				home = tt.home(t)
			}
			if home != "" {
				recordGrants(t, home, tt.grants...)
			}
			report, err := Run(context.Background(), Request{
				ManifestPath:  writeGrantedKindManifest(t),
				FixturePaths:  []string{writeConformanceFixture(t, grantedKindResult())},
				Mode:          ModeFixture,
				ComponentHome: home,
			})
			if err == nil {
				t.Fatal("Run() error = nil, want a fail-closed manifest error")
			}
			assertFinding(t, report, FindingManifestInvalid, true, true)
			if !strings.Contains(report.Findings[0].Message, "is core-owned by Eshu and cannot be claimed by optional components") {
				t.Fatalf("finding message = %q, want the existing core-owned error", report.Findings[0].Message)
			}
		})
	}
}

// TestRunNamespacedManifestIgnoresUnreadableRegistry proves a manifest that
// needs no grant behaves exactly as before: the registry is never consulted,
// so an unreadable one cannot change the verdict.
func TestRunNamespacedManifestIgnoresUnreadableRegistry(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "registry.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt registry: %v", err)
	}
	report, err := Run(context.Background(), Request{
		ManifestPath:  writeConformanceManifest(t, "source_evidence_only:no_graph_truth"),
		FixturePaths:  []string{writeConformanceFixture(t, validConformanceResult())},
		Mode:          ModeFixture,
		ComponentHome: home,
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil; findings=%#v", err, report.Findings)
	}
	if got, want := report.Status, StatusPassed; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
}
