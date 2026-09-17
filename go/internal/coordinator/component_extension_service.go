// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Root owns scheduling, policy, audit, and durable admission.

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ComponentExtensionPlanner plans generic component extension workflow rows.
type ComponentExtensionPlanner interface {
	PlanComponentExtensionWork(context.Context, extension.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleComponentExtensionWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleComponentExtension(instance) {
			continue
		}
		config, configOK, configErr := activation.ParseConfig(instance.Configuration)
		if configErr == nil && configOK {
			decision := s.Config.ExtensionEgressPolicy.Decide(ExtensionEgressRequest{
				ComponentID:   config.ComponentID,
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
			})
			if decision.Action == ExtensionEgressActionDeny {
				if err := s.recordExtensionEgressAudit(ctx, observedAt, instance, config, decision); err != nil {
					return fmt.Errorf("record component extension egress audit for %q: %w", instance.InstanceID, err)
				}
				if s.Logger != nil {
					s.Logger.Info(
						"workflow coordinator skipped component extension scheduling by egress policy",
						"collector_kind", instance.CollectorKind,
						"component_id", config.ComponentID,
						"instance_id", instance.InstanceID,
						"reason", decision.Reason,
					)
				}
				continue
			}
		}
		if s.ComponentExtensionPlanner == nil {
			return fmt.Errorf("component extension planner is required for active component extension collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.ComponentExtensionPlanner.PlanComponentExtensionWork(
			ctx,
			extension.PlanRequest{
				Instance:   instance,
				ObservedAt: observedAt,
				PlanKey:    scheduledPlanKey(instance, observedAt, interval),
			},
		)
		if err != nil {
			return fmt.Errorf("plan component extension work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create component extension scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleComponentExtension(instance workflow.CollectorInstance) bool {
	if !instance.Enabled || !instance.ClaimsEnabled {
		return false
	}
	_, ok, err := activation.ParseConfig(instance.Configuration)
	return ok || err != nil
}
