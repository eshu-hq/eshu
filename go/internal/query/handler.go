// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]any{
		"error":  http.StatusText(status),
		"detail": message,
	})
}

func WriteSuccess(w http.ResponseWriter, r *http.Request, status int, data any, truth *TruthEnvelope) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data:  data,
			Truth: truth,
			Error: nil,
		})
		return
	}
	WriteJSON(w, status, data)
}

// WriteErrorEnvelope writes an error response using the same envelope/plain split
// as WriteSuccess: a ResponseEnvelope wrapping the error for envelope-accepting
// callers, and the plain error message otherwise. It lets sibling packages return
// a canonical query error (with its code and details) without re-implementing the
// content negotiation.
func WriteErrorEnvelope(w http.ResponseWriter, r *http.Request, status int, errEnv *ErrorEnvelope) {
	if errEnv == nil {
		WriteError(w, status, http.StatusText(status))
		return
	}
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Error: errEnv})
		return
	}
	WriteError(w, status, errEnv.Message)
}

func WriteContractError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	message string,
	errCode ErrorCode,
	capability string,
	currentProfile QueryProfile,
	requiredProfile QueryProfile,
) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data: nil,
			Error: &ErrorEnvelope{
				Code:       errCode,
				Message:    message,
				Capability: capability,
				Profiles: &ErrorProfiles{
					Current:  currentProfile,
					Required: requiredProfile,
				},
			},
		})
		return
	}
	WriteError(w, status, message)
}

// ReadJSON decodes a JSON request body into v.
func ReadJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is required")
	}
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// QueryParam returns a trimmed query parameter value.
func QueryParam(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

// QueryParamInt returns a query parameter as int with a default.
func QueryParamInt(r *http.Request, key string, defaultVal int) int {
	raw := QueryParam(r, key)
	if raw == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	return n
}

// PathParam extracts a path segment by position from a ServeMux pattern.
// For routes like "/api/v0/repositories/{repo_id}/context", use PathParam(r, "repo_id").
func PathParam(r *http.Request, name string) string {
	return strings.TrimSpace(r.PathValue(name))
}

func capabilityUnsupported(profile QueryProfile, capability string) bool {
	return maxTruthLevel(capability, profile) == nil
}

// requireContextOverview writes the structured unsupported-capability envelope
// and returns false when the profile cannot serve
// platform_impact.context_overview, the shared capability behind the repository,
// service, and workload context, story, summary, dossier, and investigation
// readbacks. message names the specific surface so the operator sees which call
// needs an authoritative platform profile. Keeping these surfaces behind one
// gate keeps the capability catalog's "unsupported in local_lightweight" claim
// truthful instead of letting a graph-less profile fall through to a panic.
func requireContextOverview(w http.ResponseWriter, r *http.Request, profile QueryProfile, message string) bool {
	const capability = "platform_impact.context_overview"
	if capabilityUnsupported(profile, capability) {
		WriteContractError(w, r, http.StatusNotImplemented, message,
			"unsupported_capability", capability, profile, requiredProfile(capability))
		return false
	}
	return true
}

// APIRouter builds the top-level /api/v0 mux for all query endpoints.
type APIRouter struct {
	Repositories                 *RepositoryHandler
	Entities                     *EntityHandler
	Code                         *CodeHandler
	Content                      *ContentHandler
	Infra                        *InfraHandler
	GraphEntityInventory         *GraphEntityInventoryHandler
	CloudInventory               *CloudInventoryHandler
	CloudRuntimeDrift            *CloudRuntimeDriftHandler
	IaC                          *IaCHandler
	Impact                       *ImpactHandler
	Evidence                     *EvidenceHandler
	Documentation                *DocumentationHandler
	SemanticEvidence             *SemanticEvidenceHandler
	SemanticSearch               *SemanticSearchHandler
	PackageRegistry              *PackageRegistryHandler
	Dependencies                 *DependenciesHandler
	CodeownersOwnership          *CodeownersOwnershipHandler
	CICD                         *CICDHandler
	ServiceCatalog               *ServiceCatalogHandler
	Kubernetes                   *KubernetesHandler
	SecretsIAM                   *SecretsIAMHandler
	ObservabilityCoverage        *ObservabilityCoverageHandler
	Images                       *ImageHandler
	SupplyChain                  *SupplyChainHandler
	Incident                     *IncidentHandler
	WorkItems                    *WorkItemHandler
	Visualization                *VisualizationHandler
	Freshness                    *FreshnessHandler
	Status                       *StatusHandler
	ComponentExtensions          *ComponentExtensionsHandler
	ExtractionReadiness          *CollectorExtractionReadinessHandler
	FactSchemaVersions           *FactSchemaVersionHandler
	Playbooks                    *QueryPlaybookHandler
	InvestigationWorkflows       *InvestigationWorkflowHandler
	Metrics                      *MetricsHandler
	Capabilities                 *CapabilitiesHandler
	SurfaceInventory             *SurfaceInventoryHandler
	Compare                      *CompareHandler
	AdminDeadLetters             *AdminDeadLetterListHandler
	AdminInputInvalidFacts       *AdminInputInvalidFactListHandler
	Admin                        *AdminHandler
	Ask                          *AskHandler
	Setup                        *SetupHandler
	LocalIdentity                *LocalIdentityHandler
	BrowserSessions              *BrowserSessionHandler
	SessionList                  *BrowserSessionListHandler
	AdminIdentityReads           *AdminIdentityReadHandler
	AdminIdentityMutations       *AdminIdentityMutationHandler
	Profile                      *ProfileHandler
	OIDCLogin                    *OIDCLoginHandler
	SAML                         *SAMLHandler
	GitHubLogin                  *GitHubLoginHandler
	AuthProviders                *AuthProviderListHandler
	AdminProviderConfigReads     *AdminProviderConfigReadHandler
	AdminProviderConfigMutations *AdminProviderConfigMutationHandler
	SignInPolicyReads            *SignInPolicyReadHandler
	SignInPolicyMutations        *SignInPolicyMutationHandler
}

// Mount registers all query-layer HTTP routes on the given mux.
func (a *APIRouter) Mount(mux *http.ServeMux) {
	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// OpenAPI spec
	mux.HandleFunc("GET /api/v0/openapi.json", ServeOpenAPI)
	mux.HandleFunc("GET /api/v0/docs", ServeSwaggerUI)
	mux.HandleFunc("GET /api/v0/redoc", ServeReDoc)

	// First-run setup wizard (#4965). Mounted before LocalIdentity so the
	// route table's precedence reads the same as the login-vs-setup story.
	if a.Setup != nil {
		a.Setup.Mount(mux)
	}

	// Browser sessions
	if a.LocalIdentity != nil {
		a.LocalIdentity.Mount(mux)
	}
	if a.BrowserSessions != nil {
		a.BrowserSessions.Mount(mux)
	}
	if a.SessionList != nil {
		a.SessionList.Mount(mux)
	}
	if a.AdminIdentityReads != nil {
		a.AdminIdentityReads.Mount(mux)
	}
	if a.AdminIdentityMutations != nil {
		a.AdminIdentityMutations.Mount(mux)
	}
	if a.AdminProviderConfigReads != nil {
		a.AdminProviderConfigReads.Mount(mux)
	}
	if a.AdminProviderConfigMutations != nil {
		a.AdminProviderConfigMutations.Mount(mux)
	}
	if a.SignInPolicyReads != nil {
		a.SignInPolicyReads.Mount(mux)
	}
	if a.SignInPolicyMutations != nil {
		a.SignInPolicyMutations.Mount(mux)
	}
	if a.Profile != nil {
		a.Profile.Mount(mux)
	}
	if a.OIDCLogin != nil {
		a.OIDCLogin.Mount(mux)
	}
	if a.SAML != nil {
		a.SAML.Mount(mux)
	}
	if a.GitHubLogin != nil {
		a.GitHubLogin.Mount(mux)
	}
	if a.AuthProviders != nil {
		a.AuthProviders.Mount(mux)
	}

	// Repositories
	if a.Repositories != nil {
		a.Repositories.Mount(mux)
	}

	// Entities
	if a.Entities != nil {
		a.Entities.Mount(mux)
	}

	// Code
	if a.Code != nil {
		a.Code.Mount(mux)
	}

	// Content
	if a.Content != nil {
		a.Content.Mount(mux)
	}

	// Infra
	if a.Infra != nil {
		a.Infra.Mount(mux)
	}

	// Graph entity inventory (browsable Nodes page)
	if a.GraphEntityInventory != nil {
		a.GraphEntityInventory.Mount(mux)
	}

	// Cloud inventory readback (canonical reducer_cloud_resource_identity rows)
	if a.CloudRuntimeDrift != nil {
		a.CloudRuntimeDrift.Mount(mux)
	}
	if a.CloudInventory != nil {
		a.CloudInventory.Mount(mux)
	}

	// IaC
	if a.IaC != nil {
		a.IaC.Mount(mux)
	}

	// Impact
	if a.Impact != nil {
		a.Impact.Mount(mux)
	}

	// Evidence
	if a.Evidence != nil {
		a.Evidence.Mount(mux)
	}

	// Documentation
	if a.Documentation != nil {
		a.Documentation.Mount(mux)
	}

	// Semantic evidence
	if a.SemanticEvidence != nil {
		a.SemanticEvidence.Mount(mux)
	}

	// Semantic search
	if a.SemanticSearch != nil {
		a.SemanticSearch.Mount(mux)
	}

	// Package registry
	if a.PackageRegistry != nil {
		a.PackageRegistry.Mount(mux)
	}

	// Dependency inventory
	if a.Dependencies != nil {
		a.Dependencies.Mount(mux)
	}

	// Codeowners ownership
	if a.CodeownersOwnership != nil {
		a.CodeownersOwnership.Mount(mux)
	}

	// CI/CD
	if a.CICD != nil {
		a.CICD.Mount(mux)
	}

	// Service catalog
	if a.ServiceCatalog != nil {
		a.ServiceCatalog.Mount(mux)
	}

	// Kubernetes
	if a.Kubernetes != nil {
		a.Kubernetes.Mount(mux)
	}

	// Secrets / IAM posture
	if a.SecretsIAM != nil {
		a.SecretsIAM.Mount(mux)
	}

	// Observability coverage
	if a.ObservabilityCoverage != nil {
		a.ObservabilityCoverage.Mount(mux)
	}

	// Container images (OCI)
	if a.Images != nil {
		a.Images.Mount(mux)
	}

	// Supply chain
	if a.SupplyChain != nil {
		a.SupplyChain.Mount(mux)
	}

	// Incident context
	if a.Incident != nil {
		a.Incident.Mount(mux)
	}

	// Work items
	if a.WorkItems != nil {
		a.WorkItems.Mount(mux)
	}

	// Visualization packets
	if a.Visualization != nil {
		a.Visualization.Mount(mux)
	}

	// Freshness drilldowns
	if a.Freshness != nil {
		a.Freshness.Mount(mux)
	}

	// Status
	if a.Status != nil {
		a.Status.Mount(mux)
	}

	// Component extensions
	if a.ComponentExtensions != nil {
		a.ComponentExtensions.Mount(mux)
	}
	if a.ExtractionReadiness != nil {
		a.ExtractionReadiness.Mount(mux)
	}
	if a.FactSchemaVersions != nil {
		a.FactSchemaVersions.Mount(mux)
	}

	// Query playbooks
	if a.Playbooks != nil {
		a.Playbooks.Mount(mux)
	}
	if a.InvestigationWorkflows != nil {
		a.InvestigationWorkflows.Mount(mux)
	}

	// Metrics
	if a.Metrics != nil {
		a.Metrics.Mount(mux)
	}

	// Capabilities
	if a.Capabilities != nil {
		a.Capabilities.Mount(mux)
	}

	// Surface inventory
	if a.SurfaceInventory != nil {
		a.SurfaceInventory.Mount(mux)
	}

	// Compare
	if a.Compare != nil {
		a.Compare.Mount(mux)
	}

	// Read-only admin surfaces
	if a.AdminDeadLetters != nil {
		a.AdminDeadLetters.Mount(mux)
	}
	if a.AdminInputInvalidFacts != nil {
		a.AdminInputInvalidFacts.Mount(mux)
	}

	// Admin
	if a.Admin != nil {
		a.Admin.Mount(mux)
	}

	// Ask Eshu (default-off; nil Asker returns 503 unavailable)
	if a.Ask != nil {
		a.Ask.Mount(mux)
	}
}
