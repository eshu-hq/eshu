// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
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
	// defaultAWSFreshnessClaimLeaseDuration bounds how long a claimed AWS
	// freshness trigger can sit unresolved before runReapExpiredAWSFreshnessClaims
	// reclaims it back to 'queued'. It mirrors workflow.DefaultClaimLeaseTTL's
	// order of magnitude: long enough to cover one plan+handoff round trip,
	// short enough that a stranded trigger (mid-batch abort or coordinator
	// crash, #4576) is not silently lost for long.
	defaultAWSFreshnessClaimLeaseDuration = 5 * time.Minute
	awsFreshnessActionClaimed             = "handoff_claimed"
	awsFreshnessActionCreated             = "handoff_created"
	awsFreshnessActionFailed              = "handoff_failed"
	awsFreshnessActionSkipped             = "handoff_skipped"
	awsFreshnessActionReclaimed           = "claim_reclaimed"
)

// AWSFreshnessTriggerStore is the durable trigger queue surface used by the
// workflow coordinator handoff loop.
type AWSFreshnessTriggerStore interface {
	// ClaimQueuedTriggers atomically flips up to limit 'queued' rows to
	// 'claimed', stamping a claim_expires_at lease (claimedAt+leaseDuration) so
	// a mid-batch handoff abort or coordinator crash cannot strand the claim
	// forever (#4576).
	ClaimQueuedTriggers(ctx context.Context, owner string, claimedAt time.Time, limit int, leaseDuration time.Duration) ([]freshness.StoredTrigger, error)
	MarkTriggersHandedOff(context.Context, []string, time.Time) error
	MarkTriggersFailed(context.Context, []string, time.Time, string, string) error
	// ReapExpiredTriggerClaims reclaims 'claimed' rows whose claim_expires_at
	// lease has expired back to 'queued', mirroring the workflow_claims
	// expired-lease reclaim pattern (#4464).
	ReapExpiredTriggerClaims(ctx context.Context, asOf time.Time, limit int) ([]freshness.StoredTrigger, error)
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
	leaseDuration := s.awsFreshnessClaimLeaseDuration()
	triggers, err := s.AWSFreshnessTriggers.ClaimQueuedTriggers(ctx, awsFreshnessClaimOwner, observedAt.UTC(), claimLimit, leaseDuration)
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
	// Continue on error rather than returning on the first assignment's
	// failure: a bad plan request or transient per-trigger error must not
	// abandon every remaining claimed batch-mate (#4576). Each failing
	// assignment already durably marks its own triggers failed inside
	// handoffAWSFreshnessAssignment before returning its error, so the
	// triggers are not left stuck at 'claimed' by this path; errors are
	// aggregated and returned once every assignment has been attempted.
	var errs []error
	for _, assignment := range assignments {
		if len(assignment.triggers) == 0 {
			continue
		}
		if err := s.handoffAWSFreshnessAssignment(ctx, observedAt.UTC(), assignment); err != nil {
			if s.Logger != nil {
				s.Logger.Warn(
					"AWS freshness handoff failed for one assignment; continuing with remaining batch-mates",
					"error", err,
					"instance_id", assignment.instance.InstanceID,
					"trigger_count", len(assignment.triggers),
				)
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// awsFreshnessClaimLeaseDuration returns the configured AWS freshness claim
// lease duration, falling back to defaultAWSFreshnessClaimLeaseDuration when
// unset.
func (s Service) awsFreshnessClaimLeaseDuration() time.Duration {
	if s.Config.AWSFreshnessClaimLeaseDuration > 0 {
		return s.Config.AWSFreshnessClaimLeaseDuration
	}
	return defaultAWSFreshnessClaimLeaseDuration
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
	enqueued, err := s.createWorkflowWorkIfNoOpenTargets(ctx, assignment.instance, run, items)
	if err != nil {
		s.markAWSFreshnessFailed(ctx, assignment.triggers, observedAt, "workflow_handoff_failed", err.Error())
		return fmt.Errorf("create AWS freshness workflow run for %q: %w", assignment.instance.InstanceID, err)
	}
	triggerIDs := awsFreshnessTriggerIDs(assignment.triggers)
	if err := s.AWSFreshnessTriggers.MarkTriggersHandedOff(ctx, triggerIDs, observedAt); err != nil {
		return fmt.Errorf("mark AWS freshness triggers handed off: %w", err)
	}
	action := awsFreshnessActionSkipped
	if enqueued > 0 {
		action = awsFreshnessActionCreated
	}
	for _, trigger := range assignment.triggers {
		s.recordAWSFreshnessEvent(ctx, trigger.Kind, action)
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
		// Best-effort: we are already on the failure path, so a failed
		// failure-marking write must not abort reconciliation. It is logged
		// rather than swallowed so an operator can see that the triggers were not
		// durably marked failed (#3793).
		if err := s.AWSFreshnessTriggers.MarkTriggersFailed(ctx, ids, observedAt, failureClass, failureMessage); err != nil && s.Logger != nil {
			s.Logger.Warn(
				"aws-freshness trigger failure marking did not persist",
				"error", err,
				"trigger_count", len(ids),
				"failure_class", failureClass,
			)
		}
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
