package status

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	// SemanticProviderProfileConfigured means a profile has the metadata and
	// credential reference needed for future provider-backed extraction.
	SemanticProviderProfileConfigured = "configured"
	// SemanticProviderProfileUnconfigured means a profile is present but missing
	// required non-secret configuration.
	SemanticProviderProfileUnconfigured = "unconfigured"
	// SemanticProviderProfileHealthy means an external health source marked the
	// profile healthy. Eshu does not probe providers in the no-traffic profile
	// registry.
	SemanticProviderProfileHealthy = "healthy"
	// SemanticProviderProfileUnhealthy means an external health source marked the
	// profile unhealthy.
	SemanticProviderProfileUnhealthy = "unhealthy"
)

var semanticProviderProfileStates = []string{
	SemanticProviderProfileConfigured,
	SemanticProviderProfileUnconfigured,
	SemanticProviderProfileHealthy,
	SemanticProviderProfileUnhealthy,
}

// SemanticProviderProfileStatus is the redacted operator view of one semantic
// extraction provider profile.
type SemanticProviderProfileStatus struct {
	ProfileID              string
	DisplayName            string
	ProviderKind           string
	CredentialSourceKind   string
	CredentialConfigured   bool
	ModelID                string
	EmbeddingDimensions    int
	EndpointProfileID      string
	SourceClasses          []string
	SourcePolicyConfigured bool
	State                  string
	Reason                 string
	Detail                 string
	UpdatedAt              time.Time
}

// SemanticProviderProfileSupportedStates returns stable provider profile states.
func SemanticProviderProfileSupportedStates() []string {
	return slices.Clone(semanticProviderProfileStates)
}

// WithSemanticProviderProfiles wraps a reader with static, redacted provider
// profile metadata sourced from runtime configuration.
func WithSemanticProviderProfiles(reader Reader, profiles ...SemanticProviderProfileStatus) Reader {
	if reader == nil {
		return nil
	}
	if len(profiles) == 0 {
		return reader
	}
	return semanticProviderProfileReader{
		reader:   reader,
		profiles: cloneSemanticProviderProfiles(profiles),
	}
}

type semanticProviderProfileReader struct {
	reader   Reader
	profiles []SemanticProviderProfileStatus
}

func (r semanticProviderProfileReader) ReadStatusSnapshot(
	ctx context.Context,
	asOf time.Time,
) (RawSnapshot, error) {
	return r.ReadStatusSnapshotFiltered(ctx, asOf, FullSnapshotSelection())
}

func (r semanticProviderProfileReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	selection SnapshotSelection,
) (RawSnapshot, error) {
	raw, err := r.reader.ReadStatusSnapshotFiltered(ctx, asOf, selection)
	if err != nil {
		return RawSnapshot{}, err
	}
	raw.SemanticExtraction.ProviderProfiles = cloneSemanticProviderProfiles(r.profiles)
	return raw, nil
}

func cloneSemanticProviderProfiles(rows []SemanticProviderProfileStatus) []SemanticProviderProfileStatus {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]SemanticProviderProfileStatus, 0, len(rows))
	for _, row := range rows {
		normalized := normalizeSemanticProviderProfile(row)
		if normalized.ProfileID == "" {
			continue
		}
		cloned = append(cloned, normalized)
	}
	sort.Slice(cloned, func(i, j int) bool {
		return cloned[i].ProfileID < cloned[j].ProfileID
	})
	return cloned
}

func normalizeSemanticProviderProfile(row SemanticProviderProfileStatus) SemanticProviderProfileStatus {
	state := strings.TrimSpace(row.State)
	if !isSemanticProviderProfileState(state) {
		if row.CredentialConfigured {
			state = SemanticProviderProfileConfigured
		} else {
			state = SemanticProviderProfileUnconfigured
		}
	}

	sourceClasses := normalizeSemanticSourceClasses(row.SourceClasses)
	out := SemanticProviderProfileStatus{
		ProfileID:              strings.TrimSpace(row.ProfileID),
		DisplayName:            strings.TrimSpace(row.DisplayName),
		ProviderKind:           strings.TrimSpace(row.ProviderKind),
		CredentialSourceKind:   strings.TrimSpace(row.CredentialSourceKind),
		CredentialConfigured:   row.CredentialConfigured,
		ModelID:                strings.TrimSpace(row.ModelID),
		EmbeddingDimensions:    row.EmbeddingDimensions,
		EndpointProfileID:      strings.TrimSpace(row.EndpointProfileID),
		SourceClasses:          sourceClasses,
		SourcePolicyConfigured: row.SourcePolicyConfigured,
		State:                  state,
		Reason:                 strings.TrimSpace(row.Reason),
		Detail:                 strings.TrimSpace(row.Detail),
		UpdatedAt:              row.UpdatedAt,
	}
	if out.Reason == "" {
		out.Reason = defaultSemanticProviderProfileReason(out)
	}
	return out
}

func normalizeSemanticSourceClasses(sourceClasses []string) []string {
	if len(sourceClasses) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(sourceClasses))
	normalized := make([]string, 0, len(sourceClasses))
	for _, sourceClass := range sourceClasses {
		sourceClass = strings.TrimSpace(sourceClass)
		if sourceClass == "" {
			continue
		}
		if _, ok := seen[sourceClass]; ok {
			continue
		}
		seen[sourceClass] = struct{}{}
		normalized = append(normalized, sourceClass)
	}
	sort.Strings(normalized)
	return normalized
}

func isSemanticProviderProfileState(state string) bool {
	return slices.Contains(semanticProviderProfileStates, state)
}

func defaultSemanticProviderProfileReason(row SemanticProviderProfileStatus) string {
	switch row.State {
	case SemanticProviderProfileHealthy:
		return "provider_profile_healthy"
	case SemanticProviderProfileUnhealthy:
		return "provider_profile_unhealthy"
	case SemanticProviderProfileUnconfigured:
		return "credential_not_configured"
	default:
		if !row.SourcePolicyConfigured {
			return "source_policy_not_configured"
		}
		return "provider_profile_configured"
	}
}

func semanticProfileConfigured(row SemanticProviderProfileStatus) bool {
	switch row.State {
	case SemanticProviderProfileConfigured, SemanticProviderProfileHealthy, SemanticProviderProfileUnhealthy:
		return row.CredentialConfigured
	default:
		return false
	}
}

func semanticProfileUnhealthy(row SemanticProviderProfileStatus) bool {
	return row.State == SemanticProviderProfileUnhealthy
}

func semanticProfileAllowsSource(row SemanticProviderProfileStatus, sourceClass string) bool {
	if row.State == SemanticProviderProfileUnhealthy ||
		!semanticProfileConfigured(row) ||
		!row.SourcePolicyConfigured {
		return false
	}
	return slices.Contains(row.SourceClasses, sourceClass)
}
