package query

import (
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

const semanticExtractionStatusCapability = "semantic_extraction.status"

func (h *StatusHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *StatusHandler) getSemanticExtractionStatus(w http.ResponseWriter, r *http.Request) {
	report := status.BuildReport(status.RawSnapshot{}, status.DefaultOptions())
	if h != nil && h.StatusReader != nil {
		loaded, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
			return
		}
		report = loaded
	}

	semanticStatus := report.SemanticExtraction
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		semanticExtractionStatusToMap(semanticStatus),
		semanticExtractionStatusTruth(h.profile(), semanticStatus),
	)
}

func semanticExtractionStatusToMap(snapshot status.SemanticExtractionStatus) map[string]any {
	statusJSON := statusJSONFromSemanticExtraction(snapshot)
	result := map[string]any{
		"state":                                 statusJSON.State,
		"reason":                                statusJSON.Reason,
		"provider_configured":                   statusJSON.ProviderConfigured,
		"documentation_observations_enabled":    statusJSON.DocumentationObservationsEnabled,
		"code_hints_enabled":                    statusJSON.CodeHintsEnabled,
		"deterministic_paths_affected":          statusJSON.DeterministicPathsAffected,
		"supported_states":                      statusJSON.SupportedStates,
		"supported_provider_profile_states":     statusJSON.SupportedProviderProfileStates,
		"deterministic_documentation_unblocked": !statusJSON.DeterministicPathsAffected,
	}
	if len(statusJSON.ProviderProfiles) > 0 {
		result["provider_profiles"] = statusJSON.ProviderProfiles
	}
	if statusJSON.Detail != "" {
		result["detail"] = statusJSON.Detail
	}
	if statusJSON.UpdatedAt != "" {
		result["updated_at"] = statusJSON.UpdatedAt
	}
	return result
}

func statusJSONFromSemanticExtraction(snapshot status.SemanticExtractionStatus) semanticExtractionStatusView {
	normalized := status.BuildReport(status.RawSnapshot{SemanticExtraction: snapshot}, status.DefaultOptions()).SemanticExtraction
	view := semanticExtractionStatusView{
		State:                            normalized.State,
		Reason:                           normalized.Reason,
		Detail:                           normalized.Detail,
		ProviderConfigured:               normalized.ProviderConfigured,
		DocumentationObservationsEnabled: normalized.DocumentationObservationsEnabled,
		CodeHintsEnabled:                 normalized.CodeHintsEnabled,
		DeterministicPathsAffected:       normalized.DeterministicPathsAffected,
		ProviderProfiles:                 semanticProviderProfilesToMaps(normalized.ProviderProfiles),
		SupportedStates:                  status.SemanticExtractionSupportedStates(),
		SupportedProviderProfileStates:   status.SemanticProviderProfileSupportedStates(),
	}
	if !normalized.UpdatedAt.IsZero() {
		view.UpdatedAt = normalized.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return view
}

type semanticExtractionStatusView struct {
	State                            string
	Reason                           string
	Detail                           string
	ProviderConfigured               bool
	DocumentationObservationsEnabled bool
	CodeHintsEnabled                 bool
	DeterministicPathsAffected       bool
	UpdatedAt                        string
	ProviderProfiles                 []map[string]any
	SupportedStates                  []string
	SupportedProviderProfileStates   []string
}

func semanticProviderProfilesToMaps(profiles []status.SemanticProviderProfileStatus) []map[string]any {
	if len(profiles) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		row := map[string]any{
			"profile_id":               profile.ProfileID,
			"provider_kind":            profile.ProviderKind,
			"credential_source_kind":   profile.CredentialSourceKind,
			"credential_configured":    profile.CredentialConfigured,
			"source_classes":           profile.SourceClasses,
			"source_policy_configured": profile.SourcePolicyConfigured,
			"state":                    profile.State,
			"reason":                   profile.Reason,
		}
		if profile.DisplayName != "" {
			row["display_name"] = profile.DisplayName
		}
		if profile.ModelID != "" {
			row["model_id"] = profile.ModelID
		}
		if profile.EndpointProfileID != "" {
			row["endpoint_profile_id"] = profile.EndpointProfileID
		}
		if profile.Detail != "" {
			row["detail"] = profile.Detail
		}
		if !profile.UpdatedAt.IsZero() {
			row["updated_at"] = profile.UpdatedAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	return rows
}

func semanticExtractionStatusTruth(profile QueryProfile, snapshot status.SemanticExtractionStatus) *TruthEnvelope {
	view := statusJSONFromSemanticExtraction(snapshot)
	truth := BuildTruthEnvelope(
		profile,
		semanticExtractionStatusCapability,
		TruthBasisHybrid,
		view.Detail,
	)
	if view.State != status.SemanticExtractionAvailable {
		truth.Level = TruthLevelFallback
		truth.Freshness = TruthFreshness{
			State:  FreshnessUnavailable,
			Detail: view.Reason,
		}
	}
	return truth
}
