// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) filterCollectorInstancesByEgress(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) ([]workflow.CollectorInstance, error) {
	if len(instances) == 0 {
		return nil, nil
	}
	filtered := make([]workflow.CollectorInstance, 0, len(instances))
	for _, instance := range instances {
		decision := s.Config.CollectorEgressPolicy.Decide(instance.CollectorKind)
		if decision.Action == CollectorEgressActionDeny && instance.Enabled && instance.ClaimsEnabled {
			if err := s.recordCollectorEgressAudit(ctx, observedAt, instance, decision); err != nil {
				return nil, fmt.Errorf("record collector egress audit for %q: %w", instance.CollectorKind, err)
			}
			if s.Logger != nil {
				s.Logger.Info(
					"workflow coordinator skipped collector scheduling by egress policy",
					"collector_kind", instance.CollectorKind,
					"reason", decision.Reason,
				)
			}
			continue
		}
		filtered = append(filtered, instance)
	}
	return filtered, nil
}
