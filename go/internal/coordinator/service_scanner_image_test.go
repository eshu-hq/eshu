// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"strings"
	"testing"
	"time"

	scannerworker "github.com/eshu-hq/eshu/go/internal/coordinator/scanner/worker"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type scannerDiscoveryStoreFake struct {
	requests   []workflow.RunningImageDiscoveryRequest
	page       workflow.RunningImageDiscoveryPage
	run        workflow.Run
	items      []workflow.WorkItem
	targets    []workflow.ScannerImageTarget
	expected   workflow.RunningImageDiscoveryCursor
	next       workflow.RunningImageDiscoveryCursor
	admissions int
}

func (f *scannerDiscoveryStoreFake) ListAuthorizedRunningImageObservationsPage(
	_ context.Context, request workflow.RunningImageDiscoveryRequest,
) (workflow.RunningImageDiscoveryPage, error) {
	f.requests = append(f.requests, request)
	return f.page, nil
}

func (f *scannerDiscoveryStoreFake) CreateScannerRunWithTargetsAndCursor(
	_ context.Context, run workflow.Run, items []workflow.WorkItem, targets []workflow.ScannerImageTarget,
	_ workflow.RunningImageDiscoveryRequest, expected, next workflow.RunningImageDiscoveryCursor,
) (workflow.RunAdmission, error) {
	f.admissions++
	f.run, f.items, f.targets = run, items, targets
	f.expected, f.next = expected, next
	return workflow.RunAdmission{InsertedWorkItems: len(items)}, nil
}

func TestScheduleDerivedScannerImageWorkUsesBoundedCanonicalPageAndCursor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	instance := workflow.CollectorInstance{
		InstanceID: "scanner-a", CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
		Configuration:  `{"analyzer":"image_unpacking","derive_from_running_images":true}`,
		LastObservedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	store := &scannerDiscoveryStoreFake{page: workflow.RunningImageDiscoveryPage{
		Candidates: []workflow.RunningImageObservationCandidate{{
			ObservationID: 1, ProducerInstanceID: "source-a", SourceScopeID: "kubernetes://cluster-a",
			SourceGenerationID: "generation-a", Registry: "registry.example.invalid", Repository: "private/a",
			RuntimeImageReference: "registry.example.invalid/private/a:one", RuntimeDigest: digest,
			RuntimeDigestKind: workflow.RunningImageDigestManifest, ManifestDigest: digest,
			AuthorizationDomain: "domain-a",
		}},
		ExpectedCursor: workflow.RunningImageDiscoveryCursor{Version: 2, CycleUpperObservationID: 5},
		NextCursor:     workflow.RunningImageDiscoveryCursor{Version: 3, CycleUpperObservationID: 5, AfterObservationID: 1, AfterAuthorizationDomain: "domain-a"},
	}}
	s := Service{
		Config: Config{ScannerImageDiscovery: ScannerImageDiscoveryPolicy{
			Mode:     ScannerImageDiscoveryLocal,
			Revision: 7, SourceScopeIDs: []string{"kubernetes://cluster-a"}, Explicit: true,
		}},
		ScannerImageDiscoveryStore: store,
		ScannerImagePlanner:        scannerworker.WorkPlanner{},
		ControlRolePolicyRevision:  4,
		ControlDatabaseIdentity:    "00c9c993-20fb-4f11-9acd-44202a90948a",
	}
	if err := s.scheduleDerivedScannerImageWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 1 || store.requests[0].Limit != 256 ||
		store.requests[0].ExpectedRolePolicyRevision != 4 || store.admissions != 1 ||
		store.expected.Version != 2 || store.next.Version != 3 || len(store.items) != 1 {
		t.Fatalf("discovery request/admission = %+v / %d / %+v / %+v / %d items", store.requests, store.admissions, store.expected, store.next, len(store.items))
	}
	if store.targets[0].Sources[0].SourceObservationID != 1 || strings.Contains(store.run.RequestedScopeSet, "private/") {
		t.Fatal("canonical source or run privacy lost")
	}
}
