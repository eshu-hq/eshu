// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// factRegistrySelectionAsOf is the fixed status clock for the #10973
// fact/registry route-selection proofs.
var factRegistrySelectionAsOf = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// collectorEvidenceWithoutRegistry is the snapshot selection for status
// routes that render collector readbacks but never render the registry
// collector snapshots: every operator route except freshness-causality.
// Dropping the registry read removes the ~0.9s warm-replica fact_records
// scan (registryMetadataTargetStatusQuery over ~89M rows) from each snapshot
// without changing a rendered byte (#10973).
var collectorEvidenceWithoutRegistry = statuspkg.SnapshotSelection{
	IncludeCollectorFactEvidence: true,
	SkipTerraformStateEvidence:   true,
}

// neitherFactNorRegistry is the snapshot selection for GET
// /api/v0/status/freshness-causality, which renders neither collector fact
// evidence nor registry snapshots (#10973).
var neitherFactNorRegistry = statuspkg.SnapshotSelection{
	SkipTerraformStateEvidence: true,
}

// factRegistryStatusRoute pairs one status route with the snapshot selection
// it must request.
type factRegistryStatusRoute struct {
	path string
	want statuspkg.SnapshotSelection
}

// factRegistryStatusRoutes lists every status route served through
// operatorStatusSelection (plus freshness-causality) with the selection each
// must request after the #10973 trim.
var factRegistryStatusRoutes = []factRegistryStatusRoute{
	{"/api/v0/status/operations", collectorEvidenceWithoutRegistry},
	{"/api/v0/status/hosted-readiness", collectorEvidenceWithoutRegistry},
	{"/api/v0/status/operator-control-plane", collectorEvidenceWithoutRegistry},
	{"/api/v0/status/freshness-causality", neitherFactNorRegistry},
	{"/api/v0/status/collectors", collectorEvidenceWithoutRegistry},
	{"/api/v0/collectors", collectorEvidenceWithoutRegistry},
	{"/api/v0/status/collector-readiness", collectorEvidenceWithoutRegistry},
	{"/api/v0/collector-readiness", collectorEvidenceWithoutRegistry},
	{"/api/v0/status/governance", collectorEvidenceWithoutRegistry},
	{"/api/v0/status/answer-narration", collectorEvidenceWithoutRegistry},
}

// factRegistrySelectionSnapshot is a snapshot with populated collector fact
// evidence AND registry snapshots, so the byte-identity probe below is
// non-vacuous: a route that rendered either section would differ when the
// reader omits it.
func factRegistrySelectionSnapshot() statuspkg.RawSnapshot {
	raw := terraformSelectionSnapshot()
	raw.AsOf = factRegistrySelectionAsOf
	raw.Coordinator = &statuspkg.CoordinatorSnapshot{
		CollectorInstances: []collector.InstanceSummary{
			{InstanceID: "git-1", CollectorKind: "git", Mode: "scheduled", Enabled: true, LastObservedAt: factRegistrySelectionAsOf},
		},
	}
	raw.CollectorFactEvidence = []collector.FactEvidence{
		{
			InstanceID: "git-1", CollectorKind: "git", EvidenceSource: "file",
			SourceSystems: []string{"git"}, ObservationCount: 8,
			LastObservedAt: factRegistrySelectionAsOf, UpdatedAt: factRegistrySelectionAsOf,
		},
	}
	raw.RegistryCollectors = []statuspkg.RegistryCollectorSnapshot{
		{CollectorKind: "package_registry", ConfiguredInstances: 1, ActiveScopes: 2},
	}
	return raw
}

// TestFactRegistryStatusRoutesSkipUnrenderedSections proves each operator
// status route requests only the fact_records-backed sections it renders,
// and that its response bytes are identical whether the reader returns the
// unrendered sections or omits them.
func TestFactRegistryStatusRoutesSkipUnrenderedSections(t *testing.T) {
	t.Parallel()
	for _, route := range factRegistryStatusRoutes {
		t.Run(route.path, func(t *testing.T) {
			t.Parallel()
			baseline := &selectionRecordingReader{snapshot: factRegistrySelectionSnapshot()}
			selected := &selectionRecordingReader{
				snapshot:            factRegistrySelectionSnapshot(),
				omitTerraformOnSkip: true,
				omitFactOnSkip:      true,
				omitRegistryOnSkip:  true,
			}
			if baseline.returnedFactRows != 0 || baseline.returnedRegistryRows != 0 {
				t.Fatalf("baseline reader must start unread, got fact=%d registry=%d",
					baseline.returnedFactRows, baseline.returnedRegistryRows)
			}
			want := serveStatusRoute(t, baseline, route.path).Body.Bytes()
			got := serveStatusRoute(t, selected, route.path).Body.Bytes()
			if selected.filteredCallCount != 1 || selected.lastSelection != route.want {
				t.Fatalf("selection = %+v (calls %d), want %+v (calls 1)",
					selected.lastSelection, selected.filteredCallCount, route.want)
			}
			if baseline.returnedFactRows != 1 || baseline.returnedRegistryRows != 1 {
				t.Fatalf("fixture fact/registry rows = %d/%d, want 1/1 (probe would be vacuous)",
					baseline.returnedFactRows, baseline.returnedRegistryRows)
			}
			if route.want.IncludeCollectorFactEvidence && selected.returnedFactRows != 1 {
				t.Fatalf("selected reader must keep fact evidence, got %d rows", selected.returnedFactRows)
			}
			if !route.want.IncludeCollectorFactEvidence && selected.returnedFactRows != 0 {
				t.Fatalf("selected reader must omit fact evidence, got %d rows", selected.returnedFactRows)
			}
			if selected.returnedRegistryRows != 0 {
				t.Fatalf("selected reader must omit registry snapshots, got %d rows", selected.returnedRegistryRows)
			}
			if !statusBodiesEqual(t, route.path, got, want) {
				t.Fatalf("response changed when unrendered sections were omitted:\nfull=%s\nomitted=%s", want, got)
			}
		})
	}
}

// TestFactEvidenceOmissionChangesOperationsResponse proves the identity probe
// above can see a lost rendered section: omitting collector fact evidence
// (which operations renders as collectors) must change the operations body.
func TestFactEvidenceOmissionChangesOperationsResponse(t *testing.T) {
	t.Parallel()
	const path = "/api/v0/status/operations"
	full := serveStatusRoute(t,
		&selectionRecordingReader{snapshot: factRegistrySelectionSnapshot()}, path).Body.Bytes()
	omitted := factRegistrySelectionSnapshot()
	omitted.CollectorFactEvidence = nil
	changed := serveStatusRoute(t,
		&selectionRecordingReader{snapshot: omitted}, path).Body.Bytes()
	if statusBodiesEqual(t, path, full, changed) {
		t.Fatal("byte-equality probe cannot see a lost collectors section on operations")
	}
}
