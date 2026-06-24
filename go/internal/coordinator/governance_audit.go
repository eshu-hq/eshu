// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	governanceAuditAppendTimeout = 500 * time.Millisecond
	governanceAuditServiceID     = "svc:workflow-coordinator"
)

func (s Service) recordCollectorEgressAudit(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	decision CollectorEgressDecision,
) error {
	if s.GovernanceAudit == nil || decision.Action != CollectorEgressActionDeny {
		return nil
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeCollectorActivation,
		ActorClass:         governanceaudit.ActorClassServicePrincipal,
		ServicePrincipalID: governanceAuditServiceID,
		ScopeClass:         governanceaudit.ScopeClassCollectorKind,
		ScopeIDHash:        governanceAuditHash("collector", string(instance.CollectorKind)),
		Decision:           governanceAuditDecision(decision.Reason),
		ReasonCode:         decision.Reason,
		CorrelationID:      governanceAuditCorrelation("collector-egress", string(instance.CollectorKind)),
		OccurredAt:         governanceAuditOccurredAt(observedAt, s.Config.ReconcileInterval),
	}
	return s.appendGovernanceAudit(ctx, event)
}

func (s Service) recordExtensionEgressAudit(
	ctx context.Context,
	observedAt time.Time,
	instance workflow.CollectorInstance,
	config componentInstanceConfig,
	decision ExtensionEgressDecision,
) error {
	if s.GovernanceAudit == nil || decision.Action != ExtensionEgressActionDeny {
		return nil
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeExtensionActivation,
		ActorClass:         governanceaudit.ActorClassServicePrincipal,
		ServicePrincipalID: governanceAuditServiceID,
		ScopeClass:         governanceaudit.ScopeClassExtensionComponent,
		ScopeIDHash: governanceAuditHash(
			"extension",
			config.ComponentID,
			instance.InstanceID,
			string(instance.CollectorKind),
		),
		Decision:      governanceAuditDecision(decision.Reason),
		ReasonCode:    decision.Reason,
		CorrelationID: governanceAuditCorrelation("extension-egress", config.ComponentID, instance.InstanceID),
		OccurredAt:    governanceAuditOccurredAt(observedAt, s.Config.ReconcileInterval),
	}
	return s.appendGovernanceAudit(ctx, event)
}

func (s Service) appendGovernanceAudit(ctx context.Context, event governanceaudit.Event) error {
	auditCtx, cancel := context.WithTimeout(ctx, governanceAuditAppendTimeout)
	defer cancel()
	if err := s.GovernanceAudit.Append(auditCtx, []governanceaudit.Event{event}); err != nil {
		return fmt.Errorf("append governance audit event: %w", err)
	}
	return nil
}

func governanceAuditDecision(reason string) governanceaudit.Decision {
	if strings.TrimSpace(reason) == CollectorEgressReasonMissing ||
		strings.TrimSpace(reason) == ExtensionEgressReasonMissing {
		return governanceaudit.DecisionUnavailable
	}
	return governanceaudit.DecisionDenied
}

func governanceAuditHash(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func governanceAuditCorrelation(prefix string, parts ...string) string {
	hash := strings.TrimPrefix(governanceAuditHash(parts...), "sha256:")
	return strings.TrimSpace(prefix) + ":" + hash[:16]
}

func governanceAuditOccurredAt(observedAt time.Time, interval time.Duration) time.Time {
	observedAt = observedAt.UTC()
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	return observedAt.Truncate(interval)
}
