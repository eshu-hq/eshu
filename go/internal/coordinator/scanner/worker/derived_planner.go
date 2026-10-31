// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// DerivedImagePlanRequest carries one bounded, protected discovery page. The
// page is a planning input; durable admission rechecks source authority.
type DerivedImagePlanRequest struct {
	Instance               workflow.CollectorInstance
	ObservedAt             time.Time
	PlanKey                string
	AuthorizationNamespace workflow.ScannerAuthorizationNamespace
	TenantID               string
	WorkspaceID            string
	SubjectClass           string
	PolicyRevisionHash     string
	Candidates             []workflow.RunningImageObservationCandidate
}

// PlanDerivedImageWork groups the page by immutable image and authorization
// boundary, preserving every distinct canonical source binding for admission.
// Candidates that cannot form a valid target skip with a recorded reason
// instead of failing the page, so one malformed candidate cannot stall the
// durable discovery cursor; admission rechecks every admitted binding.
func (p WorkPlanner) PlanDerivedImageWork(
	_ context.Context, request DerivedImagePlanRequest,
) (workflow.Run, []workflow.WorkItem, []workflow.ScannerImageTarget, []schedule.DerivedTargetSkipEvidence, error) {
	empty := func(err error) (workflow.Run, []workflow.WorkItem, []workflow.ScannerImageTarget, []schedule.DerivedTargetSkipEvidence, error) {
		return workflow.Run{}, nil, nil, nil, err
	}
	if err := validateScannerWorkerPlanRequest(PlanRequest{
		Instance: request.Instance, ObservedAt: request.ObservedAt, PlanKey: request.PlanKey,
	}); err != nil {
		return empty(err)
	}
	if len(request.Candidates) > 256 {
		return empty(fmt.Errorf("scanner image discovery page exceeds 256 candidates"))
	}
	var config struct {
		Analyzer                string `json:"analyzer"`
		DeriveFromRunningImages bool   `json:"derive_from_running_images"`
	}
	if err := json.Unmarshal([]byte(request.Instance.Configuration), &config); err != nil {
		return empty(fmt.Errorf("scanner image discovery configuration: %w", err))
	}
	if !config.DeriveFromRunningImages || config.Analyzer != string(scannercomponent.AnalyzerImageUnpacking) {
		return empty(fmt.Errorf("scanner image discovery requires enabled instance opt-in"))
	}
	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              scannerWorkerRunID(request.Instance, request.PlanKey),
		TriggerKind:        scannerWorkerTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedCollector: string(request.Instance.CollectorKind),
		CreatedAt:          observedAt, UpdatedAt: observedAt,
	}
	skippedCounts := map[string]map[string]int{}
	// Image-target skips record count-only rows: no package identity is
	// known at these decision points (nil packages per the callee contract).
	skippedPackages := map[string][]schedule.DerivedTargetSkippedPackage{}
	skippedSources := map[string]struct{}{}
	targets := make([]workflow.ScannerImageTarget, 0, len(request.Candidates))
	indices := make(map[string]int, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if candidate.ManifestDigest == "" || candidate.RuntimeDigest == "" {
			schedule.RecordDerivedTargetSkip(skippedCounts, skippedPackages,
				schedule.DerivedTargetSkipReasonMissingDigest, candidate.AuthorizationDomain, nil)
			skippedSources[candidate.ProducerInstanceID] = struct{}{}
			continue
		}
		target := workflow.ScannerImageTarget{
			AuthorizationNamespace: request.AuthorizationNamespace,
			TenantID:               request.TenantID, WorkspaceID: request.WorkspaceID,
			SubjectClass: request.SubjectClass, PolicyRevisionHash: request.PolicyRevisionHash,
			AuthorizationDomain: candidate.AuthorizationDomain, ManifestDigest: candidate.ManifestDigest,
			Sources: []workflow.ScannerImageSourceBinding{{
				SourceObservationID:       candidate.ObservationID,
				SourceCollectorInstanceID: candidate.ProducerInstanceID,
				SourceScopeID:             candidate.SourceScopeID, SourceGenerationID: candidate.SourceGenerationID,
				Registry: candidate.Registry, Repository: candidate.Repository,
				RuntimeDigest:          candidate.RuntimeDigest,
				RuntimeDigestKind:      workflow.ScannerRuntimeDigestKind(candidate.RuntimeDigestKind),
				RuntimeImageReference:  candidate.RuntimeImageReference,
				ManifestVerificationID: candidate.ManifestVerificationID,
			}},
		}
		target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
		if i, exists := indices[target.ScanTargetID]; exists {
			targets[i].Sources = append(targets[i].Sources, target.Sources[0])
			continue
		}
		indices[target.ScanTargetID] = len(targets)
		targets = append(targets, target)
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	metadataTargets := make([]scannerWorkerTargetConfiguration, 0, len(targets))
	kept := targets[:0]
	for i := range targets {
		target := &targets[i]
		metadata := scannerWorkerTargetConfiguration{
			ScopeID: target.ScanTargetID, TargetKind: scannercomponent.TargetImage,
		}
		item, err := scannerWorkerWorkItem(request.Instance, scannercomponent.AnalyzerImageUnpacking,
			metadata, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return empty(err)
		}
		item.TenantID, item.WorkspaceID = target.TenantID, target.WorkspaceID
		item.SubjectClass, item.PolicyRevisionHash = target.SubjectClass, target.PolicyRevisionHash
		target.WorkItemID = item.WorkItemID
		if err := target.Validate(); err != nil {
			schedule.RecordDerivedTargetSkip(skippedCounts, skippedPackages,
				schedule.DerivedTargetSkipReasonInvalidCandidate, target.AuthorizationDomain, nil)
			for _, source := range target.Sources {
				skippedSources[source.SourceCollectorInstanceID] = struct{}{}
			}
			continue
		}
		items = append(items, item)
		metadataTargets = append(metadataTargets, metadata)
		kept = append(kept, *target)
	}
	targets = kept
	run.RequestedScopeSet = scannerWorkerRequestedScopeSet(request.Instance,
		scannercomponent.AnalyzerImageUnpacking, metadataTargets)
	sources := make([]string, 0, len(skippedSources))
	for source := range skippedSources {
		if source != "" {
			sources = append(sources, source)
		}
	}
	skipped := schedule.DerivedTargetSkipEvidenceByReasonForClass(
		string(request.Instance.CollectorKind), schedule.TargetClassDerivedImage,
		"running_image", len(request.Candidates), len(targets), skippedCounts, sources, skippedPackages)
	return run, items, targets, skipped, nil
}
