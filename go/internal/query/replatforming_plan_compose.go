// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// composeReplatformingPlan builds a bounded ReplatformingPlan for the requested
// scope from already-bounded, safety-normalized IaC management findings. It
// reuses the Terraform import-plan composition for import candidates and the
// source-state taxonomy for per-item evidence strength rather than re-deriving
// cloud truth. After mapping each finding to an item it assigns deterministic
// migration-wave and blast-radius ordering from the dependency and missing-
// evidence signals the findings already carry, never fabricating a dependency.
// The returned plan is structural only; callers validate it against the contract
// before serving.
func composeReplatformingPlan(
	scope ReplatformingPlanScope,
	findings []IaCManagementFindingRow,
	filter IaCManagementFilter,
) ReplatformingPlan {
	plan := NewReplatformingPlan(scope)
	plan.Items = make([]MigrationPacketItem, 0, len(findings))
	for _, finding := range findings {
		plan.Items = append(plan.Items, replatformingPlanItemForFinding(finding, filter))
	}
	applyReplatformingWaves(&plan, replatformingSignalsForFindings(findings))
	return plan
}

// replatformingPlanItemForFinding maps one IaC management finding into a
// provider-neutral migration packet item, carrying its management status,
// finding kind, taxonomy source state, safety gate, source layers, owner
// candidates, and import candidate.
func replatformingPlanItemForFinding(finding IaCManagementFindingRow, filter IaCManagementFilter) MigrationPacketItem {
	promotionRejected := replatformingPromotionRejected(finding)
	sourceState := ResolveReplatformingSourceState(finding.ManagementStatus, promotionRejected)
	item := MigrationPacketItem{
		ItemID:           finding.ID,
		Provider:         replatformingProvider(finding),
		ResourceType:     replatformingResourceType(finding),
		StableID:         replatformingStableID(finding),
		SourceState:      sourceState,
		ManagementStatus: finding.ManagementStatus,
		FindingKind:      finding.FindingKind,
		Confidence:       replatformingConfidenceLabel(finding.Confidence),
		SafetyGate:       finding.SafetyGate,
		SourceLayers:     replatformingSourceLayers(finding, sourceState),
		OwnerCandidates:  replatformingOwnerCandidates(finding, sourceState),
		ImportCandidate:  replatformingImportCandidate(finding, filter),
	}
	return item
}

// replatformingPromotionRejected reports whether a safety gate rejected
// promoting this read-only finding for a reason that is not already self-described
// by the management status's own taxonomy state. A security-sensitive refusal
// forces the source state to rejected so an unsafe item is never presented as
// ready. Status-driven review (ambiguous, unknown, or stale management) is left
// to its own evidence-derived taxonomy state instead of being flattened to
// rejected, so the taxonomy keeps conflicting, unproven, and stale findings
// distinct from a genuine safety refusal.
func replatformingPromotionRejected(finding IaCManagementFindingRow) bool {
	if !finding.SafetyGate.ReviewRequired &&
		!terraformImportStringSliceContains(finding.SafetyGate.RefusedActions, iacManagementSafetyActionTerraformImportPlan) {
		return false
	}
	for _, warning := range replatformingMergeWarnings(finding) {
		if warning == "security_sensitive_resource" {
			return true
		}
	}
	return false
}

// replatformingMergeWarnings collects the finding and safety-gate warning flags
// so promotion-rejection inspection sees the same warnings the safety gate used.
func replatformingMergeWarnings(finding IaCManagementFindingRow) []string {
	return iacMergeStringSets(finding.WarningFlags, finding.SafetyGate.Warnings)
}

func replatformingProvider(finding IaCManagementFindingRow) string {
	if provider := strings.TrimSpace(finding.Provider); provider != "" {
		return provider
	}
	return "aws"
}

func replatformingResourceType(finding IaCManagementFindingRow) string {
	if resourceType := strings.TrimSpace(finding.ResourceType); resourceType != "" {
		return resourceType
	}
	return "unknown"
}

// replatformingStableID returns the provider-stable identity for an item,
// preferring the ARN and falling back to the resource ID. It is never empty so
// the contract validator's required-field check holds.
func replatformingStableID(finding IaCManagementFindingRow) string {
	if stable := strings.TrimSpace(iacFirstNonEmpty(finding.ARN, finding.ResourceID)); stable != "" {
		return stable
	}
	if id := strings.TrimSpace(finding.ID); id != "" {
		return id
	}
	return "unknown"
}

// replatformingConfidenceLabel buckets a numeric confidence into a coarse,
// provider-neutral label so the packet item does not leak a false-precision
// score. Empty input maps to an unspecified label.
func replatformingConfidenceLabel(confidence float64) string {
	switch {
	case confidence <= 0:
		return "unspecified"
	case confidence >= 0.9:
		return "high"
	case confidence >= 0.6:
		return "medium"
	default:
		return "low"
	}
}

// replatformingSourceLayers records the evidence layers an item has or is
// missing so a coverage gap is never read as agreement. Declared IaC and applied
// state layers are present when the finding matched Terraform config/module or
// state; observed runtime is always present because the finding itself is
// runtime-drift evidence.
func replatformingSourceLayers(finding IaCManagementFindingRow, state ReplatformingSourceState) []ReplatformingSourceLayerStatus {
	layers := make([]ReplatformingSourceLayerStatus, 0, 4)
	declared := strings.TrimSpace(finding.MatchedTerraformConfigFile) != "" ||
		strings.TrimSpace(finding.MatchedTerraformModulePath) != "" ||
		strings.TrimSpace(finding.MatchedOtherIaCSource) != ""
	if declared {
		layers = append(layers, ReplatformingSourceLayerStatus{
			Layer:  ReplatformingSourceDeclaredIaC,
			State:  state,
			Detail: replatformingDeclaredDetail(finding),
		})
	} else {
		layers = append(layers, ReplatformingSourceLayerStatus{
			Layer:  ReplatformingSourceMissingEvidence,
			State:  ReplatformingSourceStateUnknown,
			Detail: "no source-controlled IaC declaration matched this resource",
		})
	}
	if strings.TrimSpace(finding.MatchedTerraformStateAddress) != "" {
		layers = append(layers, ReplatformingSourceLayerStatus{
			Layer:  ReplatformingSourceAppliedState,
			State:  state,
			Detail: "matched Terraform state address " + strings.TrimSpace(finding.MatchedTerraformStateAddress),
		})
	}
	layers = append(layers, ReplatformingSourceLayerStatus{
		Layer:  ReplatformingSourceObservedRuntime,
		State:  state,
		Detail: "observed via active AWS runtime drift finding " + strings.TrimSpace(finding.FindingKind),
	})
	return layers
}

func replatformingDeclaredDetail(finding IaCManagementFindingRow) string {
	switch {
	case strings.TrimSpace(finding.MatchedTerraformModulePath) != "":
		return "matched Terraform module " + strings.TrimSpace(finding.MatchedTerraformModulePath)
	case strings.TrimSpace(finding.MatchedTerraformConfigFile) != "":
		return "matched Terraform config file " + strings.TrimSpace(finding.MatchedTerraformConfigFile)
	default:
		return "matched non-Terraform IaC source " + strings.TrimSpace(finding.MatchedOtherIaCSource)
	}
}

// replatformingOwnerCandidates derives read-only owner attributions from the
// finding's service and environment candidates. When more than one candidate of
// a kind competes, or the item is ambiguous, every candidate of that kind names
// its ambiguity reasons so ownership is never promoted to a single fabricated
// owner. This honors the contract validator's ownership-ambiguity invariant.
func replatformingOwnerCandidates(finding IaCManagementFindingRow, state ReplatformingSourceState) []ReplatformingOwnerCandidate {
	owners := make([]ReplatformingOwnerCandidate, 0, len(finding.ServiceCandidates)+len(finding.EnvironmentCandidates))
	owners = append(owners, replatformingOwnerGroup("service", finding.ServiceCandidates, state)...)
	owners = append(owners, replatformingOwnerGroup("environment", finding.EnvironmentCandidates, state)...)
	if len(owners) == 0 {
		return nil
	}
	return owners
}

func replatformingOwnerGroup(kind string, values []string, state ReplatformingSourceState) []ReplatformingOwnerCandidate {
	deduped := replatformingDedupeNonEmpty(values)
	if len(deduped) == 0 {
		return nil
	}
	competing := len(deduped) > 1 || state == ReplatformingSourceStateAmbiguous
	owners := make([]ReplatformingOwnerCandidate, 0, len(deduped))
	for _, value := range deduped {
		owner := ReplatformingOwnerCandidate{Kind: kind, Value: value}
		if competing {
			owner.Confidence = "ambiguous"
			owner.AmbiguityReasons = replatformingAmbiguityReasons(kind, deduped, state)
		} else {
			owner.Confidence = "single_candidate"
		}
		owners = append(owners, owner)
	}
	return owners
}

func replatformingAmbiguityReasons(kind string, values []string, state ReplatformingSourceState) []string {
	reasons := make([]string, 0, 2)
	if len(values) > 1 {
		reasons = append(reasons, fmt.Sprintf("multiple %s attribution candidates: %s", kind, strings.Join(values, ", ")))
	}
	if state == ReplatformingSourceStateAmbiguous {
		reasons = append(reasons, "finding management status is ambiguous; ownership must not be promoted to a single owner")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, fmt.Sprintf("%s attribution is not deterministically resolved", kind))
	}
	return reasons
}

func replatformingDedupeNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// replatformingImportCandidate reuses the Terraform import-plan composition to
// produce a ready or refused import candidate, then projects it onto the
// provider-neutral contract shape. A refused candidate carries its refusal
// reasons and never an import block; a ready candidate carries its import block.
func replatformingImportCandidate(finding IaCManagementFindingRow, filter IaCManagementFilter) *ReplatformingImportCandidate {
	candidate := terraformImportPlanCandidateForFinding(finding, filter)
	if candidate.Status == "ready" {
		return &ReplatformingImportCandidate{
			Status:       ReplatformingImportStatusReady,
			ResourceType: candidate.TerraformResourceType,
			ImportBlock:  candidate.ImportBlock,
		}
	}
	reasons := candidate.RefusalReasons
	if len(reasons) == 0 {
		reasons = []string{"import_candidate_refused"}
	}
	return &ReplatformingImportCandidate{
		Status:         ReplatformingImportStatusRefused,
		ResourceType:   candidate.CloudResourceType,
		RefusalReasons: reasons,
	}
}
