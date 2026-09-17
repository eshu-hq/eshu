// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	scannerworker "github.com/eshu-hq/eshu/go/internal/coordinator/scanner/worker"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ScannerWorkerPlanner plans scanner-worker workflow rows from collector
// instance configuration.
type ScannerWorkerPlanner interface {
	PlanScannerWorkerWork(
		context.Context,
		scannerworker.PlanRequest,
	) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleScannerWorkerWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleScannerWorker(instance) {
			continue
		}
		if s.ScannerWorkerPlanner == nil {
			return fmt.Errorf("scanner-worker planner is required for active scanner_worker collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.ScannerWorkerPlanner.PlanScannerWorkerWork(ctx, scannerworker.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan scanner-worker work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create scanner-worker scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleScannerWorker(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorScannerWorker &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
