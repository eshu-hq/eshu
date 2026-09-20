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
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer/admissiondecision"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// DeployableUnitCorrelationResolutionNotReadyFailureClass classifies a
// deferral of deployable-unit correlation work whose own relationship
// generation has not activated yet.
//
// The storage queue registers this as a non-counting retry class because the
// intent is waiting on an upstream phase, not failing on its own merits.
const DeployableUnitCorrelationResolutionNotReadyFailureClass = "deployable_unit_correlation_resolution_not_ready"

// DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass classifies a
// deferral while a code repository can still be detached and re-projected.
const DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass = "deployable_unit_correlation_canonical_nodes_not_ready"

// deployableUnitCorrelationResolutionNotReadyError defers an intent until its
// own relationship generation activates.
type deployableUnitCorrelationResolutionNotReadyError struct {
	scopeID      string
	generationID string
	// cause carries the fence lookup failure when the deferral is outage
	// driven rather than backpressure driven; nil for a genuinely
	// incomplete corpus fence (see the workload-materialization twin).
	cause error
	// holdingScopeIDs names the scopes holding the corpus fence, best
	// effort; empty when the holder lookup is unwired or failed.
	holdingScopeIDs []string
}

func (e deployableUnitCorrelationResolutionNotReadyError) Error() string {
	msg := fmt.Sprintf(
		"cross-repo resolution not active for scope %s generation %s; deferring deployable unit correlation rather than evaluating against a partial resolved set",
		e.scopeID,
		e.generationID,
	)
	if e.cause != nil {
		msg += fmt.Sprintf("; corpus fence lookup failed: %v", e.cause)
	}
	if len(e.holdingScopeIDs) > 0 {
		msg += fmt.Sprintf("; holding scopes: %s", strings.Join(e.holdingScopeIDs, ","))
	}
	return msg
}

// Unwrap exposes the fence lookup failure to errors.Is/As without changing
// the retryable failure class.
func (e deployableUnitCorrelationResolutionNotReadyError) Unwrap() error {
	return e.cause
}

func (deployableUnitCorrelationResolutionNotReadyError) Retryable() bool { return true }

func (deployableUnitCorrelationResolutionNotReadyError) FailureClass() string {
	return DeployableUnitCorrelationResolutionNotReadyFailureClass
}

// deployableUnitCorrelationCanonicalNodesNotReadyError holds an edge-producing
// intent until repository projection has committed for every active code repo.
type deployableUnitCorrelationCanonicalNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e deployableUnitCorrelationCanonicalNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical repository projection is not quiescent for scope %s generation %s; deferring deployable unit correlation before graph writes",
		e.scopeID,
		e.generationID,
	)
}

func (deployableUnitCorrelationCanonicalNodesNotReadyError) Retryable() bool { return true }

func (deployableUnitCorrelationCanonicalNodesNotReadyError) FailureClass() string {
	return DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass
}

// deployableUnitCanonicalReposReady holds edge-producing work until no active
// code repository can run canonical Repository cleanup after the edge write.
func deployableUnitCanonicalReposReady(
	ctx context.Context,
	checker CanonicalCodeQuiescenceChecker,
	intent Intent,
	hasCandidates bool,
) error {
	if checker == nil || !hasCandidates {
		return nil
	}
	uncommitted, err := checker.HasUncommittedCanonicalCodeScopes(ctx)
	if err != nil {
		return fmt.Errorf("check canonical repository quiescence: %w", err)
	}
	if !uncommitted {
		return nil
	}
	return deployableUnitCorrelationCanonicalNodesNotReadyError{
		scopeID:      intent.ScopeID,
		generationID: intent.GenerationID,
	}
}

// checkDeployableUnitResolutionReadiness defers the intent while the resolved
// set it would read is partial: first the own-generation check, then the
// corpus-wide fence covering the foreign scopes the own check cannot see.
// Both defer with the same non-counting retry class, because success on a
// partial input is never reopened (#6184).
func checkDeployableUnitResolutionReadiness(
	ctx context.Context,
	activeLookup maintenance.RelationshipGenerationActiveLookup,
	completeLookup maintenance.RelationshipGenerationsCompleteLookup,
	incompleteScopesLookup maintenance.RelationshipGenerationsIncompleteScopesLookup,
	intent Intent,
	candidates []WorkloadCandidate,
) error {
	if !ownResolutionGenerationReady(activeLookup, intent, candidates) {
		return deployableUnitCorrelationResolutionNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}
	fenceReady, fenceErr := corpusResolutionsComplete(ctx, completeLookup, candidates)
	if fenceErr != nil {
		return deployableUnitCorrelationResolutionNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
			cause:        fenceErr,
		}
	}
	if !fenceReady {
		return deployableUnitCorrelationResolutionNotReadyError{
			scopeID:         intent.ScopeID,
			generationID:    intent.GenerationID,
			holdingScopeIDs: maintenance.IncompleteScopeIDs(ctx, incompleteScopesLookup),
		}
	}
	return nil
}

const (
	deployableUnitCorrelationEvidenceSource   = "reducer/deployable-unit-correlation"
	deployableUnitCorrelationRelationshipType = string(edgetype.CorrelatesDeployableUnit)
)

// ExtractDeployableUnitCorrelationRows runs the pure deployable-unit
// correlation pipeline over one intent's already-extracted workload
// candidates and its already-resolved cross-repo relationships, returning
// every candidate row (admitted and rejected) exactly as
// DeployableUnitCorrelationHandler.Handle computes them, plus the
// engine.Evaluation the rows were derived from.
//
// The caller extracts candidates from fact envelopes via
// ExtractWorkloadCandidates exactly once and passes the result in here.
// DeployableUnitCorrelationHandler.Handle needs that same candidate slice for
// its ResolvedRelationshipLoader call (loadWorkloadResolvedRelationships), so
// this seam accepting candidates instead of re-deriving them from envelopes
// avoids running ExtractWorkloadCandidates twice per intent (#5993 review).
// Ifá's deployable_unit_edges materialized-edge vacuity guard
// (go/internal/ifa/materializededges/deployable_unit.go, #5993) extracts
// candidates from a cataloged Odù's facts the same way before calling this
// seam directly with a hand-authored resolved-relationship fixture, mirroring
// the role ExtractSQLRelationshipRows and ExtractAllCodeRelationshipRows play
// for their families.
//
// Unlike those two families, deployable-unit correlation cannot derive its
// edges from facts alone: a candidate's deployment_repo_id only exists once a
// RelDeploysFrom resolved relationship (produced by a separate cross-repo
// resolution phase, never by this intent's own facts) is applied by
// applyResolvedDeploymentSources. Callers that want only the edges eligible
// for a canonical write must additionally filter the returned rows through
// AdmittedDeployableUnitRows, exactly as materializeDeployableUnitEdges does.
//
// now supplies the wall-clock reading each row's CreatedAt is stamped with
// (mirroring the AdmissionDecisionNow injection point on
// DeployableUnitCorrelationHandler); pass nil for real time.Now(), as Handle
// does. This function otherwise performs no I/O and reads no other
// process-global state, so it is safe for Ifá's deployable_unit_edges
// materialized-edge vacuity guard to call directly against a cataloged Odù --
// go/internal/ifa/AGENTS.md forbids wall-clock time inside Ifá derivation, and
// a caller that left this defaulting to real time.Now would have silently
// violated that invariant without any comparison failing to catch it, since
// CreatedAt is a SharedProjectionIntentRow struct field, not a Payload key,
// and is never asserted by an edge-identity comparison.
func ExtractDeployableUnitCorrelationRows(
	intent Intent,
	candidates []WorkloadCandidate,
	resolved []relationships.ResolvedRelationship,
	now func() time.Time,
) ([]SharedProjectionIntentRow, engine.Evaluation, error) {
	entityKeys, err := deployableUnitCorrelationEntityKeys(intent)
	if err != nil {
		return nil, engine.Evaluation{}, err
	}
	candidates = applyResolvedDeploymentSources(candidates, resolved)
	candidates = filterDeployableUnitCandidates(candidates, entityKeys)
	if len(candidates) == 0 {
		return nil, engine.Evaluation{}, nil
	}
	evaluation, err := evaluateDeployableUnitCandidates(intent, candidates)
	if err != nil {
		return nil, engine.Evaluation{}, err
	}
	return deployableUnitCorrelationRows(intent, evaluation, now), evaluation, nil
}

func (h DeployableUnitCorrelationHandler) materializeDeployableUnitEdges(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) (int, error) {
	if h.EdgeWriter == nil || len(rows) == 0 {
		return 0, nil
	}
	if err := h.retractDeployableUnitEdges(ctx, rows); err != nil {
		return 0, err
	}
	writeRows := AdmittedDeployableUnitRows(rows)
	if len(writeRows) == 0 {
		return 0, nil
	}
	// #5984: this handler has no unroutable-intent sink wired, so the write
	// report is deliberately discarded here and the rejected rows remain
	// visible only through the writer's WARN and
	// eshu_dp_shared_edge_unroutable_rows_total counter. That is unchanged
	// behaviour for this path, not a new loss -- but it IS a remaining gap,
	// and the compile-forced report is what makes it visible instead of
	// implicit. Wiring the sink here needs this handler's own proof.
	if _, err := h.EdgeWriter.WriteEdges(ctx, DomainDeployableUnitEdges, writeRows, deployableUnitCorrelationEvidenceSource); err != nil {
		return 0, fmt.Errorf("write deployable unit correlation edges: %w", err)
	}
	return len(writeRows), nil
}

func (h DeployableUnitCorrelationHandler) retractDeployableUnitEdges(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) error {
	if h.EdgeWriter == nil || len(rows) == 0 {
		return nil
	}
	if err := h.EdgeWriter.RetractEdges(ctx, DomainDeployableUnitEdges, rows, deployableUnitCorrelationEvidenceSource); err != nil {
		return fmt.Errorf("retract deployable unit correlation edges: %w", err)
	}
	return nil
}

// AdmittedDeployableUnitRows filters rows down to the ones eligible for a
// canonical CORRELATES_DEPLOYABLE_UNIT write: an admitted candidate carrying a
// non-empty deployment_repo_id. Exported so Ifá's deployable_unit_edges
// materialized-edge vacuity guard (#5993) can apply the SAME admission filter
// materializeDeployableUnitEdges uses, rather than a parallel
// re-implementation that could silently drift from it.
func AdmittedDeployableUnitRows(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	admitted := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(anyToString(row.Payload["admission_state"])) != string(correlationmodel.CandidateStateAdmitted) {
			continue
		}
		if strings.TrimSpace(anyToString(row.Payload["deployment_repo_id"])) == "" {
			continue
		}
		admitted = append(admitted, row)
	}
	return admitted
}

func deployableUnitCorrelationRows(
	intent Intent,
	evaluation engine.Evaluation,
	now func() time.Time,
) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(evaluation.Results))
	for _, result := range evaluation.Results {
		candidate := result.Candidate
		repoID := deployableUnitEvidenceValue(candidate, "repo_id")
		if repoID == "" {
			continue
		}
		deploymentRepoIDs := deployableUnitEvidenceValues(candidate, "deployment_repo_id")
		if len(deploymentRepoIDs) == 0 {
			deploymentRepoIDs = []string{""}
		}
		for _, deploymentRepoID := range deploymentRepoIDs {
			rows = append(rows, deployableUnitCorrelationRow(intent, candidate, repoID, deploymentRepoID, now))
		}
	}
	return rows
}

func deployableUnitRetractRowsFromFacts(
	intent Intent,
	envelopes []facts.Envelope,
	entityKeys map[string]struct{},
) []SharedProjectionIntentRow {
	var rows []SharedProjectionIntentRow
	for _, envelope := range envelopes {
		if envelope.FactKind != factKindRepository {
			continue
		}
		repoID := strings.TrimSpace(anyToString(envelope.Payload["graph_id"]))
		if repoID == "" {
			repoID = strings.TrimSpace(anyToString(envelope.Payload["repo_id"]))
		}
		if repoID == "" {
			continue
		}
		if !deployableUnitIntentMatchesRepository(entityKeys, repoID, anyToString(envelope.Payload["name"])) {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			IntentID:         intent.IntentID,
			ProjectionDomain: DomainDeployableUnitEdges,
			PartitionKey:     repoID,
			ScopeID:          intent.ScopeID,
			AcceptanceUnitID: deployableUnitAcceptanceUnitID(intent),
			RepositoryID:     repoID,
			SourceRunID:      intent.GenerationID,
			GenerationID:     intent.GenerationID,
			Payload: map[string]any{
				"repo_id":            repoID,
				"scope_id":           intent.ScopeID,
				"acceptance_unit_id": deployableUnitAcceptanceUnitID(intent),
				"generation_id":      intent.GenerationID,
			},
		})
	}
	return rows
}

func deployableUnitIntentMatchesRepository(entityKeys map[string]struct{}, repoID, repoName string) bool {
	for _, identity := range []string{repoID, repoName} {
		identity = strings.ToLower(strings.TrimSpace(identity))
		if identity == "" {
			continue
		}
		if _, ok := entityKeys[identity]; ok {
			return true
		}
		if _, ok := entityKeys[normalizedEntityKey(identity)]; ok {
			return true
		}
	}
	return false
}

func deployableUnitCorrelationRow(
	intent Intent,
	candidate correlationmodel.Candidate,
	repoID string,
	deploymentRepoID string,
	now func() time.Time,
) SharedProjectionIntentRow {
	unitKey := deployableUnitEvidenceValue(candidate, "deployable_unit_key")
	acceptanceUnitID := deployableUnitAcceptanceUnitID(intent)
	return SharedProjectionIntentRow{
		IntentID:         intent.IntentID,
		ProjectionDomain: DomainDeployableUnitEdges,
		PartitionKey:     repoID,
		ScopeID:          intent.ScopeID,
		AcceptanceUnitID: acceptanceUnitID,
		RepositoryID:     repoID,
		SourceRunID:      intent.GenerationID,
		GenerationID:     intent.GenerationID,
		CreatedAt:        admissiondecision.AdmissionNow(now),
		Payload: map[string]any{
			"repo_id":             repoID,
			"deployment_repo_id":  deploymentRepoID,
			"deployable_unit_key": unitKey,
			"correlation_key":     candidate.CorrelationKey,
			"relationship_type":   deployableUnitCorrelationRelationshipType,
			"evidence_type":       "deployable_unit_correlation",
			"resolution_source":   deployableUnitCorrelationEvidenceSource,
			"generation_id":       intent.GenerationID,
			"source_system":       intent.SourceSystem,
			"scope_id":            intent.ScopeID,
			"acceptance_unit_id":  acceptanceUnitID,
			"admission_state":     string(candidate.State),
			"confidence":          candidate.Confidence,
			"evidence_count":      len(candidate.Evidence),
			"evidence_kinds":      deployableUnitEvidenceKinds(candidate),
			"rule_pack":           deployableUnitRulePackName(candidate),
			"reason":              deployableUnitDecisionReason(candidate),
			"resolved_id":         fmt.Sprintf("deployable-unit-correlation:%s:%s", intent.GenerationID, candidate.CorrelationKey),
		},
	}
}

func deployableUnitAcceptanceUnitID(intent Intent) string {
	entityKeys := uniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) > 0 {
		return normalizedEntityKey(entityKeys[0])
	}
	return strings.TrimSpace(intent.ScopeID)
}

func deployableUnitEvidenceValue(candidate correlationmodel.Candidate, key string) string {
	for _, evidence := range candidate.Evidence {
		if evidence.Key == key && strings.TrimSpace(evidence.Value) != "" {
			return strings.TrimSpace(evidence.Value)
		}
	}
	return ""
}

func deployableUnitEvidenceValues(candidate correlationmodel.Candidate, key string) []string {
	var values []string
	for _, evidence := range candidate.Evidence {
		if evidence.Key != key {
			continue
		}
		values = appendUniqueString(values, evidence.Value)
	}
	return values
}

func deployableUnitEvidenceKinds(candidate correlationmodel.Candidate) []string {
	kinds := make([]string, 0, len(candidate.Evidence))
	for _, evidence := range candidate.Evidence {
		kinds = append(kinds, evidence.EvidenceType)
	}
	return uniqueSortedStrings(kinds)
}

func deployableUnitRulePackName(candidate correlationmodel.Candidate) string {
	switch {
	case deployableUnitHasEvidence(candidate, "argocd"):
		return "argocd"
	case deployableUnitHasEvidence(candidate, "kustomize"):
		return "kustomize"
	case deployableUnitHasEvidence(candidate, "helm"):
		return "helm"
	case deployableUnitHasEvidence(candidate, "jenkins"):
		return "jenkins"
	case deployableUnitHasEvidence(candidate, "github_actions"):
		return "github_actions"
	case deployableUnitHasEvidence(candidate, "docker_compose"):
		return "docker_compose"
	case deployableUnitHasEvidence(candidate, "cloudformation"):
		return "cloudformation"
	case deployableUnitHasEvidence(candidate, "dockerfile"):
		return "dockerfile"
	default:
		return "deployable-unit-fallback"
	}
}

func deployableUnitHasEvidence(candidate correlationmodel.Candidate, evidenceType string) bool {
	for _, evidence := range candidate.Evidence {
		if evidence.EvidenceType == evidenceType || strings.HasPrefix(evidence.EvidenceType, evidenceType+":") {
			return true
		}
	}
	return false
}

func deployableUnitDecisionReason(candidate correlationmodel.Candidate) string {
	if candidate.State == correlationmodel.CandidateStateAdmitted {
		return "admitted deployable unit correlation"
	}
	reasons := make([]string, 0, len(candidate.RejectionReasons))
	for _, reason := range candidate.RejectionReasons {
		reasons = append(reasons, string(reason))
	}
	if len(reasons) == 0 {
		return "deployable unit correlation not admitted"
	}
	return "deployable unit correlation rejected: " + strings.Join(uniqueSortedStrings(reasons), ",")
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
