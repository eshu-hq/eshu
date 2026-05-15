package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"go.opentelemetry.io/otel/metric"
)

const (
	awsFreshnessClaimOwner        = "workflow-coordinator"
	defaultAWSFreshnessClaimLimit = 100
	awsFreshnessActionClaimed     = "handoff_claimed"
	awsFreshnessActionCreated     = "handoff_created"
	awsFreshnessActionFailed      = "handoff_failed"
)

// AWSFreshnessTriggerStore is the durable trigger queue surface used by the
// workflow coordinator handoff loop.
type AWSFreshnessTriggerStore interface {
	ClaimQueuedTriggers(context.Context, string, time.Time, int) ([]freshness.StoredTrigger, error)
	MarkTriggersHandedOff(context.Context, []string, time.Time) error
	MarkTriggersFailed(context.Context, []string, time.Time, string, string) error
}

// AWSFreshnessPlanner plans ordinary AWS workflow work from claimed freshness
// triggers.
type AWSFreshnessPlanner interface {
	PlanAWSFreshnessWork(context.Context, AWSFreshnessPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

type awsFreshnessEventCounter interface {
	Add(context.Context, int64, ...metric.AddOption)
}

func (s Service) scheduleAWSFreshnessWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.AWSFreshnessTriggers == nil {
		return nil
	}
	if s.AWSFreshnessPlanner == nil {
		return fmt.Errorf("AWS freshness planner is required before claiming freshness triggers")
	}
	claimLimit := defaultAWSFreshnessClaimLimit
	triggers, err := s.AWSFreshnessTriggers.ClaimQueuedTriggers(ctx, awsFreshnessClaimOwner, observedAt.UTC(), claimLimit)
	if err != nil {
		return fmt.Errorf("claim AWS freshness triggers: %w", err)
	}
	for _, trigger := range triggers {
		s.recordAWSFreshnessEvent(ctx, trigger.Kind, awsFreshnessActionClaimed)
	}
	if len(triggers) == 0 {
		return nil
	}
	assignments := s.assignAWSFreshnessTriggers(ctx, observedAt.UTC(), triggers, instances)
	for _, assignment := range assignments {
		if len(assignment.triggers) == 0 {
			continue
		}
		if err := s.handoffAWSFreshnessAssignment(ctx, observedAt.UTC(), assignment); err != nil {
			return err
		}
	}
	return nil
}

type awsFreshnessAssignment struct {
	instance workflow.CollectorInstance
	triggers []freshness.StoredTrigger
}

func (s Service) assignAWSFreshnessTriggers(
	ctx context.Context,
	observedAt time.Time,
	triggers []freshness.StoredTrigger,
	instances []workflow.CollectorInstance,
) []awsFreshnessAssignment {
	assignments := map[string]awsFreshnessAssignment{}
	for _, trigger := range triggers {
		instance, ok := findAWSFreshnessInstance(trigger, instances)
		if !ok {
			s.markAWSFreshnessFailed(ctx, []freshness.StoredTrigger{trigger}, observedAt, "unauthorized_target", "no AWS collector instance authorizes the freshness target")
			continue
		}
		assignment := assignments[instance.InstanceID]
		assignment.instance = instance
		assignment.triggers = append(assignment.triggers, trigger)
		assignments[instance.InstanceID] = assignment
	}
	output := make([]awsFreshnessAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		output = append(output, assignment)
	}
	return output
}

func findAWSFreshnessInstance(
	trigger freshness.StoredTrigger,
	instances []workflow.CollectorInstance,
) (workflow.CollectorInstance, bool) {
	for _, instance := range instances {
		if !shouldScheduleAWSFreshness(instance) {
			continue
		}
		scopes, err := parseAWSFreshnessTargetScopes(instance.Configuration)
		if err != nil {
			continue
		}
		if awsFreshnessTargetAuthorized(trigger.Target(), scopes) {
			return instance, true
		}
	}
	return workflow.CollectorInstance{}, false
}

func shouldScheduleAWSFreshness(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorAWS &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) handoffAWSFreshnessAssignment(
	ctx context.Context,
	observedAt time.Time,
	assignment awsFreshnessAssignment,
) error {
	run, items, err := s.AWSFreshnessPlanner.PlanAWSFreshnessWork(ctx, AWSFreshnessPlanRequest{
		Instance:   assignment.instance,
		Triggers:   assignment.triggers,
		ObservedAt: observedAt,
		PlanKey:    s.awsFreshnessPlanKey(observedAt),
	})
	if err != nil {
		s.markAWSFreshnessFailed(ctx, assignment.triggers, observedAt, "plan_failed", err.Error())
		return fmt.Errorf("plan AWS freshness work for %q: %w", assignment.instance.InstanceID, err)
	}
	if err := s.Store.CreateRun(ctx, run); err != nil {
		s.markAWSFreshnessFailed(ctx, assignment.triggers, observedAt, "workflow_handoff_failed", err.Error())
		return fmt.Errorf("create AWS freshness workflow run for %q: %w", assignment.instance.InstanceID, err)
	}
	if err := s.Store.EnqueueWorkItems(ctx, items); err != nil {
		s.markAWSFreshnessFailed(ctx, assignment.triggers, observedAt, "workflow_handoff_failed", err.Error())
		return fmt.Errorf("enqueue AWS freshness work items for %q: %w", assignment.instance.InstanceID, err)
	}
	triggerIDs := awsFreshnessTriggerIDs(assignment.triggers)
	if err := s.AWSFreshnessTriggers.MarkTriggersHandedOff(ctx, triggerIDs, observedAt); err != nil {
		return fmt.Errorf("mark AWS freshness triggers handed off: %w", err)
	}
	for _, trigger := range assignment.triggers {
		s.recordAWSFreshnessEvent(ctx, trigger.Kind, awsFreshnessActionCreated)
	}
	return nil
}

func (s Service) markAWSFreshnessFailed(
	ctx context.Context,
	triggers []freshness.StoredTrigger,
	observedAt time.Time,
	failureClass string,
	failureMessage string,
) {
	ids := awsFreshnessTriggerIDs(triggers)
	if len(ids) > 0 {
		_ = s.AWSFreshnessTriggers.MarkTriggersFailed(ctx, ids, observedAt, failureClass, failureMessage)
	}
	for _, trigger := range triggers {
		s.recordAWSFreshnessEvent(ctx, trigger.Kind, awsFreshnessActionFailed)
	}
}

func (s Service) awsFreshnessPlanKey(observedAt time.Time) string {
	interval := s.Config.ReconcileInterval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	return "freshness-" + observedAt.UTC().Truncate(interval).Format("20060102T150405Z")
}

func (s Service) recordAWSFreshnessEvent(ctx context.Context, kind freshness.EventKind, action string) {
	if s.AWSFreshnessEvents == nil {
		return
	}
	kindValue := strings.TrimSpace(string(kind))
	if kindValue == "" {
		kindValue = "unknown"
	}
	s.AWSFreshnessEvents.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrKind(kindValue),
		telemetry.AttrAction(action),
	))
}

func awsFreshnessTriggerIDs(triggers []freshness.StoredTrigger) []string {
	ids := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		id := strings.TrimSpace(trigger.TriggerID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
