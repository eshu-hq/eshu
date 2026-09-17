// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // CI/CD scheduling and durable admission remain on root Service methods.

import (
	"context"
	"fmt"
	"time"

	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// CICDRunPlanner plans CI/CD run workflow rows from collector instance
// configuration.
type CICDRunPlanner interface {
	PlanCICDRunWork(context.Context, cicdrun.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// scheduleCICDRunWork plans one claimable work item per enabled CI/CD run
// target. It only creates durable workflow rows; provider calls and fact
// emission remain owned by the CI/CD run collector runtime.
func (s Service) scheduleCICDRunWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleCICDRun(instance) {
			continue
		}
		if s.CICDRunPlanner == nil {
			return fmt.Errorf("ci/cd run planner is required for active ci_cd_run collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.CICDRunPlanner.PlanCICDRunWork(ctx, cicdrun.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan ci/cd run work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create ci/cd run scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleCICDRun(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorCICDRun &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
