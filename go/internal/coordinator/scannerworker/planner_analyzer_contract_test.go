// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesImageAnalyzerContracts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 4, 14, 7, 12, 0, time.FixedZone("test-offset", -7*60*60))
	tests := []struct {
		name                  string
		instanceID            string
		analyzer              string
		configuration         string
		configuredScopes      []string
		wantRequestedScopeSet string
	}{
		{
			name:       "image unpacking selects image targets",
			instanceID: "scanner-worker-image",
			analyzer:   "image_unpacking",
			configuration: `{
				"analyzer":"image_unpacking",
				"sbom_targets":[{"scope_id":"scanner-worker://repository/ignored","root_path":"/private/ignored-repository"}],
				"image_targets":[
					{"scope_id":"scanner-worker://image/zeta","rootfs_path":" /private/image-zeta "},
					{"scope_id":"scanner-worker://image/alpha","layer_paths":[" /private/layer-alpha "," "]}
				],
				"os_package_targets":[{"scope_id":"scanner-worker://image/ignored-os","rootfs_path":"/private/ignored-os"}]
			}`,
			configuredScopes: []string{
				"scanner-worker://image/zeta",
				"scanner-worker://image/alpha",
			},
			wantRequestedScopeSet: `{"collector_instance_id":"scanner-worker-image","analyzer":"image_unpacking","targets":[{"scope_id":"scanner-worker://image/alpha","target_kind":"image"},{"scope_id":"scanner-worker://image/zeta","target_kind":"image"}]}`,
		},
		{
			name:       "os package extraction selects os package targets",
			instanceID: "scanner-worker-os-packages",
			analyzer:   "os_package_extraction",
			configuration: `{
				"analyzer":"os_package_extraction",
				"sbom_targets":[{"scope_id":"scanner-worker://repository/ignored","root_path":"/private/ignored-repository"}],
				"image_targets":[{"scope_id":"scanner-worker://image/ignored-image","rootfs_path":"/private/ignored-image"}],
				"os_package_targets":[
					{"scope_id":"scanner-worker://image/zeta","rootfs_path":" /private/rootfs-zeta "},
					{"scope_id":"scanner-worker://image/alpha","rootfs_path":" /private/rootfs-alpha "}
				]
			}`,
			configuredScopes: []string{
				"scanner-worker://image/zeta",
				"scanner-worker://image/alpha",
			},
			wantRequestedScopeSet: `{"collector_instance_id":"scanner-worker-os-packages","analyzer":"os_package_extraction","targets":[{"scope_id":"scanner-worker://image/alpha","target_kind":"image"},{"scope_id":"scanner-worker://image/zeta","target_kind":"image"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			instance := scannerWorkerContractInstance(tt.instanceID, tt.configuration, observedAt)
			run, items, err := WorkPlanner{}.PlanScannerWorkerWork(context.Background(), PlanRequest{
				Instance:   instance,
				ObservedAt: observedAt,
				PlanKey:    "continuous-20260604T210000Z",
			})
			if err != nil {
				t.Fatalf("PlanScannerWorkerWork() error = %v, want nil", err)
			}

			wantRunID := "scanner_worker:" + tt.instanceID + ":schedule:continuous-20260604T210000Z"
			if got := run.RunID; got != wantRunID {
				t.Fatalf("RunID = %q, want %q", got, wantRunID)
			}
			if got := run.RequestedScopeSet; got != tt.wantRequestedScopeSet {
				t.Fatalf("RequestedScopeSet = %q, want %q", got, tt.wantRequestedScopeSet)
			}
			if strings.Contains(run.RequestedScopeSet, "/private/") || strings.Contains(run.RequestedScopeSet, "ignored") {
				t.Fatalf("RequestedScopeSet leaked an unselected target or runtime-local path: %s", run.RequestedScopeSet)
			}
			assertScannerWorkerAnalyzerItems(t, items, tt.instanceID, tt.analyzer, wantRunID, tt.configuredScopes, observedAt.UTC())
		})
	}
}

func TestWorkPlannerRejectsMissingImageAnalyzerPaths(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 4, 21, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		configuration string
		wantErr       string
	}{
		{
			name:          "image unpacking requires rootfs or layers",
			configuration: `{"analyzer":"image_unpacking","image_targets":[{"scope_id":"scanner-worker://image/missing"}]}`,
			wantErr:       "scanner-worker image_unpacking target rootfs_path or layer_paths is required",
		},
		{
			name:          "os package extraction requires rootfs",
			configuration: `{"analyzer":"os_package_extraction","os_package_targets":[{"scope_id":"scanner-worker://image/missing"}]}`,
			wantErr:       "scanner-worker os_package_extraction target rootfs_path is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			instance := scannerWorkerContractInstance("scanner-worker-invalid", tt.configuration, observedAt)
			_, _, err := WorkPlanner{}.PlanScannerWorkerWork(context.Background(), PlanRequest{
				Instance:   instance,
				ObservedAt: observedAt,
				PlanKey:    "continuous-20260604T210000Z",
			})
			if err == nil {
				t.Fatalf("PlanScannerWorkerWork() error = nil, want substring %q", tt.wantErr)
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Fatalf("PlanScannerWorkerWork() error = %q, want substring %q", got, tt.wantErr)
			}
		})
	}
}

func scannerWorkerContractInstance(instanceID, configuration string, observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     instanceID,
		CollectorKind:  scope.CollectorScannerWorker,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func assertScannerWorkerAnalyzerItems(
	t *testing.T,
	items []workflow.WorkItem,
	instanceID string,
	analyzer string,
	wantRunID string,
	configuredScopes []string,
	wantTimestamp time.Time,
) {
	t.Helper()

	if got, want := len(items), len(configuredScopes); got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	for index, scopeID := range configuredScopes {
		generationID := "scanner_worker:" + facts.StableID("ScannerWorkerWorkflowGeneration", map[string]any{
			"analyzer":    analyzer,
			"instance_id": instanceID,
			"plan_key":    "continuous-20260604T210000Z",
			"scope_id":    scopeID,
		})
		item := items[index]
		if got, want := item.WorkItemID, "scanner_worker:"+instanceID+":"+generationID; got != want {
			t.Errorf("items[%d].WorkItemID = %q, want %q", index, got, want)
		}
		if got := item.RunID; got != wantRunID {
			t.Errorf("items[%d].RunID = %q, want %q", index, got, wantRunID)
		}
		if got, want := item.CollectorKind, scope.CollectorScannerWorker; got != want {
			t.Errorf("items[%d].CollectorKind = %q, want %q", index, got, want)
		}
		if got := item.CollectorInstanceID; got != instanceID {
			t.Errorf("items[%d].CollectorInstanceID = %q, want %q", index, got, instanceID)
		}
		if got, want := item.SourceSystem, string(scope.CollectorScannerWorker); got != want {
			t.Errorf("items[%d].SourceSystem = %q, want %q", index, got, want)
		}
		if got := item.ScopeID; got != scopeID {
			t.Errorf("items[%d].ScopeID = %q, want configured-order scope %q", index, got, scopeID)
		}
		if got := item.AcceptanceUnitID; got != scopeID {
			t.Errorf("items[%d].AcceptanceUnitID = %q, want %q", index, got, scopeID)
		}
		if got := item.GenerationID; got != generationID {
			t.Errorf("items[%d].GenerationID = %q, want %q", index, got, generationID)
		}
		if got := item.SourceRunID; got != generationID {
			t.Errorf("items[%d].SourceRunID = %q, want %q", index, got, generationID)
		}
		if got, want := item.FairnessKey, "scanner_worker:"+instanceID+":image"; got != want {
			t.Errorf("items[%d].FairnessKey = %q, want %q", index, got, want)
		}
		if got, want := item.Status, workflow.WorkItemStatusPending; got != want {
			t.Errorf("items[%d].Status = %q, want %q", index, got, want)
		}
		if !item.CreatedAt.Equal(wantTimestamp) || item.CreatedAt.Location() != time.UTC {
			t.Errorf("items[%d].CreatedAt = %v (%v), want UTC %v", index, item.CreatedAt, item.CreatedAt.Location(), wantTimestamp)
		}
		if !item.UpdatedAt.Equal(wantTimestamp) || item.UpdatedAt.Location() != time.UTC {
			t.Errorf("items[%d].UpdatedAt = %v (%v), want UTC %v", index, item.UpdatedAt, item.UpdatedAt.Location(), wantTimestamp)
		}
	}
}
