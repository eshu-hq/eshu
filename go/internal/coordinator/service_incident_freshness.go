// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	incidentFreshnessClaimOwner        = "workflow-coordinator"
	defaultIncidentFreshnessClaimLimit = 100
)

// IncidentFreshnessTriggerStore is the durable trigger queue surface for
// PagerDuty and Jira webhook-driven collector wake-ups.
type IncidentFreshnessTriggerStore interface {
	ClaimQueuedTriggers(context.Context, string, time.Time, int) ([]webhook.StoredIncidentFreshnessTrigger, error)
	MarkTriggersHandedOff(context.Context, []string, time.Time) error
	MarkTriggersFailed(context.Context, []string, time.Time, string, string) error
}

type incidentFreshnessAssignment struct {
	instance workflow.CollectorInstance
	triggers []webhook.StoredIncidentFreshnessTrigger
}

func (s Service) runIncidentFreshnessHandoff(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.IncidentFreshnessTriggers == nil {
		return nil
	}
	observedAt := s.now().UTC()
	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		return fmt.Errorf("list durable collector instances for incident freshness handoff: %w", err)
	}
	filtered, err := s.filterCollectorInstancesByEgress(ctx, observedAt, instances)
	if err != nil {
		return err
	}
	return s.scheduleIncidentFreshnessWork(ctx, observedAt, filtered)
}

func (s Service) scheduleIncidentFreshnessWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.IncidentFreshnessTriggers == nil {
		return nil
	}
	triggers, err := s.IncidentFreshnessTriggers.ClaimQueuedTriggers(
		ctx,
		incidentFreshnessClaimOwner,
		observedAt.UTC(),
		defaultIncidentFreshnessClaimLimit,
	)
	if err != nil {
		return fmt.Errorf("claim incident freshness triggers: %w", err)
	}
	if len(triggers) == 0 {
		return nil
	}
	assignments := s.assignIncidentFreshnessTriggers(ctx, observedAt.UTC(), triggers, instances)
	for _, assignment := range assignments {
		if len(assignment.triggers) == 0 {
			continue
		}
		if err := s.handoffIncidentFreshnessAssignment(ctx, observedAt.UTC(), assignment); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) assignIncidentFreshnessTriggers(
	ctx context.Context,
	observedAt time.Time,
	triggers []webhook.StoredIncidentFreshnessTrigger,
	instances []workflow.CollectorInstance,
) []incidentFreshnessAssignment {
	assignments := map[string]incidentFreshnessAssignment{}
	for _, trigger := range triggers {
		instance, ok := findIncidentFreshnessInstance(trigger, instances)
		if !ok {
			s.markIncidentFreshnessFailed(ctx, []webhook.StoredIncidentFreshnessTrigger{trigger}, observedAt, "unauthorized_target", "no collector instance authorizes the incident freshness target")
			continue
		}
		assignment := assignments[instance.InstanceID]
		assignment.instance = instance
		assignment.triggers = append(assignment.triggers, trigger)
		assignments[instance.InstanceID] = assignment
	}
	output := make([]incidentFreshnessAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		output = append(output, assignment)
	}
	sort.Slice(output, func(i, j int) bool {
		return output[i].instance.InstanceID < output[j].instance.InstanceID
	})
	return output
}

func findIncidentFreshnessInstance(
	trigger webhook.StoredIncidentFreshnessTrigger,
	instances []workflow.CollectorInstance,
) (workflow.CollectorInstance, bool) {
	for _, instance := range instances {
		if !shouldScheduleIncidentFreshness(instance, trigger.Provider) {
			continue
		}
		if incidentFreshnessScopeAuthorized(trigger, instance) {
			return instance, true
		}
	}
	return workflow.CollectorInstance{}, false
}

func shouldScheduleIncidentFreshness(instance workflow.CollectorInstance, provider webhook.Provider) bool {
	if !instance.Enabled || !instance.ClaimsEnabled {
		return false
	}
	switch provider {
	case webhook.ProviderPagerDuty:
		return instance.CollectorKind == scope.CollectorPagerDuty
	case webhook.ProviderJira:
		return instance.CollectorKind == scope.CollectorJira
	default:
		return false
	}
}

func incidentFreshnessScopeAuthorized(
	trigger webhook.StoredIncidentFreshnessTrigger,
	instance workflow.CollectorInstance,
) bool {
	switch trigger.Provider {
	case webhook.ProviderPagerDuty:
		targets, err := parsePagerDutyRuntimeTargets(instance.Configuration)
		if err != nil {
			return false
		}
		return pagerDutyScopeAuthorized(trigger.ScopeID, targets)
	case webhook.ProviderJira:
		targets, err := parseJiraRuntimeTargets(instance.Configuration)
		if err != nil {
			return false
		}
		return jiraScopeAuthorized(trigger.ScopeID, targets)
	default:
		return false
	}
}

func pagerDutyScopeAuthorized(scopeID string, targets []pagerDutyTargetConfiguration) bool {
	for _, target := range targets {
		if strings.TrimSpace(target.ScopeID) == strings.TrimSpace(scopeID) {
			return true
		}
	}
	return false
}

func jiraScopeAuthorized(scopeID string, targets []jiraTargetConfiguration) bool {
	for _, target := range targets {
		if strings.TrimSpace(target.ScopeID) == strings.TrimSpace(scopeID) {
			return true
		}
	}
	return false
}

func (s Service) handoffIncidentFreshnessAssignment(
	ctx context.Context,
	observedAt time.Time,
	assignment incidentFreshnessAssignment,
) error {
	switch assignment.instance.CollectorKind {
	case scope.CollectorPagerDuty:
		return s.handoffPagerDutyFreshnessAssignment(ctx, observedAt, assignment)
	case scope.CollectorJira:
		return s.handoffJiraFreshnessAssignment(ctx, observedAt, assignment)
	default:
		s.markIncidentFreshnessFailed(ctx, assignment.triggers, observedAt, "unsupported_provider", "incident freshness provider is not supported")
		return nil
	}
}

func (s Service) handoffPagerDutyFreshnessAssignment(
	ctx context.Context,
	observedAt time.Time,
	assignment incidentFreshnessAssignment,
) error {
	if s.PagerDutyPlanner == nil {
		return fmt.Errorf("pagerduty planner is required before claiming incident freshness triggers")
	}
	run, items, err := s.PagerDutyPlanner.PlanPagerDutyWork(ctx, PagerDutyPlanRequest{
		Instance:    assignment.instance,
		ObservedAt:  observedAt,
		PlanKey:     s.incidentFreshnessPlanKey(observedAt),
		TriggerKind: workflow.TriggerKindWebhook,
		ScopeIDs:    incidentFreshnessScopeIDs(assignment.triggers),
	})
	if err != nil {
		s.markIncidentFreshnessFailed(ctx, assignment.triggers, observedAt, "plan_failed", err.Error())
		return fmt.Errorf("plan pagerduty freshness work for %q: %w", assignment.instance.InstanceID, err)
	}
	return s.finishIncidentFreshnessHandoff(ctx, observedAt, assignment, run, items)
}

func (s Service) handoffJiraFreshnessAssignment(
	ctx context.Context,
	observedAt time.Time,
	assignment incidentFreshnessAssignment,
) error {
	if s.JiraPlanner == nil {
		return fmt.Errorf("jira planner is required before claiming incident freshness triggers")
	}
	run, items, err := s.JiraPlanner.PlanJiraWork(ctx, JiraPlanRequest{
		Instance:    assignment.instance,
		ObservedAt:  observedAt,
		PlanKey:     s.incidentFreshnessPlanKey(observedAt),
		TriggerKind: workflow.TriggerKindWebhook,
		ScopeIDs:    incidentFreshnessScopeIDs(assignment.triggers),
	})
	if err != nil {
		s.markIncidentFreshnessFailed(ctx, assignment.triggers, observedAt, "plan_failed", err.Error())
		return fmt.Errorf("plan jira freshness work for %q: %w", assignment.instance.InstanceID, err)
	}
	return s.finishIncidentFreshnessHandoff(ctx, observedAt, assignment, run, items)
}

func (s Service) finishIncidentFreshnessHandoff(
	ctx context.Context,
	observedAt time.Time,
	assignment incidentFreshnessAssignment,
	run workflow.Run,
	items []workflow.WorkItem,
) error {
	if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, assignment.instance, run, items); err != nil {
		s.markIncidentFreshnessFailed(ctx, assignment.triggers, observedAt, "workflow_handoff_failed", err.Error())
		return fmt.Errorf("create incident freshness workflow run for %q: %w", assignment.instance.InstanceID, err)
	}
	triggerIDs := incidentFreshnessTriggerIDs(assignment.triggers)
	if err := s.IncidentFreshnessTriggers.MarkTriggersHandedOff(ctx, triggerIDs, observedAt); err != nil {
		return fmt.Errorf("mark incident freshness triggers handed off: %w", err)
	}
	return nil
}

func (s Service) incidentFreshnessPlanKey(observedAt time.Time) string {
	interval := s.Config.ReconcileInterval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	return "freshness-" + observedAt.UTC().Truncate(interval).Format("20060102T150405Z")
}

func (s Service) markIncidentFreshnessFailed(
	ctx context.Context,
	triggers []webhook.StoredIncidentFreshnessTrigger,
	observedAt time.Time,
	failureClass string,
	failureMessage string,
) {
	ids := incidentFreshnessTriggerIDs(triggers)
	if len(ids) > 0 {
		// Best-effort: we are already on the failure path (the primary handoff
		// could not be planned), so a failed failure-marking write must not abort
		// reconciliation. It is logged rather than swallowed so an operator can
		// see that the triggers were not durably marked failed (#3793).
		if err := s.IncidentFreshnessTriggers.MarkTriggersFailed(ctx, ids, observedAt, failureClass, failureMessage); err != nil && s.Logger != nil {
			s.Logger.Warn(
				"incident-freshness trigger failure marking did not persist",
				"error", err,
				"trigger_count", len(ids),
				"failure_class", failureClass,
			)
		}
	}
}

func incidentFreshnessTriggerIDs(triggers []webhook.StoredIncidentFreshnessTrigger) []string {
	ids := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		if id := strings.TrimSpace(trigger.TriggerID); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func incidentFreshnessScopeIDs(triggers []webhook.StoredIncidentFreshnessTrigger) []string {
	seen := make(map[string]struct{}, len(triggers))
	scopeIDs := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		scopeID := strings.TrimSpace(trigger.ScopeID)
		if scopeID == "" {
			continue
		}
		if _, ok := seen[scopeID]; ok {
			continue
		}
		seen[scopeID] = struct{}{}
		scopeIDs = append(scopeIDs, scopeID)
	}
	sort.Strings(scopeIDs)
	return scopeIDs
}
