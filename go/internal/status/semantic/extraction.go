// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

const (
	// ExtractionUnavailable means no semantic extraction provider can run.
	ExtractionUnavailable = "unavailable"
	// ExtractionAvailable means provider-backed semantic extraction can run.
	ExtractionAvailable = "available"
	// ExtractionAvailableButDisabledForScope means the provider is
	// configured, but the selected scope is disabled.
	ExtractionAvailableButDisabledForScope = "available_but_disabled_for_scope"
	// ExtractionDisabledByPolicy means policy disables semantic extraction.
	ExtractionDisabledByPolicy = "disabled_by_policy"
	// ExtractionProviderUnhealthy means a configured provider is not healthy.
	ExtractionProviderUnhealthy = "provider_unhealthy"
)

const (
	// ExtractionReasonProviderNotConfigured is the stable no-provider reason.
	ExtractionReasonProviderNotConfigured = "provider_not_configured"
	// ExtractionReasonProviderConfigured marks an available provider.
	ExtractionReasonProviderConfigured = "provider_configured"
	// ExtractionReasonScopeDisabled marks a scope-level disablement.
	ExtractionReasonScopeDisabled = "scope_disabled"
	// ExtractionReasonPolicyDisabled marks an operator policy disablement.
	ExtractionReasonPolicyDisabled = "policy_disabled"
	// ExtractionReasonProviderUnhealthy marks a configured but unhealthy provider.
	ExtractionReasonProviderUnhealthy = "provider_unhealthy"
	// ExtractionReasonInvalidState marks a malformed status row.
	ExtractionReasonInvalidState = "invalid_semantic_extraction_state"
)

var extractionStates = []string{
	ExtractionUnavailable,
	ExtractionAvailable,
	ExtractionAvailableButDisabledForScope,
	ExtractionDisabledByPolicy,
	ExtractionProviderUnhealthy,
}

// ExtractionStatus captures optional LLM-assisted extraction liveness.
type ExtractionStatus struct {
	State                            string
	Reason                           string
	Detail                           string
	ProviderConfigured               bool
	DocumentationObservationsEnabled bool
	CodeHintsEnabled                 bool
	DeterministicPathsAffected       bool
	UpdatedAt                        time.Time
	ProviderProfiles                 []ProviderProfileStatus
	Queue                            ExtractionQueueSnapshot
	Budget                           ExtractionBudgetSnapshot
	Audit                            ExtractionAuditSnapshot
}

// ExtractionSupportedStates returns the stable status enum values.
func ExtractionSupportedStates() []string {
	return slices.Clone(extractionStates)
}

// DefaultExtractionStatus returns the zero-key runtime status.
func DefaultExtractionStatus() ExtractionStatus {
	return ExtractionStatus{
		State:                            ExtractionUnavailable,
		Reason:                           ExtractionReasonProviderNotConfigured,
		Detail:                           "no semantic extraction provider is configured; deterministic indexing, reducer projection, API reads, MCP tools, and documentation fact verification are unaffected",
		ProviderConfigured:               false,
		DocumentationObservationsEnabled: false,
		CodeHintsEnabled:                 false,
		DeterministicPathsAffected:       false,
	}
}

func NormalizeExtractionStatus(snapshot ExtractionStatus) ExtractionStatus {
	profiles := CloneProviderProfiles(snapshot.ProviderProfiles)
	state := strings.TrimSpace(snapshot.State)
	if state == "" {
		if len(profiles) > 0 {
			return extractionStatusFromProviderProfiles(snapshot, profiles)
		}
		return defaultExtractionStatusWithObservability(snapshot, profiles)
	}
	if !isExtractionState(state) {
		out := defaultExtractionStatusWithObservability(snapshot, profiles)
		out.Reason = ExtractionReasonInvalidState
		out.Detail = fmt.Sprintf("semantic extraction status %q is unsupported; treating semantic extraction as unavailable", state)
		out.UpdatedAt = snapshot.UpdatedAt
		return out
	}

	out := ExtractionStatus{
		State:                            state,
		Reason:                           strings.TrimSpace(snapshot.Reason),
		Detail:                           safeExtractionDetail(snapshot.Detail),
		ProviderConfigured:               snapshot.ProviderConfigured,
		DocumentationObservationsEnabled: snapshot.DocumentationObservationsEnabled,
		CodeHintsEnabled:                 snapshot.CodeHintsEnabled,
		DeterministicPathsAffected:       false,
		UpdatedAt:                        snapshot.UpdatedAt,
		ProviderProfiles:                 profiles,
		Queue:                            normalizeExtractionQueueSnapshot(snapshot.Queue),
		Budget:                           normalizeExtractionBudgetSnapshot(snapshot.Budget),
		Audit:                            normalizeExtractionAuditSnapshot(snapshot.Audit),
	}
	if len(profiles) > 0 {
		out.ProviderConfigured = out.ProviderConfigured || profilesConfigured(profiles)
		out.DocumentationObservationsEnabled = out.DocumentationObservationsEnabled ||
			profilesAllowSource(profiles, "documentation")
		out.CodeHintsEnabled = out.CodeHintsEnabled || profilesAllowSource(profiles, "code_hints")
	}
	if out.Reason == "" {
		out.Reason = defaultExtractionReason(out.State)
	}
	if out.Detail == "" {
		out.Detail = defaultExtractionDetail(out.State)
	}
	switch out.State {
	case ExtractionAvailable, ExtractionAvailableButDisabledForScope, ExtractionProviderUnhealthy:
		out.ProviderConfigured = true
	case ExtractionUnavailable:
		out.ProviderConfigured = false
	}
	if out.State != ExtractionAvailable {
		out.DocumentationObservationsEnabled = false
		out.CodeHintsEnabled = false
	}
	return out
}

func defaultExtractionStatusWithObservability(
	snapshot ExtractionStatus,
	profiles []ProviderProfileStatus,
) ExtractionStatus {
	out := DefaultExtractionStatus()
	out.UpdatedAt = snapshot.UpdatedAt
	out.ProviderProfiles = profiles
	out.Queue = normalizeExtractionQueueSnapshot(snapshot.Queue)
	out.Budget = normalizeExtractionBudgetSnapshot(snapshot.Budget)
	out.Audit = normalizeExtractionAuditSnapshot(snapshot.Audit)
	return out
}

func extractionStatusFromProviderProfiles(
	snapshot ExtractionStatus,
	profiles []ProviderProfileStatus,
) ExtractionStatus {
	out := ExtractionStatus{
		UpdatedAt:        snapshot.UpdatedAt,
		ProviderProfiles: profiles,
		Queue:            normalizeExtractionQueueSnapshot(snapshot.Queue),
		Budget:           normalizeExtractionBudgetSnapshot(snapshot.Budget),
		Audit:            normalizeExtractionAuditSnapshot(snapshot.Audit),
	}
	out.ProviderConfigured = profilesConfigured(profiles)
	out.DocumentationObservationsEnabled = profilesAllowSource(profiles, "documentation")
	out.CodeHintsEnabled = profilesAllowSource(profiles, "code_hints")

	switch {
	case profilesUnhealthy(profiles):
		out.State = ExtractionProviderUnhealthy
		out.Reason = ExtractionReasonProviderUnhealthy
	case !out.ProviderConfigured:
		out.State = ExtractionUnavailable
		out.Reason = ExtractionReasonProviderNotConfigured
	case !profilesHaveAnySourcePolicy(profiles):
		out.State = ExtractionDisabledByPolicy
		out.Reason = ExtractionReasonPolicyDisabled
	case out.DocumentationObservationsEnabled || out.CodeHintsEnabled:
		out.State = ExtractionAvailable
		out.Reason = ExtractionReasonProviderConfigured
	default:
		out.State = ExtractionAvailableButDisabledForScope
		out.Reason = ExtractionReasonScopeDisabled
	}
	out.Detail = defaultExtractionDetail(out.State)
	if out.State != ExtractionAvailable {
		out.DocumentationObservationsEnabled = false
		out.CodeHintsEnabled = false
	}
	return out
}

func profilesConfigured(profiles []ProviderProfileStatus) bool {
	for _, profile := range profiles {
		if profileConfigured(profile) {
			return true
		}
	}
	return false
}

func profilesUnhealthy(profiles []ProviderProfileStatus) bool {
	for _, profile := range profiles {
		if profileUnhealthy(profile) {
			return true
		}
	}
	return false
}

func profilesHaveAnySourcePolicy(profiles []ProviderProfileStatus) bool {
	for _, profile := range profiles {
		if profileConfigured(profile) && profile.SourcePolicyConfigured {
			return true
		}
	}
	return false
}

func profilesAllowSource(profiles []ProviderProfileStatus, sourceClass string) bool {
	for _, profile := range profiles {
		if profileAllowsSource(profile, sourceClass) {
			return true
		}
	}
	return false
}

func isExtractionState(state string) bool {
	return slices.Contains(extractionStates, state)
}

func defaultExtractionReason(state string) string {
	switch state {
	case ExtractionAvailable:
		return ExtractionReasonProviderConfigured
	case ExtractionAvailableButDisabledForScope:
		return ExtractionReasonScopeDisabled
	case ExtractionDisabledByPolicy:
		return ExtractionReasonPolicyDisabled
	case ExtractionProviderUnhealthy:
		return ExtractionReasonProviderUnhealthy
	default:
		return ExtractionReasonProviderNotConfigured
	}
}

func defaultExtractionDetail(state string) string {
	switch state {
	case ExtractionAvailable:
		return "semantic extraction provider is configured; deterministic evidence remains the admission gate for code hints"
	case ExtractionAvailableButDisabledForScope:
		return "semantic extraction provider is configured, but this scope is disabled; deterministic indexing and reads are unaffected"
	case ExtractionDisabledByPolicy:
		return "semantic extraction is disabled by operator policy; deterministic indexing and reads are unaffected"
	case ExtractionProviderUnhealthy:
		return "semantic extraction provider is unhealthy; deterministic indexing and reads are unaffected"
	default:
		return DefaultExtractionStatus().Detail
	}
}

func safeExtractionDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	lower := strings.ToLower(detail)
	for _, unsafe := range []string{
		"prompt",
		"response",
		"secret",
		"credential",
		"token",
		"api key",
	} {
		if strings.Contains(lower, unsafe) {
			return ""
		}
	}
	return detail
}

func RenderExtractionLine(snapshot ExtractionStatus) string {
	status := NormalizeExtractionStatus(snapshot)
	line := fmt.Sprintf(
		"Semantic extraction: state=%s reason=%s code_hints=%s documentation_observations=%s deterministic_paths=%s provider_profiles=%d",
		status.State,
		status.Reason,
		enabledText(status.CodeHintsEnabled),
		enabledText(status.DocumentationObservationsEnabled),
		affectedText(status.DeterministicPathsAffected),
		len(status.ProviderProfiles),
	)
	if len(status.ProviderProfiles) == 0 {
		return line + extractionObservabilityText(status)
	}

	profileParts := make([]string, 0, len(status.ProviderProfiles))
	for _, profile := range status.ProviderProfiles {
		profileParts = append(profileParts, fmt.Sprintf(
			"profile=%s provider=%s credential_source=%s credential_configured=%t state=%s source_policy=%t sources=%s",
			profile.ProfileID,
			profile.ProviderKind,
			profile.CredentialSourceKind,
			profile.CredentialConfigured,
			profile.State,
			profile.SourcePolicyConfigured,
			strings.Join(profile.SourceClasses, ","),
		))
	}
	return line + " " + strings.Join(profileParts, "; ") + extractionObservabilityText(status)
}

func extractionObservabilityText(status ExtractionStatus) string {
	parts := []string{}
	if extractionQueueHasValues(status.Queue) {
		parts = append(
			parts,
			fmt.Sprintf("semantic_queue_total=%d", status.Queue.Total),
			fmt.Sprintf("semantic_queue_pending=%d", status.Queue.Pending),
			fmt.Sprintf("semantic_queue_retrying=%d", status.Queue.Retrying),
			fmt.Sprintf("semantic_queue_dead_letter=%d", status.Queue.DeadLetter),
			fmt.Sprintf("semantic_budget_exhausted=%d", status.Queue.BudgetExhausted),
		)
	}
	if extractionBudgetHasValues(status.Budget) {
		parts = append(
			parts,
			fmt.Sprintf("semantic_estimated_input_tokens=%d", status.Budget.EstimatedInputTokens),
			fmt.Sprintf("semantic_actual_input_tokens=%d", status.Budget.ActualInputTokens),
			fmt.Sprintf("semantic_estimated_cost_micros=%d", status.Budget.EstimatedCostMicros),
			fmt.Sprintf("semantic_actual_cost_micros=%d", status.Budget.ActualCostMicros),
		)
	}
	if extractionAuditHasValues(status.Audit) {
		parts = append(
			parts,
			fmt.Sprintf(
				"semantic_audit_actor_classes=%s",
				shared.FormatTotals(shared.CountMap(status.Audit.ActorClassCounts)),
			),
		)
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func enabledText(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func affectedText(affected bool) string {
	if affected {
		return "affected"
	}
	return "unaffected"
}

type ExtractionJSON struct {
	State                            string                `json:"state"`
	Reason                           string                `json:"reason"`
	Detail                           string                `json:"detail,omitempty"`
	ProviderConfigured               bool                  `json:"provider_configured"`
	DocumentationObservationsEnabled bool                  `json:"documentation_observations_enabled"`
	CodeHintsEnabled                 bool                  `json:"code_hints_enabled"`
	DeterministicPathsAffected       bool                  `json:"deterministic_paths_affected"`
	UpdatedAt                        string                `json:"updated_at,omitempty"`
	ProviderProfiles                 []ProviderProfileJSON `json:"provider_profiles,omitempty"`
	Queue                            *extractionQueueJSON  `json:"queue,omitempty"`
	Budget                           *extractionBudgetJSON `json:"budget,omitempty"`
	Audit                            *extractionAuditJSON  `json:"audit,omitempty"`
	SupportedStates                  []string              `json:"supported_states"`
	SupportedProviderProfileStates   []string              `json:"supported_provider_profile_states"`
}

func ExtractionStatusJSON(snapshot ExtractionStatus) ExtractionJSON {
	status := NormalizeExtractionStatus(snapshot)
	out := ExtractionJSON{
		State:                            status.State,
		Reason:                           status.Reason,
		Detail:                           status.Detail,
		ProviderConfigured:               status.ProviderConfigured,
		DocumentationObservationsEnabled: status.DocumentationObservationsEnabled,
		CodeHintsEnabled:                 status.CodeHintsEnabled,
		DeterministicPathsAffected:       status.DeterministicPathsAffected,
		ProviderProfiles:                 ProviderProfilesJSON(status.ProviderProfiles),
		SupportedStates:                  ExtractionSupportedStates(),
		SupportedProviderProfileStates:   ProviderProfileSupportedStates(),
	}
	if !status.UpdatedAt.IsZero() {
		out.UpdatedAt = status.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if extractionQueueHasValues(status.Queue) {
		out.Queue = extractionQueueStatusJSON(status.Queue)
	}
	if extractionBudgetHasValues(status.Budget) {
		out.Budget = extractionBudgetStatusJSON(status.Budget)
	}
	if extractionAuditHasValues(status.Audit) {
		out.Audit = extractionAuditStatusJSON(status.Audit)
	}
	return out
}
