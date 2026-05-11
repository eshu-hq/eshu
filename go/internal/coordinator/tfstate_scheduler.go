package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TerraformStatePlanRequest carries one collector instance planning request.
type TerraformStatePlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// TerraformStateWorkPlanner plans workflow rows for exact Terraform-state
// candidates without opening the state source.
//
// BackendFacts returns candidates from both Terraform backend blocks and
// Terragrunt remote_state blocks. The Terragrunt indirection is resolved by
// the storage adapter into the underlying s3 or local backend kind, so this
// planner never observes BackendTerragrunt and does not need a separate
// scheduler shape to fan out Terragrunt sources.
type TerraformStateWorkPlanner struct {
	GitReadiness terraformstate.GitReadinessChecker
	BackendFacts terraformstate.BackendFactReader
}

// PlanTerraformStateWork resolves exact Terraform-state candidates and returns
// the workflow run plus candidate-scoped work items to enqueue.
func (p TerraformStateWorkPlanner) PlanTerraformStateWork(
	ctx context.Context,
	request TerraformStatePlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateTerraformStatePlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	if err := workflow.ValidateTerraformStateCollectorConfiguration(request.Instance.Configuration); err != nil {
		return workflow.Run{}, nil, err
	}
	discoveryConfig, err := terraformstate.ParseDiscoveryConfig(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	candidates, err := (terraformstate.DiscoveryResolver{
		Config:       discoveryConfig,
		GitReadiness: p.GitReadiness,
		BackendFacts: p.BackendFacts,
	}).Resolve(ctx)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if len(candidates) == 0 {
		return workflow.Run{}, nil, nil
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              terraformStateRunID(request.Instance, request.PlanKey),
		TriggerKind:        terraformStateTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  terraformStateRequestedScopeSet(request.Instance, candidates),
		RequestedCollector: string(scope.CollectorTerraformState),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(candidates))
	for _, candidate := range candidates {
		item, err := terraformStateWorkItem(request.Instance, candidate, run.RunID, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateTerraformStatePlanRequest(request TerraformStatePlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("terraform state plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorTerraformState {
		return fmt.Errorf("terraform state planner requires collector_kind %q", scope.CollectorTerraformState)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("terraform state planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("terraform state planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("terraform state planner observed_at must not be zero")
	}
	if err := validateTerraformStatePlanKey(request.PlanKey); err != nil {
		return err
	}
	return nil
}

func validateTerraformStatePlanKey(planKey string) error {
	planKey = strings.TrimSpace(planKey)
	if planKey == "" {
		return fmt.Errorf("terraform state planner plan_key must not be blank")
	}
	if strings.Contains(planKey, "s3://") || strings.ContainsAny(planKey, `/\`) {
		return fmt.Errorf("terraform state planner plan_key must not include raw source locator material")
	}
	for _, char := range planKey {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '.', '_', '-':
			continue
		default:
			return fmt.Errorf("terraform state planner plan_key contains unsupported character %q", char)
		}
	}
	return nil
}

func terraformStateRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorTerraformState,
		strings.TrimSpace(instance.InstanceID),
		terraformStateTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func terraformStateTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func terraformStateRequestedScopeSet(
	instance workflow.CollectorInstance,
	candidates []terraformstate.DiscoveryCandidate,
) string {
	type requestedCandidate struct {
		ScopeID     string `json:"scope_id"`
		CandidateID string `json:"candidate_id"`
		Source      string `json:"source"`
		BackendKind string `json:"backend_kind"`
	}
	payload := struct {
		CollectorInstanceID string               `json:"collector_instance_id"`
		Candidates          []requestedCandidate `json:"candidates"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Candidates:          make([]requestedCandidate, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		scopeValue, err := terraformStateCandidateScope(candidate)
		if err != nil {
			continue
		}
		candidateID, err := terraformstate.CandidatePlanningID(candidate)
		if err != nil {
			continue
		}
		payload.Candidates = append(payload.Candidates, requestedCandidate{
			ScopeID:     scopeValue.ScopeID,
			CandidateID: candidateID,
			Source:      string(candidate.Source),
			BackendKind: string(candidate.State.BackendKind),
		})
	}
	sort.Slice(payload.Candidates, func(i, j int) bool {
		return payload.Candidates[i].CandidateID < payload.Candidates[j].CandidateID
	})
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func terraformStateWorkItem(
	instance workflow.CollectorInstance,
	candidate terraformstate.DiscoveryCandidate,
	runID string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeValue, err := terraformStateCandidateScope(candidate)
	if err != nil {
		return workflow.WorkItem{}, err
	}
	candidateID, err := terraformstate.CandidatePlanningID(candidate)
	if err != nil {
		return workflow.WorkItem{}, err
	}
	acceptanceUnitID := strings.TrimSpace(candidate.RepoID)
	if acceptanceUnitID == "" {
		acceptanceUnitID = scopeValue.PartitionKey
	}
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s:%s", scope.CollectorTerraformState, instance.InstanceID, runID, candidateID),
		RunID:               runID,
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    acceptanceUnitID,
		SourceRunID:         candidateID,
		GenerationID:        candidateID,
		FairnessKey:         fmt.Sprintf("%s:%s", scope.CollectorTerraformState, strings.TrimSpace(instance.InstanceID)),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}

func terraformStateCandidateScope(candidate terraformstate.DiscoveryCandidate) (scope.IngestionScope, error) {
	metadata := map[string]string{}
	if repoID := strings.TrimSpace(candidate.RepoID); repoID != "" {
		metadata["repo_id"] = repoID
	}
	return scope.NewTerraformStateSnapshotScope(
		strings.TrimSpace(candidate.RepoID),
		string(candidate.State.BackendKind),
		candidate.State.Locator,
		metadata,
	)
}
