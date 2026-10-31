// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	scannerworker "github.com/eshu-hq/eshu/go/internal/coordinator/scanner/worker"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const scannerImageDiscoveryPageLimit = 256

// ScannerImageDiscoveryStore reads canonical observations and advances the
// durable cursor in the same transaction as authorized target admission.
type ScannerImageDiscoveryStore interface {
	ListAuthorizedRunningImageObservationsPage(context.Context, workflow.RunningImageDiscoveryRequest) (workflow.RunningImageDiscoveryPage, error)
	CreateScannerRunWithTargetsAndCursor(context.Context, workflow.Run, []workflow.WorkItem,
		[]workflow.ScannerImageTarget, workflow.RunningImageDiscoveryRequest,
		workflow.RunningImageDiscoveryCursor, workflow.RunningImageDiscoveryCursor) (workflow.RunAdmission, error)
}

// ScannerImagePlanner converts one discovery page into boundary-partitioned
// workflow rows; the store rechecks authority at admission. Skipped
// candidates return as per-reason evidence for the operator signal.
type ScannerImagePlanner interface {
	PlanDerivedImageWork(context.Context, scannerworker.DerivedImagePlanRequest) (
		workflow.Run, []workflow.WorkItem, []workflow.ScannerImageTarget,
		[]schedule.DerivedTargetSkipEvidence, error)
}

func (s Service) scheduleDerivedScannerImageWork(
	ctx context.Context, observedAt time.Time, instances []workflow.CollectorInstance,
) error {
	policy := s.Config.ScannerImageDiscovery
	if policy.Mode == "" || policy.Mode == ScannerImageDiscoveryDisabled {
		return nil
	}
	if s.ScannerImageDiscoveryStore == nil || s.ScannerImagePlanner == nil ||
		s.ControlRolePolicyRevision <= 0 || s.ControlDatabaseIdentity == "" {
		return fmt.Errorf("scanner image discovery requires store, planner, and role contract")
	}
	for _, instance := range instances {
		if !shouldScheduleScannerWorker(instance) {
			continue
		}
		var config struct {
			DeriveFromRunningImages bool `json:"derive_from_running_images"`
		}
		if err := json.Unmarshal([]byte(defaultScannerImageConfiguration(instance.Configuration)), &config); err != nil {
			return fmt.Errorf("scanner image discovery instance configuration: %w", err)
		}
		if !config.DeriveFromRunningImages {
			continue
		}
		request := s.scannerImageDiscoveryRequest(instance.InstanceID)
		page, err := s.ScannerImageDiscoveryStore.ListAuthorizedRunningImageObservationsPage(ctx, request)
		if err != nil {
			return fmt.Errorf("read scanner image discovery page for %q: %w", instance.InstanceID, err)
		}
		if len(page.Candidates) > request.Limit || page.NextCursor.Version != page.ExpectedCursor.Version+1 {
			return fmt.Errorf("scanner image discovery page violates bounded cursor contract")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scanner image interval for %q: %w", instance.InstanceID, err)
		}
		planKey := fmt.Sprintf("derived-%s-r%d-v%d", scheduledPlanKey(instance, observedAt, interval),
			policy.Revision, page.ExpectedCursor.Version)
		run, items, targets, skipped, err := s.ScannerImagePlanner.PlanDerivedImageWork(ctx,
			scannerworker.DerivedImagePlanRequest{
				Instance: instance, ObservedAt: observedAt, PlanKey: planKey,
				AuthorizationNamespace: request.AuthorizationNamespace,
				TenantID:               request.TenantID, WorkspaceID: request.WorkspaceID,
				SubjectClass: request.SubjectClass, PolicyRevisionHash: request.PolicyRevisionHash,
				Candidates: page.Candidates,
			})
		if err != nil {
			return fmt.Errorf("plan scanner image page for %q: %w", instance.InstanceID, err)
		}
		admission, err := s.ScannerImageDiscoveryStore.CreateScannerRunWithTargetsAndCursor(ctx,
			run, items, targets, request, page.ExpectedCursor, page.NextCursor)
		if err != nil {
			return fmt.Errorf("admit scanner image page for %q: %w", instance.InstanceID, err)
		}
		skipped = append(skipped, schedule.DerivedTargetSkipEvidenceByReasonForClass(
			string(instance.CollectorKind), schedule.TargetClassDerivedImage,
			"running_image", len(targets), admission.InsertedWorkItems,
			admission.AdmissionSkipped, nil, map[string][]schedule.DerivedTargetSkippedPackage{})...)
		s.recordScannerDerivedPage(ctx, ScannerDerivedPageObservation{
			InsertedWorkItems: admission.InsertedWorkItems,
			Skipped:           skipped,
			PageCandidates:    len(page.Candidates),
		})
		if s.Logger != nil {
			s.Logger.InfoContext(ctx, "scanner image discovery page admitted",
				"collector_instance_id", instance.InstanceID,
				"candidates", len(page.Candidates), "targets", len(targets),
				"work_items", admission.InsertedWorkItems,
				"page_skipped_tag_only", page.SkippedTagOnly,
				"page_skipped_unresolved_config", page.SkippedUnresolvedConfig,
				"planner_skipped", schedule.DerivedTargetSkipEvidenceSummary(skipped),
				"cursor_version", page.NextCursor.Version, "cycle_exhausted", page.Exhausted)
		}
	}
	return nil
}

func (s Service) scannerImageDiscoveryRequest(instanceID string) workflow.RunningImageDiscoveryRequest {
	policy := s.Config.ScannerImageDiscovery
	request := workflow.RunningImageDiscoveryRequest{
		ScannerCollectorInstanceID: instanceID, DiscoveryPolicyRevision: policy.Revision,
		ExpectedRolePolicyRevision: s.ControlRolePolicyRevision,
		ExpectedDatabaseIdentity:   s.ControlDatabaseIdentity,
		Limit:                      scannerImageDiscoveryPageLimit,
	}
	if policy.Mode == ScannerImageDiscoveryHosted {
		boundary := s.Config.TenantBoundary.normalize()
		request.AuthorizationNamespace = workflow.ScannerAuthorizationHosted
		request.TenantID, request.WorkspaceID = boundary.TenantID, boundary.WorkspaceID
		request.SubjectClass, request.PolicyRevisionHash = boundary.SubjectClass, boundary.PolicyRevisionHash
	} else {
		request.AuthorizationNamespace = workflow.ScannerAuthorizationLocal
		request.LocalSourceScopeIDs = append([]string(nil), policy.SourceScopeIDs...)
	}
	return request
}

func defaultScannerImageConfiguration(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}
