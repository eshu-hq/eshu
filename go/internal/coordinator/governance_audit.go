// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Governance-audit appender wiring and its root Service dependencies stay in the root; the event shape and identity helpers moved to the governance/audit subpackage (#6781).

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/egress"
	"github.com/eshu-hq/eshu/go/internal/coordinator/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) recordCollectorEgressAudit(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	decision egress.CollectorDecision,
) error {
	if s.GovernanceAudit == nil || decision.Action != egress.CollectorActionDeny {
		return nil
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeCollectorActivation,
		ActorClass:         governanceaudit.ActorClassServicePrincipal,
		ServicePrincipalID: audit.ServiceID,
		ScopeClass:         governanceaudit.ScopeClassCollectorKind,
		ScopeIDHash:        audit.Hash("collector", string(instance.CollectorKind)),
		Decision:           governanceAuditDecision(decision.Reason),
		ReasonCode:         decision.Reason,
		CorrelationID:      audit.CorrelationID("collector-egress", string(instance.CollectorKind)),
		OccurredAt:         governanceAuditOccurredAt(observedAt, s.Config.ReconcileInterval),
	}
	return s.appendGovernanceAudit(ctx, event)
}

func (s Service) recordExtensionEgressAudit(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	config activation.Config,
	decision egress.ExtensionDecision,
) error {
	if s.GovernanceAudit == nil || decision.Action != egress.ExtensionActionDeny {
		return nil
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeExtensionActivation,
		ActorClass:         governanceaudit.ActorClassServicePrincipal,
		ServicePrincipalID: audit.ServiceID,
		ScopeClass:         governanceaudit.ScopeClassExtensionComponent,
		ScopeIDHash: audit.Hash(
			"extension",
			config.ComponentID,
			instance.InstanceID,
			string(instance.CollectorKind),
		),
		Decision:      governanceAuditDecision(decision.Reason),
		ReasonCode:    decision.Reason,
		CorrelationID: audit.CorrelationID("extension-egress", config.ComponentID, instance.InstanceID),
		OccurredAt:    governanceAuditOccurredAt(observedAt, s.Config.ReconcileInterval),
	}
	return s.appendGovernanceAudit(ctx, event)
}

func (s Service) appendGovernanceAudit(ctx context.Context, event governanceaudit.Event) error {
	auditCtx, cancel := context.WithTimeout(ctx, audit.AppendTimeout)
	defer cancel()
	if err := s.GovernanceAudit.Append(auditCtx, []governanceaudit.Event{event}); err != nil {
		return fmt.Errorf("append governance audit event: %w", err)
	}
	return nil
}

func governanceAuditDecision(reason string) governanceaudit.Decision {
	if strings.TrimSpace(reason) == egress.CollectorReasonMissing ||
		strings.TrimSpace(reason) == egress.ExtensionReasonMissing {
		return governanceaudit.DecisionUnavailable
	}
	return governanceaudit.DecisionDenied
}

func governanceAuditOccurredAt(observedAt time.Time, interval time.Duration) time.Time {
	observedAt = observedAt.UTC()
	if interval <= 0 {
		interval = schedule.DefaultReconcileInterval
	}
	return observedAt.Truncate(interval)
}
