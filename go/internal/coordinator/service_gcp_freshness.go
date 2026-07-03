// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"go.opentelemetry.io/otel/metric"
)

const (
	gcpFreshnessClaimOwner        = "workflow-coordinator"
	defaultGCPFreshnessClaimLimit = 100
	gcpFreshnessActionClaimed     = "handoff_claimed"
	gcpFreshnessActionCreated     = "handoff_created"
	gcpFreshnessActionFailed      = "handoff_failed"
	gcpFreshnessActionSkipped     = "handoff_skipped"
)

// GCPFreshnessTriggerStore is the durable trigger queue surface used by the
// workflow coordinator handoff loop. It mirrors AWSFreshnessTriggerStore; the
// concrete Postgres implementation is postgres.GCPFreshnessStore (#4300).
type GCPFreshnessTriggerStore interface {
	ClaimQueuedTriggers(context.Context, string, time.Time, int) ([]freshness.StoredTrigger, error)
	MarkTriggersHandedOff(context.Context, []string, time.Time) error
	MarkTriggersFailed(context.Context, []string, time.Time, string, string) error
}

type gcpFreshnessEventCounter interface {
	Add(context.Context, int64, ...metric.AddOption)
}

// gcpFreshnessFanOutRecorder records the number of scopes one GCP freshness
// trigger fanned out to. A GCP CAI asset-change event carries no
// content_family signal (see Trigger/Target in
// go/internal/collector/gcpcloud/freshness/types.go), so one trigger
// legitimately resolves to more than one configured scope; this histogram
// gives an operator the fan-out cardinality distribution at 3 AM.
type gcpFreshnessFanOutRecorder interface {
	Record(context.Context, int64, ...metric.RecordOption)
}

// scheduleGCPFreshnessWork claims queued GCP freshness triggers and hands
// each off to every configured scope that matches the trigger's
// (parent_scope_kind, parent_scope_id, asset_type_family, location_bucket)
// tuple.
//
// Fan-out, not guess: CAI asset-change events carry no content_family
// signal (freshness.Trigger has Kind/ParentScopeKind/ParentScopeID/AssetType/
// Location only). Guessing a single content_family would silently
// under-scan every other tracked content family sharing that tuple — a
// direct accuracy violation under this repo's accuracy-first life motto.
// Over-scanning costs extra compute; under-scanning costs correctness, so
// fan-out to every matching scope is the safe default (#4338).
func (s Service) scheduleGCPFreshnessWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.GCPFreshnessTriggers == nil {
		return nil
	}
	if s.GCPPlanner == nil {
		return fmt.Errorf("GCP planner is required before claiming GCP freshness triggers")
	}
	claimLimit := defaultGCPFreshnessClaimLimit
	triggers, err := s.GCPFreshnessTriggers.ClaimQueuedTriggers(ctx, gcpFreshnessClaimOwner, observedAt.UTC(), claimLimit)
	if err != nil {
		return fmt.Errorf("claim GCP freshness triggers: %w", err)
	}
	for _, trigger := range triggers {
		s.recordGCPFreshnessEvent(ctx, trigger.Kind, gcpFreshnessActionClaimed)
	}
	if len(triggers) == 0 {
		return nil
	}
	for _, trigger := range triggers {
		if err := s.handoffGCPFreshnessTrigger(ctx, observedAt.UTC(), trigger, instances); err != nil {
			return err
		}
	}
	return nil
}

// handoffGCPFreshnessTrigger resolves one claimed trigger to the fan-out set
// of matching scope ids across every enabled, claim-enabled GCP collector
// instance, then plans one work item per resolved scope.
func (s Service) handoffGCPFreshnessTrigger(
	ctx context.Context,
	observedAt time.Time,
	trigger freshness.StoredTrigger,
	instances []workflow.CollectorInstance,
) error {
	instance, scopeIDs, err := resolveGCPFreshnessScopeIDs(trigger, instances)
	if err != nil {
		s.markGCPFreshnessFailed(ctx, []freshness.StoredTrigger{trigger}, observedAt, "plan_failed", err.Error())
		return fmt.Errorf("resolve GCP freshness scopes for trigger %q: %w", trigger.TriggerID, err)
	}
	if len(scopeIDs) == 0 {
		s.markGCPFreshnessFailed(ctx, []freshness.StoredTrigger{trigger}, observedAt, "unauthorized_target", "no GCP collector instance scope matches the freshness target tuple")
		return nil
	}
	s.recordGCPFreshnessFanOut(ctx, len(scopeIDs))

	run, items, err := s.GCPPlanner.PlanGCPWork(ctx, GCPPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    s.gcpFreshnessPlanKey(observedAt),
		ScopeIDs:   scopeIDs,
	})
	if err != nil {
		s.markGCPFreshnessFailed(ctx, []freshness.StoredTrigger{trigger}, observedAt, "plan_failed", err.Error())
		return fmt.Errorf("plan GCP freshness work for %q: %w", instance.InstanceID, err)
	}
	enqueued, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items)
	if err != nil {
		s.markGCPFreshnessFailed(ctx, []freshness.StoredTrigger{trigger}, observedAt, "workflow_handoff_failed", err.Error())
		return fmt.Errorf("create GCP freshness workflow run for %q: %w", instance.InstanceID, err)
	}
	if err := s.GCPFreshnessTriggers.MarkTriggersHandedOff(ctx, []string{trigger.TriggerID}, observedAt); err != nil {
		return fmt.Errorf("mark GCP freshness trigger handed off: %w", err)
	}
	action := gcpFreshnessActionSkipped
	if enqueued > 0 {
		action = gcpFreshnessActionCreated
	}
	s.recordGCPFreshnessEvent(ctx, trigger.Kind, action)
	return nil
}

// resolveGCPFreshnessScopeIDs finds the first enabled, claim-enabled GCP
// collector instance that authorizes the trigger's target and returns every
// configured scope id sharing the trigger's (parent_scope_kind,
// parent_scope_id, asset_type_family, location_bucket) tuple, regardless of
// content_family. Returns a zero-value instance and an empty slice when no
// instance authorizes the target.
func resolveGCPFreshnessScopeIDs(
	trigger freshness.StoredTrigger,
	instances []workflow.CollectorInstance,
) (workflow.CollectorInstance, []string, error) {
	target := trigger.Target()
	assetFamily := gcpcloud.AssetTypeFamily(target.AssetType)
	locationBucket := gcpcloud.LocationBucket(target.Location)
	for _, instance := range instances {
		if !shouldScheduleGCPFreshness(instance) {
			continue
		}
		config, err := parseGCPRuntimeConfiguration(instance.Configuration)
		if err != nil {
			continue
		}
		scopes, err := gcpEnabledScopes(config)
		if err != nil {
			continue
		}
		scopeIDs := matchingGCPFreshnessScopeIDs(scopes, target.ParentScopeKind, target.ParentScopeID, assetFamily, locationBucket)
		if len(scopeIDs) > 0 {
			return instance, scopeIDs, nil
		}
	}
	return workflow.CollectorInstance{}, nil, nil
}

func shouldScheduleGCPFreshness(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorGCP &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

// matchingGCPFreshnessScopeIDs returns every configured scope id whose
// (parent_scope_kind, parent_scope_id, asset_type_family, location_bucket)
// matches the trigger target, ignoring content_family. This is the fan-out
// axis: a CAI asset-change event has no content_family signal, so every
// content family sharing the tuple must be scheduled (#4338).
func matchingGCPFreshnessScopeIDs(
	scopes []gcpScopeConfiguration,
	parentScopeKind gcpcloud.ParentScopeKind,
	parentScopeID string,
	assetFamily string,
	locationBucket string,
) []string {
	var scopeIDs []string
	for _, candidate := range scopes {
		if candidate.ParentScopeKind != string(parentScopeKind) {
			continue
		}
		if candidate.ParentScopeID != parentScopeID {
			continue
		}
		if candidate.AssetTypeFamily != assetFamily {
			continue
		}
		if candidate.LocationBucket != locationBucket {
			continue
		}
		scopeIDs = append(scopeIDs, candidate.ScopeID)
	}
	return scopeIDs
}

func (s Service) markGCPFreshnessFailed(
	ctx context.Context,
	triggers []freshness.StoredTrigger,
	observedAt time.Time,
	failureClass string,
	failureMessage string,
) {
	ids := gcpFreshnessTriggerIDs(triggers)
	if len(ids) > 0 {
		// Best-effort: we are already on the failure path, so a failed
		// failure-marking write must not abort reconciliation. It is logged
		// rather than swallowed so an operator can see that the triggers were not
		// durably marked failed (mirrors AWS freshness #3793).
		if err := s.GCPFreshnessTriggers.MarkTriggersFailed(ctx, ids, observedAt, failureClass, failureMessage); err != nil && s.Logger != nil {
			s.Logger.Warn(
				"gcp-freshness trigger failure marking did not persist",
				"error", err,
				"trigger_count", len(ids),
				"failure_class", failureClass,
			)
		}
	}
	for _, trigger := range triggers {
		s.recordGCPFreshnessEvent(ctx, trigger.Kind, gcpFreshnessActionFailed)
	}
}

func (s Service) gcpFreshnessPlanKey(observedAt time.Time) string {
	interval := s.Config.ReconcileInterval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	return "freshness-" + observedAt.UTC().Truncate(interval).Format("20060102T150405Z")
}

func (s Service) recordGCPFreshnessEvent(ctx context.Context, kind freshness.EventKind, action string) {
	if s.GCPFreshnessEvents == nil {
		return
	}
	kindValue := strings.TrimSpace(string(kind))
	if kindValue == "" {
		kindValue = "unknown"
	}
	s.GCPFreshnessEvents.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrKind(kindValue),
		telemetry.AttrAction(action),
	))
}

func (s Service) recordGCPFreshnessFanOut(ctx context.Context, resolvedScopeCount int) {
	if s.GCPFreshnessFanOut == nil {
		return
	}
	s.GCPFreshnessFanOut.Record(ctx, int64(resolvedScopeCount))
}

func gcpFreshnessTriggerIDs(triggers []freshness.StoredTrigger) []string {
	ids := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		id := strings.TrimSpace(trigger.TriggerID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
