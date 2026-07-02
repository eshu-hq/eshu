// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const deployableUnitCorrelationFallbackThreshold = 0.90

// DeployableUnitCorrelationHandler reduces one correlation intent into
// evidence-backed candidate evaluation and materializes admitted exact
// deployable-unit correlation edges when an edge writer is wired.
type DeployableUnitCorrelationHandler struct {
	FactLoader              FactLoader
	ResolvedLoader          ResolvedRelationshipLoader
	PhasePublisher          GraphProjectionPhasePublisher
	EdgeWriter              SharedProjectionEdgeWriter
	AdmissionDecisionWriter AdmissionDecisionWriter
	AdmissionDecisionNow    func() time.Time
}

// Handle executes the deployable-unit correlation reduction path.
func (h DeployableUnitCorrelationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStarted := time.Now()
	var timing deployableUnitCorrelationTiming
	var signals deployableUnitCorrelationSignals

	if intent.Domain != DomainDeployableUnitCorrelation {
		return Result{}, fmt.Errorf(
			"deployable unit correlation handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("deployable unit correlation fact loader is required")
	}

	entityKeys, err := deployableUnitCorrelationEntityKeys(intent)
	if err != nil {
		return Result{}, err
	}

	loadStarted := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindRepository, factKindFile},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for deployable unit correlation: %w", err)
	}
	timing.loadFactsDuration = time.Since(loadStarted)
	signals.factCount = len(envelopes)

	extractStarted := time.Now()
	candidates, _ := ExtractWorkloadCandidates(envelopes)
	timing.extractCandidatesDuration = time.Since(extractStarted)
	signals.rawCandidateCount = len(candidates)
	if h.ResolvedLoader != nil {
		resolvedStarted := time.Now()
		resolved, err := loadResolvedRelationshipsForIntent(ctx, h.ResolvedLoader, intent)
		timing.loadResolvedDuration = time.Since(resolvedStarted)
		if err != nil {
			return Result{}, fmt.Errorf("load resolved relationships for deployable unit correlation: %w", err)
		}
		applyStarted := time.Now()
		candidates = applyResolvedDeploymentSources(candidates, resolved)
		timing.applyResolvedDuration = time.Since(applyStarted)
	}

	filterStarted := time.Now()
	candidates = filterDeployableUnitCandidates(candidates, entityKeys)
	timing.filterCandidatesDuration = time.Since(filterStarted)
	signals.candidateCount = len(candidates)
	if len(candidates) == 0 {
		retractRows := deployableUnitRetractRowsFromFacts(intent, envelopes, entityKeys)
		signals.retractRows = len(retractRows)
		edgeStarted := time.Now()
		retractStarted := time.Now()
		if err := h.retractDeployableUnitEdges(ctx, retractRows); err != nil {
			return Result{}, err
		}
		timing.edgeRetractDuration = time.Since(retractStarted)
		timing.edgeMaterializeDuration = time.Since(edgeStarted)
		phaseStarted := time.Now()
		if err := publishIntentGraphPhase(
			ctx,
			h.PhasePublisher,
			intent,
			GraphProjectionKeyspaceDeployableUnitUID,
			GraphProjectionPhaseDeployableUnitCorrelation,
			time.Now().UTC(),
		); err != nil {
			return Result{}, err
		}
		timing.phasePublishDuration = time.Since(phaseStarted)
		timing.totalDuration = time.Since(totalStarted)
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainDeployableUnitCorrelation,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no deployable unit candidates found",
			SubDurations:    deployableUnitCorrelationSubDurations(timing),
			SubSignals:      deployableUnitCorrelationSubSignals(signals),
		}, nil
	}

	evaluateStarted := time.Now()
	evaluation, err := evaluateDeployableUnitCandidates(intent, candidates)
	if err != nil {
		return Result{}, err
	}
	timing.evaluateCandidatesDuration = time.Since(evaluateStarted)
	summary := correlation.BuildSummary(evaluation)
	evaluatedCandidateCount := len(evaluation.Results)
	signals.evaluatedCandidates = evaluatedCandidateCount
	edgeRows := deployableUnitCorrelationRows(intent, evaluation)
	signals.edgeRows = len(edgeRows)
	edgeStarted := time.Now()
	edgeResult, err := h.materializeDeployableUnitEdges(ctx, edgeRows)
	if err != nil {
		return Result{}, err
	}
	timing.edgeMaterializeDuration = time.Since(edgeStarted)
	timing.edgeRetractDuration = edgeResult.retractDuration
	timing.edgeWriteDuration = edgeResult.writeDuration
	signals.retractRows = edgeResult.retractRows
	signals.writeRows = edgeResult.writeRows
	signals.canonicalWrites = edgeResult.canonicalWrites
	decisionStarted := time.Now()
	if err := h.writeDeployableUnitAdmissionDecisions(ctx, intent, evaluation, edgeResult.canonicalWrites); err != nil {
		return Result{}, err
	}
	timing.admissionDecisionDuration = time.Since(decisionStarted)
	phaseStarted := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceDeployableUnitUID,
		GraphProjectionPhaseDeployableUnitCorrelation,
		time.Now().UTC(),
	); err != nil {
		return Result{}, err
	}
	timing.phasePublishDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainDeployableUnitCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: deployableUnitCorrelationSummary(evaluatedCandidateCount, summary),
		CanonicalWrites: edgeResult.canonicalWrites,
		SubDurations:    deployableUnitCorrelationSubDurations(timing),
		SubSignals:      deployableUnitCorrelationSubSignals(signals),
	}, nil
}

func deployableUnitCorrelationEntityKeys(intent Intent) (map[string]struct{}, error) {
	entityKeys := uniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) == 0 {
		return nil, fmt.Errorf(
			"deployable unit correlation intent %q must include at least one entity key",
			intent.IntentID,
		)
	}

	normalized := make(map[string]struct{}, len(entityKeys))
	for _, key := range entityKeys {
		raw := strings.ToLower(strings.TrimSpace(key))
		if raw != "" {
			normalized[raw] = struct{}{}
		}
		if alias := normalizedEntityKey(key); alias != "" {
			normalized[alias] = struct{}{}
		}
	}
	return normalized, nil
}

func loadResolvedRelationshipsForIntent(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	intent Intent,
) ([]relationships.ResolvedRelationship, error) {
	if generationScoped, ok := loader.(GenerationScopedResolvedRelationshipLoader); ok {
		return generationScoped.GetResolvedRelationshipsForGeneration(
			ctx,
			intent.ScopeID,
			intent.GenerationID,
		)
	}
	return loader.GetResolvedRelationships(ctx, intent.ScopeID)
}

func filterDeployableUnitCandidates(
	candidates []WorkloadCandidate,
	entityKeys map[string]struct{},
) []WorkloadCandidate {
	filtered := make([]WorkloadCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		for _, key := range candidateIdentityKeys(candidate) {
			if _, ok := entityKeys[strings.ToLower(strings.TrimSpace(key))]; ok {
				filtered = append(filtered, candidate)
				break
			}
		}
	}
	return filtered
}

func candidateIdentityKeys(candidate WorkloadCandidate) []string {
	keys := make([]string, 0, 4)
	appendCandidateIdentityKey := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for _, existing := range keys {
			if existing == value {
				return
			}
		}
		keys = append(keys, value)
	}

	appendCandidateIdentityKey(candidate.RepoID)
	appendCandidateIdentityKey(candidate.RepoName)
	appendCandidateIdentityKey(normalizedEntityKey(candidate.RepoID))
	appendCandidateIdentityKey(normalizedEntityKey(candidate.RepoName))

	return keys
}

func normalizedEntityKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return ""
	}
	if idx := strings.LastIndex(key, ":"); idx >= 0 && idx < len(key)-1 {
		return strings.TrimSpace(key[idx+1:])
	}
	return key
}

func evaluateDeployableUnitCandidates(
	intent Intent,
	candidates []WorkloadCandidate,
) (engine.Evaluation, error) {
	var merged engine.Evaluation

	for _, candidate := range candidates {
		pack := deployableUnitRulePack(candidate)
		modelCandidates := deployableUnitModelCandidates(intent, candidate)
		evaluation, err := engine.Evaluate(pack, modelCandidates)
		if err != nil {
			return engine.Evaluation{}, fmt.Errorf(
				"evaluate deployable unit candidate %q: %w",
				candidate.RepoName,
				err,
			)
		}
		merged.OrderedRuleNames = append(merged.OrderedRuleNames, evaluation.OrderedRuleNames...)
		merged.Results = append(merged.Results, evaluation.Results...)
	}

	return merged, nil
}

func deployableUnitModelCandidates(
	intent Intent,
	candidate WorkloadCandidate,
) []correlationmodel.Candidate {
	unitKeys := deployableUnitKeys(candidate)
	modelCandidates := make([]correlationmodel.Candidate, 0, len(unitKeys))
	for _, unitKey := range unitKeys {
		modelCandidates = append(modelCandidates, deployableUnitModelCandidate(intent, candidate, unitKey, len(unitKeys) > 1))
	}
	return modelCandidates
}

func deployableUnitModelCandidate(
	intent Intent,
	candidate WorkloadCandidate,
	unitKey string,
	ambiguous bool,
) correlationmodel.Candidate {
	evidence := make([]correlationmodel.EvidenceAtom, 0, len(candidate.Provenance)+len(candidate.ResourceKinds)+len(candidate.Namespaces)+2)
	confidence := normalizedCandidateConfidence(candidate.Confidence)
	if ambiguous && !deployableUnitMatchesPrimaryIdentity(candidate, unitKey) {
		confidence = boundedAmbiguousDeployableUnitConfidence(confidence)
	}
	evidence = append(evidence, correlationmodel.EvidenceAtom{
		ID:           fmt.Sprintf("%s:%s:repo", intent.IntentID, candidate.RepoID),
		SourceSystem: intent.SourceSystem,
		EvidenceType: "repository_identity",
		ScopeID:      intent.ScopeID,
		Key:          "repo_id",
		Value:        candidate.RepoID,
		Confidence:   confidence,
	})
	evidence = append(evidence, correlationmodel.EvidenceAtom{
		ID:           fmt.Sprintf("%s:%s:unit", intent.IntentID, candidate.RepoID),
		SourceSystem: intent.SourceSystem,
		EvidenceType: "deployable_unit_key",
		ScopeID:      intent.ScopeID,
		Key:          "deployable_unit_key",
		Value:        unitKey,
		Confidence:   confidence,
	})
	evidence = append(evidence, deployableUnitStructuralEvidence(intent, candidate, unitKey, confidence)...)
	for idx, provenance := range candidate.Provenance {
		evidence = append(evidence, correlationmodel.EvidenceAtom{
			ID:           fmt.Sprintf("%s:%s:%d", intent.IntentID, candidate.RepoID, idx),
			SourceSystem: intent.SourceSystem,
			EvidenceType: strings.TrimSpace(provenance),
			ScopeID:      intent.ScopeID,
			Key:          "repo_name",
			Value:        candidate.RepoName,
			Confidence:   confidence,
		})
	}
	for idx, kind := range candidate.ResourceKinds {
		evidence = append(evidence, correlationmodel.EvidenceAtom{
			ID:           fmt.Sprintf("%s:%s:resource-kind:%d", intent.IntentID, candidate.RepoID, idx),
			SourceSystem: intent.SourceSystem,
			EvidenceType: "resource_kind",
			ScopeID:      intent.ScopeID,
			Key:          "resource_kind",
			Value:        kind,
			Confidence:   confidence,
		})
	}
	for idx, namespace := range candidate.Namespaces {
		evidence = append(evidence, correlationmodel.EvidenceAtom{
			ID:           fmt.Sprintf("%s:%s:namespace:%d", intent.IntentID, candidate.RepoID, idx),
			SourceSystem: intent.SourceSystem,
			EvidenceType: "namespace",
			ScopeID:      intent.ScopeID,
			Key:          "namespace",
			Value:        namespace,
			Confidence:   confidence,
		})
	}
	for idx, deploymentRepoID := range candidateDeploymentRepoIDs(candidate) {
		evidence = append(evidence, correlationmodel.EvidenceAtom{
			ID:           fmt.Sprintf("%s:%s:deploy-repo:%d", intent.IntentID, candidate.RepoID, idx),
			SourceSystem: intent.SourceSystem,
			EvidenceType: "deployment_repo",
			ScopeID:      intent.ScopeID,
			Key:          "deployment_repo_id",
			Value:        deploymentRepoID,
			Confidence:   confidence,
		})
	}

	return correlationmodel.Candidate{
		ID:             fmt.Sprintf("deployable-unit:%s:%s", candidate.RepoID, unitKey),
		Kind:           "deployable_unit",
		CorrelationKey: fmt.Sprintf("%s:%s", candidate.RepoID, unitKey),
		Confidence:     confidence,
		State:          correlationmodel.CandidateStateProvisional,
		Evidence:       evidence,
	}
}

func deployableUnitStructuralEvidence(
	intent Intent,
	candidate WorkloadCandidate,
	unitKey string,
	confidence float64,
) []correlationmodel.EvidenceAtom {
	evidence := make([]correlationmodel.EvidenceAtom, 0, 4)
	for _, provenance := range candidate.Provenance {
		var (
			evidenceType string
			key          string
			value        string
		)
		switch {
		case strings.HasPrefix(provenance, "dockerfile_runtime"):
			evidenceType = "dockerfile"
			key = "image"
			value = unitKey
		case strings.HasPrefix(provenance, "docker_compose_runtime"):
			evidenceType = "docker_compose"
			key = "service"
			value = unitKey
		case strings.HasPrefix(provenance, "argocd_application"):
			evidenceType = "argocd"
			key = "application"
			value = unitKey
		case strings.HasPrefix(provenance, "kustomize_resource"):
			evidenceType = "kustomize"
			key = "resource"
			value = unitKey
		case strings.HasPrefix(provenance, "helm_deployment"):
			evidenceType = "helm"
			key = "release"
			value = unitKey
		case strings.HasPrefix(provenance, "jenkins_pipeline"):
			evidenceType = "jenkins"
			key = "repository"
			value = candidate.RepoName
		case strings.HasPrefix(provenance, "github_actions_workflow"):
			evidenceType = "github_actions"
			key = "repository"
			value = candidate.RepoName
		case strings.HasPrefix(provenance, "terraform"):
			evidenceType = "terraform_config"
			key = "module"
			value = unitKey
		case strings.HasPrefix(provenance, "cloudformation_template"):
			evidenceType = "cloudformation"
			key = "stack"
			value = unitKey
		default:
			continue
		}
		evidence = append(evidence, correlationmodel.EvidenceAtom{
			ID:           fmt.Sprintf("%s:%s:struct:%s:%s", intent.IntentID, candidate.RepoID, evidenceType, key),
			SourceSystem: intent.SourceSystem,
			EvidenceType: evidenceType,
			ScopeID:      intent.ScopeID,
			Key:          key,
			Value:        value,
			Confidence:   confidence,
		})
	}
	return evidence
}

func deployableUnitKeys(candidate WorkloadCandidate) []string {
	keys := make(map[string]struct{})
	for _, provenance := range candidate.Provenance {
		if !strings.HasPrefix(provenance, "dockerfile_runtime:") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(provenance, "dockerfile_runtime:"))
		key := deployableUnitKeyFromPath(candidate.RepoName, path)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	if len(keys) == 0 {
		return []string{candidate.RepoName}
	}
	values := make([]string, 0, len(keys))
	for key := range keys {
		values = append(values, key)
	}
	return uniqueSortedStrings(values)
}

func deployableUnitCorrelationSummary(evaluatedCandidates int, summary correlation.Summary) string {
	return fmt.Sprintf(
		"evaluated %d deployable unit candidate(s); admitted=%d rejected=%d low_confidence=%d conflicts=%d rules=%d",
		evaluatedCandidates,
		summary.AdmittedCandidates,
		summary.RejectedCandidates,
		summary.LowConfidenceCount,
		summary.ConflictCount,
		summary.EvaluatedRules,
	)
}
