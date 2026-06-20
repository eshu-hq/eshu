package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const (
	componentExtensionsInventoryCapability   = "component_extensions.inventory"
	componentExtensionsDiagnosticsCapability = "component_extensions.diagnostics"
	componentExtensionsSchemaVersion         = "eshu.component_extensions.v1"
	componentExtensionsDefaultLimit          = 100
	componentExtensionsMaxLimit              = 500
)

// ComponentExtensionsHandler exposes local component registry readback through
// the query API without leaking server-local manifest or activation paths.
type ComponentExtensionsHandler struct {
	ComponentHome string
	Policy        component.Policy
	Profile       QueryProfile
}

// ComponentExtensionInventoryResponse is the API response for extension
// inventory readback.
type ComponentExtensionInventoryResponse struct {
	SchemaVersion           string                         `json:"schema_version"`
	Status                  string                         `json:"status"`
	ComponentHomeConfigured bool                           `json:"component_home_configured"`
	Components              []ComponentExtensionComponent  `json:"components"`
	Count                   int                            `json:"count"`
	TotalCount              int                            `json:"total_count"`
	Limit                   int                            `json:"limit"`
	Truncated               bool                           `json:"truncated"`
	Policy                  ComponentExtensionPolicyStatus `json:"policy"`
}

// ComponentExtensionDiagnosticsResponse is the API response for one extension
// diagnostics drilldown.
type ComponentExtensionDiagnosticsResponse struct {
	SchemaVersion           string                         `json:"schema_version"`
	Status                  string                         `json:"status"`
	ComponentHomeConfigured bool                           `json:"component_home_configured"`
	Component               ComponentExtensionComponent    `json:"component"`
	Policy                  ComponentExtensionPolicyStatus `json:"policy"`
}

// ComponentExtensionComponent is a sanitized component package readback row.
type ComponentExtensionComponent struct {
	ID                    string                                  `json:"id"`
	Name                  string                                  `json:"name,omitempty"`
	Publisher             string                                  `json:"publisher,omitempty"`
	Version               string                                  `json:"version,omitempty"`
	ManifestDigest        string                                  `json:"manifest_digest,omitempty"`
	Verified              bool                                    `json:"verified,omitempty"`
	TrustMode             string                                  `json:"trust_mode,omitempty"`
	InstalledAt           string                                  `json:"installed_at,omitempty"`
	States                []string                                `json:"states,omitempty"`
	Activations           []ComponentExtensionActivation          `json:"activations,omitempty"`
	Diagnostics           *ComponentExtensionPolicyDiagnostics    `json:"diagnostics,omitempty"`
	TrustDecision         ComponentExtensionTrustDecision         `json:"trust_decision"`
	PolicyGate            ComponentExtensionPolicyGate            `json:"policy_gate"`
	LastConformanceProof  ComponentExtensionConformanceProof      `json:"last_conformance_proof"`
	SchedulerState        ComponentExtensionSchedulerState        `json:"scheduler_state"`
	ReadModelAvailability ComponentExtensionReadModelAvailability `json:"read_model_availability"`
	Error                 *ComponentExtensionError                `json:"error,omitempty"`
}

// ComponentExtensionActivation is a sanitized activation readback row.
type ComponentExtensionActivation struct {
	InstanceID    string `json:"instance_id"`
	Mode          string `json:"mode"`
	ClaimsEnabled bool   `json:"claims_enabled"`
	ConfigHandle  string `json:"config_handle,omitempty"`
	EnabledAt     string `json:"enabled_at,omitempty"`
}

// ComponentExtensionPolicyDiagnostics reports local policy re-verification
// without exposing registry file paths or activation config paths.
type ComponentExtensionPolicyDiagnostics struct {
	PolicyConfigured bool                `json:"policy_configured"`
	PolicyAllowed    bool                `json:"policy_allowed"`
	PolicyMode       string              `json:"policy_mode,omitempty"`
	PolicyCode       component.ErrorCode `json:"policy_code,omitempty"`
	PolicyReason     string              `json:"policy_reason,omitempty"`
}

// ComponentExtensionTrustDecision classifies whether the installed package is
// currently trusted by the configured hosted policy.
type ComponentExtensionTrustDecision struct {
	Decision string              `json:"decision"`
	Code     component.ErrorCode `json:"code,omitempty"`
	Reason   string              `json:"reason,omitempty"`
}

// ComponentExtensionPolicyGate gives clients a stable blocker class without
// exposing local manifest paths or provider credentials.
type ComponentExtensionPolicyGate struct {
	State string              `json:"state"`
	Mode  string              `json:"mode,omitempty"`
	Code  component.ErrorCode `json:"code,omitempty"`
}

// ComponentExtensionConformanceProof reports the last known proof state for a
// component extension.
type ComponentExtensionConformanceProof struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// ComponentExtensionSchedulerState reports whether hosted scheduling can claim
// work for the component activation.
type ComponentExtensionSchedulerState struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// ComponentExtensionReadModelAvailability reports whether API/MCP readback can
// expect component-emitted facts to have materialized.
type ComponentExtensionReadModelAvailability struct {
	State             string `json:"state"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

// ComponentExtensionPolicyStatus summarizes which trust policy knobs were
// configured without echoing private filesystem paths.
type ComponentExtensionPolicyStatus struct {
	Mode                       string `json:"mode,omitempty"`
	AllowIDsConfigured         bool   `json:"allow_ids_configured"`
	AllowPublishersConfigured  bool   `json:"allow_publishers_configured"`
	RevokeIDsConfigured        bool   `json:"revoke_ids_configured"`
	RevokePublishersConfigured bool   `json:"revoke_publishers_configured"`
	CoreVersionConfigured      bool   `json:"core_version_configured"`
}

// ComponentExtensionError is a stable sanitized component error.
type ComponentExtensionError struct {
	Code    component.ErrorCode `json:"code,omitempty"`
	Message string              `json:"message,omitempty"`
}

// Mount registers component extension inventory and diagnostics routes.
func (h *ComponentExtensionsHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/component-extensions", h.listComponentExtensions)
	mux.HandleFunc("GET /api/v0/component-extensions/{component_id}/diagnostics", h.getComponentExtensionDiagnostics)
}

func (h *ComponentExtensionsHandler) listComponentExtensions(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.inventoryLimit(w, r)
	if !ok {
		return
	}
	readback, ok := h.readbackOrUnavailable(w, r, componentExtensionsInventoryCapability)
	if !ok {
		return
	}
	components := sanitizedComponentExtensions(readback)
	totalCount := len(components)
	truncated := totalCount > limit
	if truncated {
		components = components[:limit]
	}
	response := ComponentExtensionInventoryResponse{
		SchemaVersion:           componentExtensionsSchemaVersion,
		Status:                  "available",
		ComponentHomeConfigured: true,
		Components:              components,
		Count:                   len(components),
		TotalCount:              totalCount,
		Limit:                   limit,
		Truncated:               truncated,
		Policy:                  h.policyStatus(),
	}
	WriteSuccess(w, r, http.StatusOK, response, h.truth(componentExtensionsInventoryCapability))
}

func (h *ComponentExtensionsHandler) getComponentExtensionDiagnostics(w http.ResponseWriter, r *http.Request) {
	componentID := strings.TrimSpace(r.PathValue("component_id"))
	if componentID == "" {
		WriteContractError(
			w,
			r,
			http.StatusBadRequest,
			"component_id is required",
			ErrorCodeInvalidArgument,
			componentExtensionsDiagnosticsCapability,
			h.profile(),
			h.profile(),
		)
		return
	}
	readback, ok := h.readbackOrUnavailable(w, r, componentExtensionsDiagnosticsCapability)
	if !ok {
		return
	}
	components := sanitizedComponentExtensions(readback)
	for _, candidate := range components {
		if candidate.ID != componentID {
			continue
		}
		response := ComponentExtensionDiagnosticsResponse{
			SchemaVersion:           componentExtensionsSchemaVersion,
			Status:                  "available",
			ComponentHomeConfigured: true,
			Component:               candidate,
			Policy:                  h.policyStatus(),
		}
		WriteSuccess(w, r, http.StatusOK, response, h.truth(componentExtensionsDiagnosticsCapability))
		return
	}
	WriteContractError(
		w,
		r,
		http.StatusNotFound,
		"component extension is not installed",
		ErrorCodeNotFound,
		componentExtensionsDiagnosticsCapability,
		h.profile(),
		h.profile(),
	)
}

func (h *ComponentExtensionsHandler) readbackOrUnavailable(
	w http.ResponseWriter,
	r *http.Request,
	capability string,
) ([]component.RegistryReadbackComponent, bool) {
	if strings.TrimSpace(h.ComponentHome) == "" {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"component extension registry is unavailable: set ESHU_COMPONENT_HOME on this runtime",
			ErrorCodeComponentRegistryUnavailable,
			capability,
			h.profile(),
			h.profile(),
		)
		return nil, false
	}
	readback, err := component.NewRegistry(h.ComponentHome).Readback(h.Policy)
	if err != nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"component extension registry readback failed",
			ErrorCodeComponentRegistryUnavailable,
			capability,
			h.profile(),
			h.profile(),
		)
		return nil, false
	}
	return readback, true
}

func (h *ComponentExtensionsHandler) truth(capability string) *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		capability,
		TruthBasisRuntimeState,
		"resolved from local component registry readback and configured trust policy",
	)
}

func (h *ComponentExtensionsHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *ComponentExtensionsHandler) policyStatus() ComponentExtensionPolicyStatus {
	return ComponentExtensionPolicyStatus{
		Mode:                       h.Policy.Mode,
		AllowIDsConfigured:         len(h.Policy.AllowedIDs) > 0,
		AllowPublishersConfigured:  len(h.Policy.AllowedPublishers) > 0,
		RevokeIDsConfigured:        len(h.Policy.RevokedIDs) > 0,
		RevokePublishersConfigured: len(h.Policy.RevokedPublishers) > 0,
		CoreVersionConfigured:      strings.TrimSpace(h.Policy.CoreVersion) != "",
	}
}

func (h *ComponentExtensionsHandler) inventoryLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(QueryParam(r, "limit"))
	if raw == "" {
		return componentExtensionsDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > componentExtensionsMaxLimit {
		WriteContractError(
			w,
			r,
			http.StatusBadRequest,
			fmt.Sprintf("limit must be between 1 and %d", componentExtensionsMaxLimit),
			ErrorCodeInvalidArgument,
			componentExtensionsInventoryCapability,
			h.profile(),
			h.profile(),
		)
		return 0, false
	}
	return limit, true
}

func sanitizedComponentExtensions(readback []component.RegistryReadbackComponent) []ComponentExtensionComponent {
	out := make([]ComponentExtensionComponent, 0, len(readback))
	for _, entry := range readback {
		row := ComponentExtensionComponent{
			ID:             entry.ID,
			Name:           entry.Name,
			Publisher:      entry.Publisher,
			Version:        entry.Version,
			ManifestDigest: entry.ManifestDigest,
			Verified:       entry.Verified,
			TrustMode:      entry.TrustMode,
			InstalledAt:    formatComponentExtensionTime(entry.InstalledAt),
			States:         append([]string(nil), entry.States...),
			Activations:    sanitizedComponentExtensionActivations(entry),
			Diagnostics:    componentExtensionDiagnostics(entry),
			TrustDecision:  componentExtensionTrustDecision(entry),
			PolicyGate:     componentExtensionPolicyGate(entry),
			LastConformanceProof: ComponentExtensionConformanceProof{
				Status: "missing",
				Reason: "missing_conformance_proof",
			},
			SchedulerState: componentExtensionSchedulerState(entry),
			ReadModelAvailability: componentExtensionReadModelAvailability(
				entry,
			),
			Error: componentExtensionError(entry.Error),
		}
		out = append(out, row)
	}
	return out
}

func sanitizedComponentExtensionActivations(
	entry component.RegistryReadbackComponent,
) []ComponentExtensionActivation {
	activations := make([]ComponentExtensionActivation, 0, len(entry.Activations))
	for _, activation := range entry.Activations {
		out := ComponentExtensionActivation{
			InstanceID:    activation.InstanceID,
			Mode:          activation.Mode,
			ClaimsEnabled: activation.ClaimsEnabled,
			EnabledAt:     formatComponentExtensionTime(activation.EnabledAt),
		}
		if strings.TrimSpace(activation.ConfigPath) != "" {
			out.ConfigHandle = component.ActivationConfigHandle(entry.ID, entry.Version, activation)
		}
		activations = append(activations, out)
	}
	return activations
}

func componentExtensionDiagnostics(
	entry component.RegistryReadbackComponent,
) *ComponentExtensionPolicyDiagnostics {
	if entry.Verification == nil && entry.Error == nil {
		return nil
	}
	diagnostics := &ComponentExtensionPolicyDiagnostics{
		PolicyConfigured: entry.Verification != nil,
	}
	if entry.Verification != nil {
		diagnostics.PolicyAllowed = entry.Verification.Allowed
		diagnostics.PolicyMode = entry.Verification.Mode
		diagnostics.PolicyCode = entry.Verification.Code
		diagnostics.PolicyReason = entry.Verification.Reason
		return diagnostics
	}
	if entry.Error != nil {
		diagnostics.PolicyCode = entry.Error.Code
		diagnostics.PolicyReason = safeComponentExtensionErrorMessage(entry.Error.Code)
	}
	return diagnostics
}

func componentExtensionError(summary *component.ErrorSummary) *ComponentExtensionError {
	if summary == nil {
		return nil
	}
	return &ComponentExtensionError{Code: summary.Code, Message: safeComponentExtensionErrorMessage(summary.Code)}
}

func safeComponentExtensionErrorMessage(code component.ErrorCode) string {
	switch code {
	case component.ErrorCodeInvalidManifest:
		return "component manifest is invalid or unavailable"
	case component.ErrorCodeIncompatibleCore:
		return "component is incompatible with this Eshu runtime"
	case component.ErrorCodeRevokedPackage:
		return "component package is revoked by policy"
	case component.ErrorCodeUntrustedPublisher:
		return "component publisher is not trusted by policy"
	default:
		return "component extension readback failed"
	}
}

func formatComponentExtensionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
