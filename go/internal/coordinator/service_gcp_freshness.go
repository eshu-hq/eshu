// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
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
	// defaultGCPFreshnessClaimLeaseDuration mirrors
	// defaultAWSFreshnessClaimLeaseDuration; see that constant's doc comment
	// (#4576).
	defaultGCPFreshnessClaimLeaseDuration = 5 * time.Minute
	gcpFreshnessActionClaimed             = "handoff_claimed"
	gcpFreshnessActionCreated             = "handoff_created"
	gcpFreshnessActionFailed              = "handoff_failed"
	gcpFreshnessActionSkipped             = "handoff_skipped"
	gcpFreshnessActionReclaimed           = "claim_reclaimed"

	// gcpFreshnessAuthPathNotApplicable is the neutral auth_path value the
	// coordinator's handoff loop stamps on eshu_dp_gcp_freshness_events_total.
	// The webhook listener's intake path (go/cmd/webhook-listener) stamps this
	// same counter with the auth mechanism that authenticated the inbound push
	// ("shared_token"/"oidc"/"none"); the coordinator's own handoff loop is a
	// downstream trigger-claim/fan-out step with no request to authenticate,
	// so it is not one of those three values. Every producer of this counter
	// must set auth_path so all series share one bounded label set — a mixed
	// label set (some series with the attribute, some without) breaks
	// Prometheus `sum by (..., auth_path)` aggregation (see issue #4659 review).
	gcpFreshnessAuthPathNotApplicable = "n/a"
)

// GCPFreshnessTriggerStore is the durable trigger queue surface used by the
// workflow coordinator handoff loop. It mirrors AWSFreshnessTriggerStore; the
// concrete Postgres implementation is postgres.GCPFreshnessStore (#4300).
type GCPFreshnessTriggerStore interface {
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
	leaseDuration := s.gcpFreshnessClaimLeaseDuration()
	triggers, err := s.GCPFreshnessTriggers.ClaimQueuedTriggers(ctx, gcpFreshnessClaimOwner, observedAt.UTC(), claimLimit, leaseDuration)
	if err != nil {
		return fmt.Errorf("claim GCP freshness triggers: %w", err)
	}
	for _, trigger := range triggers {
		s.recordGCPFreshnessEvent(ctx, trigger.Kind, gcpFreshnessActionClaimed)
	}
	if len(triggers) == 0 {
		return nil
	}

	assignments, order := groupGCPFreshnessAssignments(triggers, instances)
	for _, trigger := range triggers {
		if _, matched := assignments.matched[trigger.TriggerID]; matched {
			continue
		}
		s.markGCPFreshnessFailed(ctx, []freshness.StoredTrigger{trigger}, observedAt, "unauthorized_target", "no GCP collector instance scope matches the freshness target tuple")
	}
	// Continue on error rather than returning on the first assignment's
	// failure: a bad plan request or transient per-trigger error must not
	// abandon every remaining claimed batch-mate (#4576). Each failing
	// assignment already durably marks its own triggers failed inside
	// handoffGCPFreshnessAssignment before returning its error, so the
	// triggers are not left stuck at 'claimed' by this path; errors are
	// aggregated and returned once every assignment has been attempted.
	var errs []error
	for _, instanceID := range order {
		assignment := *assignments.byInstance[instanceID]
		if err := s.handoffGCPFreshnessAssignment(ctx, observedAt.UTC(), assignment); err != nil {
			if s.Logger != nil {
				s.Logger.Warn(
					"GCP freshness handoff failed for one assignment; continuing with remaining batch-mates",
					"error", err,
					"instance_id", assignment.Instance.InstanceID,
					"trigger_count", len(assignment.Triggers),
				)
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// gcpFreshnessClaimLeaseDuration returns the configured GCP freshness claim
// lease duration, falling back to defaultGCPFreshnessClaimLeaseDuration when
// unset.
func (s Service) gcpFreshnessClaimLeaseDuration() time.Duration {
	if s.Config.GCPFreshnessClaimLeaseDuration > 0 {
		return s.Config.GCPFreshnessClaimLeaseDuration
	}
	return defaultGCPFreshnessClaimLeaseDuration
}

// gcpFreshnessAssignment is every trigger and the deduped, fanned-out scope
// id set that resolved to one GCP collector instance within a single
// reconcile batch. Grouping here — rather than handing off one trigger at a
// time — mirrors the AWS freshness assignment pattern
// (handoffAWSFreshnessAssignment) and is required for correctness: the
// coordinator plans and hands off exactly once per (instance, reconcile
// interval), so multiple triggers resolving to the same instance in the same
// batch must be merged into one PlanGCPWork/createWorkflowWorkIfNoOpenTargets
// call. Planning them independently would compute the identical PlanKey (and
// therefore RunID) for every trigger sharing that instance and interval,
// and the second, independent createWorkflowWorkIfNoOpenTargets call would
// either collide with or short-circuit on the first trigger's now-existing
// run, silently dropping the second trigger's scope ids (#4577).
type gcpFreshnessAssignment struct {
	Instance workflow.CollectorInstance
	ScopeIDs []string
	Triggers []freshness.StoredTrigger
}

// gcpFreshnessAssignmentSet is the working state groupGCPFreshnessAssignments
// builds while walking one claimed batch of triggers.
type gcpFreshnessAssignmentSet struct {
	byInstance map[string]*gcpFreshnessAssignment
	matched    map[string]struct{}
}

// groupGCPFreshnessAssignments resolves every claimed trigger to the fan-out
// set of matching scope ids across every matching enabled, claim-enabled GCP
// collector instance — not only the first match — and groups the result by
// instance so each instance is planned and handed off exactly once for this
// batch. It returns the grouped assignments plus a deterministic instance
// processing order (first-seen order across triggers).
func groupGCPFreshnessAssignments(
	triggers []freshness.StoredTrigger,
	instances []workflow.CollectorInstance,
) (gcpFreshnessAssignmentSet, []string) {
	set := gcpFreshnessAssignmentSet{
		byInstance: make(map[string]*gcpFreshnessAssignment),
		matched:    make(map[string]struct{}),
	}
	var order []string
	for _, trigger := range triggers {
		matches := resolveGCPFreshnessScopeIDs(trigger, instances)
		if len(matches) == 0 {
			continue
		}
		set.matched[trigger.TriggerID] = struct{}{}
		for _, match := range matches {
			instanceID := strings.TrimSpace(match.Instance.InstanceID)
			assignment, ok := set.byInstance[instanceID]
			if !ok {
				assignment = &gcpFreshnessAssignment{Instance: match.Instance}
				set.byInstance[instanceID] = assignment
				order = append(order, instanceID)
			}
			assignment.ScopeIDs = appendUniqueStrings(assignment.ScopeIDs, match.ScopeIDs)
			assignment.Triggers = append(assignment.Triggers, trigger)
		}
	}
	return set, order
}

// appendUniqueStrings appends every value from additions not already present
// in existing, preserving existing's order and additions' relative order.
func appendUniqueStrings(existing []string, additions []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, v := range existing {
		seen[v] = struct{}{}
	}
	for _, v := range additions {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		existing = append(existing, v)
	}
	return existing
}

// handoffGCPFreshnessAssignment plans and hands off one instance's fanned-out
// scope ids for a batch, covering every trigger that resolved to this
// instance.
func (s Service) handoffGCPFreshnessAssignment(
	ctx context.Context,
	observedAt time.Time,
	assignment gcpFreshnessAssignment,
) error {
	s.recordGCPFreshnessFanOut(ctx, len(assignment.ScopeIDs))

	run, items, err := s.GCPPlanner.PlanGCPWork(ctx, GCPPlanRequest{
		Instance:   assignment.Instance,
		ObservedAt: observedAt,
		PlanKey:    s.gcpFreshnessPlanKey(observedAt),
		ScopeIDs:   assignment.ScopeIDs,
	})
	if err != nil {
		s.markGCPFreshnessFailed(ctx, assignment.Triggers, observedAt, "plan_failed", err.Error())
		return fmt.Errorf("plan GCP freshness work for %q: %w", assignment.Instance.InstanceID, err)
	}
	enqueued, err := s.createWorkflowWorkIfNoOpenTargets(ctx, assignment.Instance, run, items)
	if err != nil {
		s.markGCPFreshnessFailed(ctx, assignment.Triggers, observedAt, "workflow_handoff_failed", err.Error())
		return fmt.Errorf("create GCP freshness workflow run for %q: %w", assignment.Instance.InstanceID, err)
	}
	if err := s.GCPFreshnessTriggers.MarkTriggersHandedOff(ctx, gcpFreshnessTriggerIDs(assignment.Triggers), observedAt); err != nil {
		return fmt.Errorf("mark GCP freshness trigger handed off: %w", err)
	}
	action := gcpFreshnessActionSkipped
	if enqueued > 0 {
		action = gcpFreshnessActionCreated
	}
	for _, trigger := range assignment.Triggers {
		s.recordGCPFreshnessEvent(ctx, trigger.Kind, action)
	}
	return nil
}

// gcpFreshnessInstanceMatch is one GCP collector instance that authorizes a
// trigger's target, paired with the scope ids on that instance the trigger
// fans out to.
type gcpFreshnessInstanceMatch struct {
	Instance workflow.CollectorInstance
	ScopeIDs []string
}

// resolveGCPFreshnessScopeIDs returns every enabled, claim-enabled GCP
// collector instance that authorizes the trigger's target — not only the
// first match — each paired with every configured scope id on that instance
// sharing the trigger's (parent_scope_kind, parent_scope_id,
// asset_type_family, location_bucket) tuple, regardless of content_family.
// More than one instance can legitimately authorize the same target tuple
// (for example two GCP collector instances each scoped to a disjoint set of
// content families over the same project/location), and every one of them
// must be scheduled or the content families only that instance covers are
// silently under-scanned (#4577). Returns nil when no instance authorizes
// the target.
func resolveGCPFreshnessScopeIDs(
	trigger freshness.StoredTrigger,
	instances []workflow.CollectorInstance,
) []gcpFreshnessInstanceMatch {
	target := trigger.Target()
	assetFamily := gcpcloud.AssetTypeFamily(target.AssetType)
	locationBucket := gcpcloud.LocationBucket(target.Location)
	var matches []gcpFreshnessInstanceMatch
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
			matches = append(matches, gcpFreshnessInstanceMatch{Instance: instance, ScopeIDs: scopeIDs})
		}
	}
	return matches
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
		telemetry.AttrAuthPath(gcpFreshnessAuthPathNotApplicable),
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
