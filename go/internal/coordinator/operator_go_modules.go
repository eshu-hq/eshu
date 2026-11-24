// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/contract"
	"github.com/eshu-hq/eshu/go/internal/coordinator/reachability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// OperatorPlanOutcome is the closed outcome set of an out-of-rotation Go
// reachability plan (eshu-hq/eshu#10002, arbiter ruling B.3). Business outcomes
// are not errors: the caller maps planned, already_open, and dry_run to a
// zero exit and not_eligible to a non-zero one.
type OperatorPlanOutcome string

const (
	// OperatorPlanPlanned reports one repository planned and admitted: the run
	// row and its single work item exist.
	OperatorPlanPlanned OperatorPlanOutcome = "planned"
	// OperatorPlanAlreadyOpen reports the fixture scope already holds an open
	// item, so nothing was created: either a scheduled run planned it first,
	// or this request id already fired. The run id names the existing run.
	OperatorPlanAlreadyOpen OperatorPlanOutcome = "already_open"
	// OperatorPlanNotEligible reports the repository would never enter the
	// rotation page: unknown id, no active generation, no go.mod and no Go
	// evidence, or the planner's own skip reason.
	OperatorPlanNotEligible OperatorPlanOutcome = "not_eligible"
	// OperatorPlanDryRun reports what a real fire would do without writing
	// anything: no fence write, no run, no items.
	OperatorPlanDryRun OperatorPlanOutcome = "dry_run"
)

// OperatorPlanRequest is one out-of-rotation Go reachability plan for one
// repository. Every field but DryRun and ObservedAt is required: the command
// refuses a flag it does not have rather than reading a hand-built instance
// or minting an identity from the clock.
type OperatorPlanRequest struct {
	InstanceID   string
	RepositoryID string
	RequestID    string
	RequestedBy  string
	Reason       string
	DryRun       bool
	ObservedAt   time.Time
}

// OperatorPlanResult reports one operator plan attempt. Reason carries the
// not_eligible skip, the already_open holder, or the dry-run verdict;
// WouldPlan says whether Plan yields items a real fire could admit
// (plannable). Dry-run never consults open-target admission: a true
// WouldPlan with an already-open run still reports plannable on re-fire
// (the re-fire itself is the exact no-op), it does not promise admission.
type OperatorPlanResult struct {
	Outcome   OperatorPlanOutcome
	PlanKey   string
	RunID     string
	ScopeID   string
	CommitSHA string
	WouldPlan bool
	Reason    string
}

// GoModuleSingleRepositoryReader reads one repository exactly as the rotation
// page read would return it. The gomodules store satisfies it; the operator
// path never scans a page.
type GoModuleSingleRepositoryReader interface {
	GetGoModuleRepository(context.Context, string) (gomodules.GoModuleRepositoryRead, error)
}

// operatorPlanKeyPrefix keeps operator runs in their own identity space: no
// scheduled key ever carries it, so a one-repo fire can never suppress a
// rotation page (ruling 3a), plan a bucket early (3b), or rewrite generation
// identity (3c).
const operatorPlanKeyPrefix = "go-modules-operator-"

// PlanGoReachabilityScopeNow plans one repository out of rotation through the
// real reachability.Plan and the real open-target admission, under a distinct
// operator plan key and the operator_recovery trigger. It is a one-shot: it
// reads and writes exactly this attempt and never starts the reconciler,
// reaper, or any loop.
//
// The instance resolves from the durable collector-instance set with the
// identical gates the scheduled path applies: enabled and claims-enabled, the
// derive_from_go_modules section enabled with a matching host, and a valid
// scan interval. Zero or multiple matches refuse. The repository resolves
// through the single-repo reader with rotation-identical eligibility, and the
// commit-skew fence runs before admission with the same fail-the-attempt
// semantics as the scheduled path. A dry run evaluates eligibility and the
// fence decision and writes nothing. A re-fire under the same request id is
// an exact no-op: the existing run id probes planned first, so neither the
// fence nor admission runs twice.
func (s Service) PlanGoReachabilityScopeNow(
	ctx context.Context,
	request OperatorPlanRequest,
) (OperatorPlanResult, error) {
	if s.Store == nil {
		return OperatorPlanResult{}, errors.New("plan go reachability scope now: workflow control store is required")
	}
	observedAt := request.ObservedAt
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	instanceID := strings.TrimSpace(request.InstanceID)
	repositoryID := strings.TrimSpace(request.RepositoryID)
	requestID := strings.TrimSpace(request.RequestID)
	requestedBy := strings.TrimSpace(request.RequestedBy)
	reason := strings.TrimSpace(request.Reason)
	planKey := operatorPlanKeyPrefix + requestID
	scopeID, _ := scannercomponent.GoReachabilityScopeID(repositoryID)
	report := func(outcome OperatorPlanOutcome, wouldPlan bool, outcomeReason string) OperatorPlanResult {
		return OperatorPlanResult{
			Outcome:   outcome,
			PlanKey:   planKey,
			RunID:     reachability.RunIDWithTrigger(workflow.CollectorInstance{InstanceID: instanceID}, planKey, workflow.TriggerKindOperatorRecovery),
			ScopeID:   scopeID,
			Reason:    outcomeReason,
			WouldPlan: wouldPlan,
		}
	}

	if instanceID == "" || repositoryID == "" || requestID == "" || requestedBy == "" || reason == "" {
		s.logOperatorPlanOutcome(planKey, "", scopeID, "", "refused", requestedBy, "missing required flag", request.DryRun)
		return OperatorPlanResult{}, fmt.Errorf("plan go reachability scope now: instance-id, repository-id, request-id, requested-by, and reason are all required")
	}
	if err := contract.ValidateSafePlanKey("go modules operator", planKey); err != nil {
		s.logOperatorPlanOutcome(planKey, "", scopeID, "", "refused", requestedBy, err.Error(), request.DryRun)
		return OperatorPlanResult{}, fmt.Errorf("plan go reachability scope now: %w", err)
	}
	if isTimestampOperatorRequestID(requestID) {
		s.logOperatorPlanOutcome(planKey, "", scopeID, "", "refused", requestedBy, "timestamp-only request-id is not an idempotency token", request.DryRun)
		return OperatorPlanResult{}, fmt.Errorf(
			"plan go reachability scope now: request-id %q is timestamp-only, retry would mint a second identity", requestID)
	}
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		s.logOperatorPlanOutcome(planKey, "", scopeID, "", "refused", requestedBy, "coordinator scheduling is not active", request.DryRun)
		return OperatorPlanResult{}, fmt.Errorf("plan go reachability scope now: coordinator scheduling is not active")
	}

	instance, err := s.resolveOperatorPlanInstance(ctx, instanceID)
	if err != nil {
		s.logOperatorPlanOutcome(planKey, "", scopeID, "", "refused", requestedBy, err.Error(), request.DryRun)
		return OperatorPlanResult{}, err
	}
	derivation, err := s.operatorPlanDerivation(instance)
	if err != nil {
		s.logOperatorPlanOutcome(planKey, "", scopeID, "", "refused", requestedBy, err.Error(), request.DryRun)
		return OperatorPlanResult{}, err
	}
	interval := derivation.Interval()

	runID := reachability.RunIDWithTrigger(instance, planKey, workflow.TriggerKindOperatorRecovery)
	if s.GoModuleRepositoryReader == nil {
		return OperatorPlanResult{}, fmt.Errorf("plan go reachability scope now for %q: go module repository reader is required", instanceID)
	}
	if s.GoModuleSingleRepositoryReader == nil {
		return OperatorPlanResult{}, fmt.Errorf("plan go reachability scope now for %q: go module single repository reader is required", instanceID)
	}
	probe, err := s.GoModuleRepositoryReader.ListGoModuleRepositories(ctx, scannercomponent.GoModuleRepositoryFilter{
		RunID:    runID,
		PageSize: scannercomponent.GoModulePageSize,
	})
	if err != nil {
		return OperatorPlanResult{}, fmt.Errorf("probe go reachability operator run %q: %w", runID, err)
	}
	if probe.AlreadyPlanned {
		result := report(OperatorPlanAlreadyOpen, false, "request_id_already_planned")
		result.RunID = runID
		s.logOperatorPlanOutcome(planKey, runID, scopeID, "", string(result.Outcome), requestedBy, result.Reason, request.DryRun)
		return result, nil
	}

	read, err := s.GoModuleSingleRepositoryReader.GetGoModuleRepository(ctx, repositoryID)
	switch {
	case errors.Is(err, gomodules.ErrGoModuleRepositoryNotFound):
		result := report(OperatorPlanNotEligible, false, "repository_not_found")
		s.logOperatorPlanOutcome(planKey, runID, scopeID, "", string(result.Outcome), requestedBy, result.Reason, request.DryRun)
		return result, nil
	case errors.Is(err, gomodules.ErrGoModuleRepositoryNoActiveGeneration):
		result := report(OperatorPlanNotEligible, false, "no_active_generation")
		s.logOperatorPlanOutcome(planKey, runID, scopeID, "", string(result.Outcome), requestedBy, result.Reason, request.DryRun)
		return result, nil
	case err != nil:
		return OperatorPlanResult{}, fmt.Errorf("read go module repository %q: %w", repositoryID, err)
	}
	repository := read.Repository
	if !read.HasGoModule && !repository.HasGoEvidence {
		result := report(OperatorPlanNotEligible, false, "no_go_module_or_evidence")
		s.logOperatorPlanOutcome(planKey, runID, scopeID, shortCommit(repository.SourceCommitSHA), string(result.Outcome), requestedBy, result.Reason, request.DryRun)
		return result, nil
	}

	_, slotCount := scannercomponent.GoModulePageWindow(0, read.TotalCandidates, scannercomponent.GoModulePageSize)
	fence := reachability.StaleEvidence([]scannercomponent.GoModuleRepository{repository}, observedAt, interval, slotCount)

	run, items, skipped, err := reachability.Plan(reachability.PlanRequest{
		Instance: instance, ObservedAt: observedAt, PlanKey: planKey,
		TriggerKind:  workflow.TriggerKindOperatorRecovery,
		Repositories: []scannercomponent.GoModuleRepository{repository},
	})
	if err != nil {
		return OperatorPlanResult{}, fmt.Errorf("plan go reachability scope now for %q: %w", repositoryID, err)
	}
	if len(items) == 0 {
		skipReason := firstSkipReason(skipped)
		result := report(OperatorPlanNotEligible, false, skipReason)
		result.CommitSHA = repository.SourceCommitSHA
		s.logOperatorPlanOutcome(planKey, runID, scopeID, shortCommit(repository.SourceCommitSHA), string(result.Outcome), requestedBy, skipReason, request.DryRun)
		return result, nil
	}

	if request.DryRun {
		result := report(OperatorPlanDryRun, true, "plannable")
		result.RunID = runID
		result.CommitSHA = repository.SourceCommitSHA
		s.logOperatorPlanOutcome(planKey, runID, scopeID, shortCommit(repository.SourceCommitSHA), string(result.Outcome), requestedBy, result.Reason, true)
		return result, nil
	}

	if s.GoEvidenceFencer == nil {
		return OperatorPlanResult{}, fmt.Errorf("plan go reachability scope now for %q: go evidence fencer is required", instanceID)
	}
	if _, err := s.fenceStaleGoEvidence(ctx, observedAt, instance, fence); err != nil {
		return OperatorPlanResult{}, fmt.Errorf("fence stale go evidence for %q: %w", instanceID, err)
	}

	s.recordActivationDerivedScopes(ctx, instance, items)
	enqueued, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items)
	if err != nil {
		return OperatorPlanResult{}, fmt.Errorf("create go reachability operator scoped work for %q: %w", instanceID, err)
	}
	if enqueued == 0 {
		result := report(OperatorPlanAlreadyOpen, false, "target_already_open")
		result.RunID = runID
		result.CommitSHA = repository.SourceCommitSHA
		s.logOperatorPlanOutcome(planKey, runID, scopeID, shortCommit(repository.SourceCommitSHA), string(result.Outcome), requestedBy, result.Reason, false)
		return result, nil
	}
	result := report(OperatorPlanPlanned, true, "planned")
	result.RunID = runID
	result.CommitSHA = repository.SourceCommitSHA
	s.logOperatorPlanOutcome(planKey, runID, scopeID, shortCommit(repository.SourceCommitSHA), string(result.Outcome), requestedBy, result.Reason, false)
	return result, nil
}

// resolveOperatorPlanInstance resolves one durable collector instance by id.
// It never reads a hand-built instance: zero or multiple matches refuse, and
// the scheduled path's enabled and claims-enabled gates apply unchanged.
func (s Service) resolveOperatorPlanInstance(
	ctx context.Context,
	instanceID string,
) (workflow.CollectorInstance, error) {
	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		return workflow.CollectorInstance{}, fmt.Errorf("list collector instances for operator plan: %w", err)
	}
	var matches []workflow.CollectorInstance
	for _, instance := range instances {
		if strings.TrimSpace(instance.InstanceID) == instanceID {
			matches = append(matches, instance)
		}
	}
	if len(matches) == 0 {
		return workflow.CollectorInstance{}, fmt.Errorf(
			"plan go reachability scope now: instance %q matches no durable collector instance", instanceID)
	}
	if len(matches) > 1 {
		return workflow.CollectorInstance{}, fmt.Errorf(
			"plan go reachability scope now: instance %q matches %d durable collector instances", instanceID, len(matches))
	}
	instance := matches[0]
	if !instance.Enabled || !instance.ClaimsEnabled {
		return workflow.CollectorInstance{}, fmt.Errorf(
			"plan go reachability scope now: instance %q is not enabled and claims-enabled", instanceID)
	}
	return instance, nil
}

// operatorPlanDerivation applies the scheduled path's derivation gates to an
// operator plan: the activation document must parse, the derive block must be
// enabled with a matching host, and the interval must fit the reconcile tick.
func (s Service) operatorPlanDerivation(instance workflow.CollectorInstance) (reachability.Derivation, error) {
	config, ok, err := activation.ParseConfig(instance.Configuration)
	if err != nil || !ok {
		return reachability.Derivation{}, fmt.Errorf(
			"plan go reachability scope now: instance %q carries no activation configuration", instance.InstanceID)
	}
	derivation, err := checkGoModuleDerivation(instance.CollectorKind, config)
	if err != nil {
		return reachability.Derivation{}, fmt.Errorf("plan go reachability scope now for %q: %w", instance.InstanceID, err)
	}
	if !derivation.Enabled() {
		return reachability.Derivation{}, fmt.Errorf(
			"plan go reachability scope now: instance %q does not enable derive_from_go_modules", instance.InstanceID)
	}
	if err := validateGoModuleInterval(derivation, s.Config.ReconcileInterval); err != nil {
		return reachability.Derivation{}, fmt.Errorf("plan go reachability scope now for %q: %w", instance.InstanceID, err)
	}
	return derivation, nil
}

// firstSkipReason names the planner's verdict in a fixed order so the
// not_eligible reason is deterministic when several skips coincide.
func firstSkipReason(skipped map[string]int) string {
	for _, reason := range []string{
		reachability.SkipInvalidRepositoryID,
		reachability.SkipDuplicateRepository,
		reachability.SkipUnsupportedRemote,
		reachability.SkipUnsupportedRemoteWithEvidence,
		reachability.SkipMissingCommit,
	} {
		if skipped[reason] > 0 {
			return reason
		}
	}
	return "not_plannable"
}

// isTimestampOperatorRequestID reports request ids that carry no identity
// beyond a clock reading: a retry would mint a second key, so they are
// refused. Anything with a letter outside the T/Z clock markers is a real
// token (for example 10032-fixture-20261123).
func isTimestampOperatorRequestID(requestID string) bool {
	if requestID == "" {
		return false
	}
	for _, char := range requestID {
		switch {
		case char >= '0' && char <= '9':
			// A clock digit.
		case char == 'T' || char == 'Z' || char == '-' || char == '_' || char == '.' || char == ':':
			// A clock separator.
		default:
			return false
		}
	}
	return true
}

// shortCommit names the planned commit the way the scheduled log line does:
// twelve hex chars when there is a full SHA, the value itself otherwise, and
// nothing when the repository has none yet.
func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

// logOperatorPlanOutcome writes the one structured audit line every operator
// attempt owes: the plan key, run id, scope id, and short commit that locate
// the act, the outcome word, who asked, why, and whether it was a dry run.
// It mirrors the field discipline of the scheduled "planned derived scopes"
// line, with log-safe values only: no remote URL, no requested_scope_set.
func (s Service) logOperatorPlanOutcome(
	planKey, runID, scopeID, commit, outcome, requestedBy, reason string,
	dryRun bool,
) {
	if s.Logger == nil {
		return
	}
	s.Logger.Info(
		"workflow coordinator planned go reachability scope for operator request",
		"collector_kind", scope.CollectorScannerWorker,
		"plan_key", planKey,
		"run_id", runID,
		"scope_id", scopeID,
		"commit", commit,
		"outcome", outcome,
		"trigger_kind", workflow.TriggerKindOperatorRecovery,
		"requested_by", requestedBy,
		"reason", reason,
		"dry_run", dryRun,
	)
}
