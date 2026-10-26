// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	cicdActionsClaimOwner = "workflow-coordinator"
	cicdActionsClaimLimit = 100
	cicdActionsClaimLease = time.Minute
)

// CICDRunActionsTriggerStore owns the durable, fenced Actions delivery inbox.
type CICDRunActionsTriggerStore interface {
	Claim(context.Context, string, time.Time, int, time.Duration) ([]webhook.StoredActionsTrigger, error)
	Reap(context.Context, time.Time, int) (webhook.ActionsReapResult, error)
	Ack(context.Context, webhook.StoredActionsTrigger, time.Time) (bool, error)
	Release(context.Context, webhook.StoredActionsTrigger, time.Time, string) (bool, error)
	Fail(context.Context, webhook.StoredActionsTrigger, time.Time, string) (bool, error)
	HasPendingRepository(context.Context, string) (bool, error)
}

type exactCICDWebhookWorkReader interface {
	HasExactCICDWebhookWork(context.Context, workflow.WorkItem) (bool, error)
}

func (s Service) runCICDRunWebhookHandoff(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.CICDRunActionsTriggers == nil {
		return nil
	}
	now := s.now().UTC()
	reaped, err := s.CICDRunActionsTriggers.Reap(ctx, now, cicdActionsClaimLimit)
	if err != nil {
		return fmt.Errorf("reap actions delivery claims: %w", err)
	}
	if s.CICDRunInstruments != nil {
		if reaped.Requeued > 0 {
			s.CICDRunInstruments.CICDRunWebhookReaps.Add(ctx, int64(reaped.Requeued), metric.WithAttributes(attribute.String("outcome", "requeued")))
		}
		if reaped.Exhausted > 0 {
			s.CICDRunInstruments.CICDRunWebhookReaps.Add(ctx, int64(reaped.Exhausted), metric.WithAttributes(attribute.String("outcome", "exhausted")))
		}
	}
	if reaped.Exhausted > 0 && s.Logger != nil {
		s.Logger.Warn("ci/cd actions delivery attempts exhausted during reap", telemetry.LogKeyCICDActionsExhausted, reaped.Exhausted, telemetry.LogKeyCICDActionsRequeued, reaped.Requeued)
	}
	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		return fmt.Errorf("list ci/cd collector instances for actions delivery: %w", err)
	}
	filtered, err := s.filterCollectorInstancesByEgress(ctx, now, instances)
	if err != nil {
		return err
	}
	claims, err := s.CICDRunActionsTriggers.Claim(ctx, cicdActionsClaimOwner, now, cicdActionsClaimLimit, cicdActionsClaimLease)
	if err != nil {
		return fmt.Errorf("claim actions deliveries: %w", err)
	}
	var handoffErrors []error
	groups := make(map[string][]webhook.StoredActionsTrigger)
	keys := make([]string, 0, len(claims))
	for _, claim := range claims {
		if !strings.HasPrefix(claim.HandoffKey, "batch-v1:") || len(claim.HandoffKey) == len("batch-v1:") {
			if err = s.retryCICDWebhook(ctx, claim, "admission_failed", errors.New("actions delivery has no valid handoff key")); err != nil {
				handoffErrors = append(handoffErrors, err)
			}
			continue
		}
		if _, exists := groups[claim.HandoffKey]; !exists {
			keys = append(keys, claim.HandoffKey)
		}
		groups[claim.HandoffKey] = append(groups[claim.HandoffKey], claim)
	}
	for _, key := range keys {
		if err = s.handoffCICDRunWebhookGroup(ctx, now, groups[key], instances, filtered); err != nil {
			handoffErrors = append(handoffErrors, err)
		}
	}
	return errors.Join(handoffErrors...)
}

func (s Service) handoffCICDRunWebhookGroup(ctx context.Context, now time.Time, claims []webhook.StoredActionsTrigger, all, active []workflow.CollectorInstance) error {
	claim := claims[0]
	for _, other := range claims[1:] {
		if other.Repository != claim.Repository || other.RepositoryID != claim.RepositoryID {
			return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", errors.New("actions handoff key spans repositories"))
		}
	}
	configured, err := cicdWebhookAssignments(all, claim.Repository)
	if err != nil {
		return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", err)
	}
	if len(configured) == 0 {
		return s.failCICDWebhookGroup(ctx, claims, "unauthorized_target")
	}
	activeAssignments, err := cicdWebhookAssignments(active, claim.Repository)
	if err != nil {
		return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", err)
	}
	if len(activeAssignments) != len(configured) {
		return s.retryCICDWebhookGroup(ctx, claims, "no_active_instance", nil)
	}
	if s.CICDRunPlanner == nil {
		return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", errors.New("ci/cd run planner unavailable"))
	}
	reader, ok := s.Store.(exactCICDWebhookWorkReader)
	if !ok {
		return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", errors.New("exact webhook work reader unavailable"))
	}
	newlyAdmitted := false
	for _, assignment := range activeAssignments {
		for _, scopeID := range assignment.scopes {
			run, items, planErr := s.CICDRunPlanner.PlanCICDRunWork(ctx, cicdrun.PlanRequest{
				Instance: assignment.instance, ObservedAt: now, PlanKey: cicdWebhookGroupPlanKey(claim.HandoffKey, scopeID, s.Config.TenantBoundary), TriggerKind: workflow.TriggerKindWebhook, ScopeIDs: []string{scopeID},
			})
			if planErr != nil {
				return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", planErr)
			}
			if len(items) != 1 {
				return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", errors.New("ci/cd webhook scope did not plan exactly one target"))
			}
			authorized, denied, authErr := s.authorizeWorkflowWorkItems(ctx, run, items)
			if authErr != nil {
				return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", authErr)
			}
			if denied > 0 || len(authorized) != 1 {
				return s.failCICDWebhookGroup(ctx, claims, "unauthorized_target")
			}
			admission, admitErr := s.Store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, authorized)
			if admission.InsertedWorkItems > 0 {
				newlyAdmitted = true
			}
			if admitErr != nil {
				return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", admitErr)
			}
			// An unrelated open target cannot cover this group. A terminal run
			// can still contain exact work from a prior partial admission.
			exists, checkErr := reader.HasExactCICDWebhookWork(ctx, authorized[0])
			if checkErr != nil {
				return s.retryCICDWebhookGroup(ctx, claims, "admission_failed", checkErr)
			}
			if !exists {
				return s.retryCICDWebhookGroup(ctx, claims, "blocked", nil)
			}
		}
	}
	outcome := "recovered"
	if newlyAdmitted {
		outcome = "admitted"
	}
	var ackErrors []error
	for _, claim := range claims {
		finishedAt := s.now().UTC()
		acked, ackErr := s.CICDRunActionsTriggers.Ack(ctx, claim, finishedAt)
		if ackErr != nil {
			ackErrors = append(ackErrors, fmt.Errorf("ack actions delivery %q: %w", claim.DeliveryID, ackErr))
			continue
		}
		if !acked {
			ackErrors = append(ackErrors, fmt.Errorf("actions delivery %q claim expired before ACK", claim.DeliveryID))
			continue
		}
		s.recordCICDWebhookOutcome(ctx, outcome)
		if s.CICDRunInstruments != nil {
			s.CICDRunInstruments.CICDRunWebhookLag.Record(ctx, math.Max(0, finishedAt.Sub(claim.ReceivedAt).Seconds()))
		}
	}
	return errors.Join(ackErrors...)
}

type cicdWebhookAssignment struct {
	instance workflow.CollectorInstance
	scopes   []string
}

func cicdWebhookAssignments(instances []workflow.CollectorInstance, repository string) ([]cicdWebhookAssignment, error) {
	output := make([]cicdWebhookAssignment, 0)
	for _, instance := range instances {
		if !shouldScheduleCICDRun(instance) {
			continue
		}
		scopes, err := cicdrun.ConfiguredGitHubScopes(instance.Configuration, repository)
		if err != nil {
			return nil, fmt.Errorf("read ci/cd webhook target configuration: %w", err)
		}
		if len(scopes) > 0 {
			output = append(output, cicdWebhookAssignment{instance: instance, scopes: scopes})
		}
	}
	return output, nil
}

func cicdWebhookGroupPlanKey(handoffKey, scopeID string, boundary WorkflowTenantBoundary) string {
	boundary = boundary.normalize()
	var input strings.Builder
	for _, part := range [...]string{handoffKey, scopeID, boundary.TenantID, boundary.WorkspaceID, boundary.SubjectClass, boundary.PolicyRevisionHash} {
		input.WriteString(strconv.Itoa(len(part)))
		input.WriteByte(':')
		input.WriteString(part)
	}
	digest := sha256.Sum256([]byte(input.String()))
	return "webhook-group-v1-" + hex.EncodeToString(digest[:])
}

func (s Service) retryCICDWebhookGroup(ctx context.Context, claims []webhook.StoredActionsTrigger, class string, cause error) error {
	var completionErrors []error
	for _, claim := range claims {
		if err := s.retryCICDWebhook(ctx, claim, class, nil); err != nil {
			completionErrors = append(completionErrors, err)
		}
	}
	return errors.Join(append([]error{cause}, completionErrors...)...)
}

func (s Service) failCICDWebhookGroup(ctx context.Context, claims []webhook.StoredActionsTrigger, class string) error {
	var completionErrors []error
	for _, claim := range claims {
		if err := s.failCICDWebhook(ctx, claim, class); err != nil {
			completionErrors = append(completionErrors, err)
		}
	}
	return errors.Join(completionErrors...)
}

func (s Service) retryCICDWebhook(ctx context.Context, claim webhook.StoredActionsTrigger, class string, cause error) error {
	updated, err := s.CICDRunActionsTriggers.Release(ctx, claim, s.now().UTC(), class)
	if err != nil {
		return fmt.Errorf("release actions delivery: %w", err)
	}
	if !updated {
		return fmt.Errorf("actions delivery claim expired before release")
	}
	s.recordCICDWebhookOutcome(ctx, classToOutcome(class))
	if cause != nil {
		return fmt.Errorf("ci/cd webhook handoff (%s): %w", class, cause)
	}
	return nil
}

func (s Service) failCICDWebhook(ctx context.Context, claim webhook.StoredActionsTrigger, class string) error {
	updated, err := s.CICDRunActionsTriggers.Fail(ctx, claim, s.now().UTC(), class)
	if err != nil {
		return fmt.Errorf("fail actions delivery: %w", err)
	}
	if !updated {
		return fmt.Errorf("actions delivery claim expired before failure mark")
	}
	s.recordCICDWebhookOutcome(ctx, "unauthorized")
	return nil
}

func classToOutcome(class string) string {
	if class == "blocked" || class == "no_active_instance" {
		return "blocked"
	}
	return "failed"
}

func (s Service) recordCICDWebhookOutcome(ctx context.Context, outcome string) {
	if s.CICDRunInstruments != nil {
		s.CICDRunInstruments.CICDRunWebhookHandoffs.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}
