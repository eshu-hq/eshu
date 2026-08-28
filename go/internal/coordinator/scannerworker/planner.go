// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	collectorscannerworker "github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/coordinator/plannercontract"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PlanRequest carries one scanner-worker planning request.
type PlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// WorkPlanner plans workflow rows for explicitly configured
// scanner-worker targets without reading source paths or image artifacts.
type WorkPlanner struct{}

type scannerWorkerRuntimeConfiguration struct {
	Analyzer         string                             `json:"analyzer"`
	SBOMTargets      []scannerWorkerTargetConfiguration `json:"sbom_targets"`
	ImageTargets     []scannerWorkerTargetConfiguration `json:"image_targets"`
	OSPackageTargets []scannerWorkerTargetConfiguration `json:"os_package_targets"`
}

type scannerWorkerTargetConfiguration struct {
	ScopeID    string                            `json:"scope_id"`
	RootPath   string                            `json:"root_path"`
	RootFSPath string                            `json:"rootfs_path"`
	LayerPaths []string                          `json:"layer_paths"`
	TargetKind collectorscannerworker.TargetKind `json:"-"`
}

// PlanScannerWorkerWork returns one run and one work item per configured
// scanner-worker target.
func (p WorkPlanner) PlanScannerWorkerWork(
	_ context.Context,
	request PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateScannerWorkerPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	analyzer, targets, err := parseScannerWorkerRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if len(targets) == 0 {
		return workflow.Run{}, nil, nil
	}
	if err := validateUniqueScannerWorkerTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              scannerWorkerRunID(request.Instance, request.PlanKey),
		TriggerKind:        scannerWorkerTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  scannerWorkerRequestedScopeSet(request.Instance, analyzer, targets),
		RequestedCollector: string(scope.CollectorScannerWorker),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := scannerWorkerWorkItem(request.Instance, analyzer, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateScannerWorkerPlanRequest(request PlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("scanner-worker plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorScannerWorker {
		return fmt.Errorf("scanner-worker planner requires collector_kind %q", scope.CollectorScannerWorker)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("scanner-worker planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("scanner-worker planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("scanner-worker planner observed_at must not be zero")
	}
	if err := plannercontract.ValidateSafePlanKey("scanner-worker planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func parseScannerWorkerRuntimeTargets(raw string) (collectorscannerworker.AnalyzerKind, []scannerWorkerTargetConfiguration, error) {
	var decoded scannerWorkerRuntimeConfiguration
	if err := json.Unmarshal([]byte(defaultScannerWorkerConfiguration(raw)), &decoded); err != nil {
		return "", nil, fmt.Errorf("decode scanner-worker collector configuration: %w", err)
	}
	analyzer := collectorscannerworker.AnalyzerKind(strings.TrimSpace(decoded.Analyzer))
	if analyzer == "" {
		analyzer = collectorscannerworker.AnalyzerSourceAnalysis
	}
	targets := scannerWorkerTargetsForAnalyzer(analyzer, decoded)
	for i := range targets {
		validated, err := validateScannerWorkerTarget(analyzer, targets[i])
		if err != nil {
			return "", nil, err
		}
		targets[i] = validated
	}
	return analyzer, targets, nil
}

func defaultScannerWorkerConfiguration(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	return raw
}

func scannerWorkerTargetsForAnalyzer(
	analyzer collectorscannerworker.AnalyzerKind,
	decoded scannerWorkerRuntimeConfiguration,
) []scannerWorkerTargetConfiguration {
	switch analyzer {
	case collectorscannerworker.AnalyzerSBOMGeneration:
		return scannerWorkerTargetsWithKind(decoded.SBOMTargets, collectorscannerworker.TargetRepository)
	case collectorscannerworker.AnalyzerImageUnpacking:
		return scannerWorkerTargetsWithKind(decoded.ImageTargets, collectorscannerworker.TargetImage)
	case collectorscannerworker.AnalyzerOSPackageExtraction:
		return scannerWorkerTargetsWithKind(decoded.OSPackageTargets, collectorscannerworker.TargetImage)
	default:
		return nil
	}
}

func scannerWorkerTargetsWithKind(
	targets []scannerWorkerTargetConfiguration,
	kind collectorscannerworker.TargetKind,
) []scannerWorkerTargetConfiguration {
	out := make([]scannerWorkerTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		target.TargetKind = kind
		out = append(out, target)
	}
	return out
}

func validateScannerWorkerTarget(
	analyzer collectorscannerworker.AnalyzerKind,
	target scannerWorkerTargetConfiguration,
) (scannerWorkerTargetConfiguration, error) {
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.RootPath = strings.TrimSpace(target.RootPath)
	target.RootFSPath = strings.TrimSpace(target.RootFSPath)
	target.LayerPaths = cleanScannerWorkerTargetStrings(target.LayerPaths)
	if target.ScopeID == "" {
		return scannerWorkerTargetConfiguration{}, fmt.Errorf("scanner-worker target scope_id is required")
	}
	switch analyzer {
	case collectorscannerworker.AnalyzerSBOMGeneration:
		if target.RootPath == "" {
			return scannerWorkerTargetConfiguration{}, fmt.Errorf("scanner-worker sbom_generation target root_path is required")
		}
	case collectorscannerworker.AnalyzerImageUnpacking:
		if target.RootFSPath == "" && len(target.LayerPaths) == 0 {
			return scannerWorkerTargetConfiguration{}, fmt.Errorf("scanner-worker image_unpacking target rootfs_path or layer_paths is required")
		}
	case collectorscannerworker.AnalyzerOSPackageExtraction:
		if target.RootFSPath == "" {
			return scannerWorkerTargetConfiguration{}, fmt.Errorf("scanner-worker os_package_extraction target rootfs_path is required")
		}
	}
	return target, nil
}

func cleanScannerWorkerTargetStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func validateUniqueScannerWorkerTargets(targets []scannerWorkerTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate scanner-worker target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func scannerWorkerRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorScannerWorker,
		strings.TrimSpace(instance.InstanceID),
		scannerWorkerTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func scannerWorkerTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func scannerWorkerRequestedScopeSet(
	instance workflow.CollectorInstance,
	analyzer collectorscannerworker.AnalyzerKind,
	targets []scannerWorkerTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID    string `json:"scope_id"`
		TargetKind string `json:"target_kind"`
	}
	payload := struct {
		CollectorInstanceID string            `json:"collector_instance_id"`
		Analyzer            string            `json:"analyzer"`
		Targets             []requestedTarget `json:"targets"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Analyzer:            string(analyzer),
		Targets:             make([]requestedTarget, 0, len(targets)),
	}
	for _, target := range targets {
		payload.Targets = append(payload.Targets, requestedTarget{
			ScopeID:    strings.TrimSpace(target.ScopeID),
			TargetKind: string(target.TargetKind),
		})
	}
	sort.Slice(payload.Targets, func(i, j int) bool {
		return payload.Targets[i].ScopeID < payload.Targets[j].ScopeID
	})
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func scannerWorkerWorkItem(
	instance workflow.CollectorInstance,
	analyzer collectorscannerworker.AnalyzerKind,
	target scannerWorkerTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	instanceID := strings.TrimSpace(instance.InstanceID)
	generationID := "scanner_worker:" + facts.StableID("ScannerWorkerWorkflowGeneration", map[string]any{
		"analyzer":    string(analyzer),
		"instance_id": instanceID,
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorScannerWorker, instanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: instanceID,
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorScannerWorker, instanceID, target.TargetKind),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
