// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachability

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/contract"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Skip reasons name why a page's repository got no work item. They are a closed
// set safe for log keys: none holds a repository value.
const (
	// SkipInvalidRepositoryID counts a scope whose source key is not a
	// canonical repository id.
	SkipInvalidRepositoryID = "invalid_repository_id"
	// SkipDuplicateRepository counts a repository already on the page.
	SkipDuplicateRepository = "duplicate_repository"
	// SkipUnsupportedRemote counts a repository with no remote or a non-canonical
	// one, such as a local path or file URL. It checks syntax only: a dead or
	// revoked remote passes, is claimed, and fails at fetch.
	SkipUnsupportedRemote = "unsupported_remote"
	// SkipUnsupportedRemoteWithEvidence counts the same defect on a repository
	// whose go-reachability scope still holds Go evidence. It is an invariant
	// alarm: a stored remote keeps one form for a scope's life, so a non-zero count
	// means identity normalization or the remote stamp changed, a bug to root-cause.
	// The commit-skew fence still bounds that evidence (eshu-hq/eshu#10055).
	SkipUnsupportedRemoteWithEvidence = "unsupported_remote_with_evidence"
	// SkipMissingCommit counts a generation with no full observed commit.
	SkipMissingCommit = "missing_commit"
)

// PlanRequest carries one rotation page for one activation.
type PlanRequest struct {
	Instance     workflow.CollectorInstance
	ObservedAt   time.Time
	PlanKey      string
	Repositories []scannercomponent.GoModuleRepository
}

// RunID returns the run a plan key's page lands under. The root reads it before
// reading candidates, so a bucket with a planned page costs one primary-key
// probe. The "derived" segment keeps it distinct from the generic
// component-extension run.
func RunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf("%s:%s:%s:derived:%s",
		scope.CollectorScannerWorker, strings.TrimSpace(instance.InstanceID), triggerKind(instance), strings.TrimSpace(planKey))
}

// Plan returns one run and one work item per plannable repository, plus a count
// per skip reason. One unplannable repository never fails the page. With nothing
// plannable it returns a zero run and no items.
func Plan(request PlanRequest) (workflow.Run, []workflow.WorkItem, map[string]int, error) {
	if err := validate(request); err != nil {
		return workflow.Run{}, nil, nil, err
	}
	observedAt := request.ObservedAt.UTC()
	runID := RunID(request.Instance, request.PlanKey)
	skipped := map[string]int{}
	seen := make(map[string]struct{}, len(request.Repositories))
	targets := make([]scannercomponent.GoReachabilityTarget, 0, len(request.Repositories))
	items := make([]workflow.WorkItem, 0, len(request.Repositories))
	for _, repository := range request.Repositories {
		target, reason := targetFor(repository, seen)
		if reason != "" {
			skipped[reason]++
			continue
		}
		item, err := workItem(request.Instance, target, runID, request.PlanKey, observedAt, len(items))
		if err != nil {
			return workflow.Run{}, nil, nil, err
		}
		targets = append(targets, target)
		items = append(items, item)
	}
	if len(items) == 0 {
		return workflow.Run{}, nil, skipped, nil
	}
	requested, err := json.Marshal(scannercomponent.RequestedScopeSet{
		CollectorInstanceID: strings.TrimSpace(request.Instance.InstanceID),
		Analyzer:            string(scannercomponent.AnalyzerGoReachability),
		Targets:             targets,
	})
	if err != nil {
		return workflow.Run{}, nil, nil, fmt.Errorf("encode go reachability requested scope set: %w", err)
	}
	return workflow.Run{
		RunID:              runID,
		TriggerKind:        triggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  string(requested),
		RequestedCollector: string(scope.CollectorScannerWorker),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}, items, skipped, nil
}

func validate(request PlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("go reachability plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorScannerWorker {
		return fmt.Errorf("go reachability planner requires collector_kind %q", scope.CollectorScannerWorker)
	}
	if request.ObservedAt.IsZero() {
		return errors.New("go reachability planner observed_at must not be zero")
	}
	if err := contract.ValidateSafePlanKey("go reachability planner", request.PlanKey); err != nil {
		return err
	}
	if len(request.Repositories) > scannercomponent.GoModulePageSize {
		return fmt.Errorf("go reachability page exceeds %d repositories", scannercomponent.GoModulePageSize)
	}
	return nil
}

// targetFor validates one repository, returning the skip reason when it cannot
// be planned. A repository is recorded as seen only once it has a valid id.
func targetFor(
	repository scannercomponent.GoModuleRepository,
	seen map[string]struct{},
) (scannercomponent.GoReachabilityTarget, string) {
	scopeID, err := scannercomponent.GoReachabilityScopeID(repository.RepositoryID)
	if err != nil {
		return scannercomponent.GoReachabilityTarget{}, SkipInvalidRepositoryID
	}
	if _, duplicate := seen[scopeID]; duplicate {
		return scannercomponent.GoReachabilityTarget{}, SkipDuplicateRepository
	}
	seen[scopeID] = struct{}{}
	if !scannercomponent.ValidCloneURL(repository.RemoteURL) {
		if repository.HasGoEvidence {
			return scannercomponent.GoReachabilityTarget{}, SkipUnsupportedRemoteWithEvidence
		}
		return scannercomponent.GoReachabilityTarget{}, SkipUnsupportedRemote
	}
	target := scannercomponent.GoReachabilityTarget{
		ScopeID:          scopeID,
		TargetKind:       string(scannercomponent.TargetRepository),
		RepositoryID:     repository.RepositoryID,
		SourceCommitSHA:  repository.SourceCommitSHA,
		CloneURL:         repository.RemoteURL,
		AcceptanceUnitID: scopeID,
	}
	// The id and remote are proven above, so the commit is the only defect left.
	if err := target.Validate(); err != nil {
		return scannercomponent.GoReachabilityTarget{}, SkipMissingCommit
	}
	return target, ""
}

// workItem mints one claimable item. The commit is part of the identity, so a
// new commit is a new generation of the same scope. The open-target guard holds it
// back only while the earlier item's run is not yet complete or failed. One terminal
// item sets its whole run failed while siblings stay claimable, and the guard stops
// counting them as open, so a second item for such a scope is possible
// (eshu-hq/eshu#10091). The activation stays at replicas 0 until that merges.
func workItem(
	instance workflow.CollectorInstance,
	target scannercomponent.GoReachabilityTarget,
	runID, planKey string,
	observedAt time.Time,
	ordinal int,
) (workflow.WorkItem, error) {
	instanceID := strings.TrimSpace(instance.InstanceID)
	generationID := string(scope.CollectorScannerWorker) + ":" + facts.StableID("GoReachabilityWorkflowGeneration", map[string]any{
		"analyzer":          string(scannercomponent.AnalyzerGoReachability),
		"instance_id":       instanceID,
		"plan_key":          strings.TrimSpace(planKey),
		"scope_id":          target.ScopeID,
		"source_commit_sha": target.SourceCommitSHA,
	})
	createdAt := schedule.TargetCreatedAt(observedAt, ordinal)
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorScannerWorker, instanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: instanceID,
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             target.ScopeID,
		AcceptanceUnitID:    target.AcceptanceUnitID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey: fmt.Sprintf("%s:%s:%s",
			scope.CollectorScannerWorker, instanceID, scannercomponent.AnalyzerGoReachability),
		ClaimEcosystem: scannercomponent.EcosystemGo,
		Status:         workflow.WorkItemStatusPending,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}

func triggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}
