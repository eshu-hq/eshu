// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Sentinel errors an MutationStore implementation (in
// cmd/api, backed by the postgres package) may return so this handler can map
// them to the right HTTP status without importing storage/postgres or
// secretcrypto directly — this package stays layered above both.
var (
	// ErrDuplicateKey mirrors
	// postgres.ErrProviderConfigDuplicateKey: a live provider already exists
	// for this tenant/kind/key.
	ErrDuplicateKey = errors.New("admin provider config: already exists for this tenant/kind/key")
	// ErrKeyringUnavailable mirrors
	// postgres.ErrProviderSecretKeyringUnavailable: no DEK is configured, so a
	// write carrying a secret cannot be sealed.
	ErrKeyringUnavailable = errors.New("admin provider config: encryption key is not configured")
	// ErrRevisionNotFound mirrors
	// postgres.ErrProviderConfigRevisionNotFound: a revert target revision
	// does not belong to the provider config.
	ErrRevisionNotFound = errors.New("admin provider config: revision not found")
	// ErrKindMismatch mirrors
	// postgres.ErrProviderConfigKindMismatch: an update request's
	// provider_kind disagrees with the existing provider config's immutable
	// kind.
	ErrKindMismatch = errors.New("admin provider config: provider_kind does not match the existing provider config")
	// ErrRevisionChanged mirrors
	// postgres.ErrProviderConfigRevisionChanged: the provider config's active
	// revision changed (via a concurrent Update/Revert) between the
	// test-connection call and the Enable call.
	ErrRevisionChanged = errors.New("admin provider config: active revision changed since it was tested")
	// ErrManagedByEnvironment is returned by
	// MutationStore implementations (never by this
	// package directly — go/internal/query has no knowledge of env/file
	// registration) when the target provider_config_id is registered via
	// env/file config (ESHU_AUTH_OIDC_CONFIG_FILE, ESHU_SAML_PROVIDERS_JSON),
	// whether as a pure env-only provider or a DB row shadowed by one.
	// Update, Revert, Enable, and Disable all reject with this; Create does
	// not (see CreateRequest's ProviderConfigID doc
	// comment for why creating a shadow row is intentionally allowed).
	ErrManagedByEnvironment = errors.New("admin provider config: managed by environment; edit in your IaC, not here")
)

// MutationHandler serves the DB-backed identity
// provider-config CRUD write endpoints under the identity_admin capability
// family (#4966, epic #4962). Every route requires all-scope admin
// authentication, writes strictly within the caller's own tenant, and emits a
// governance audit event. Client secrets and SAML signing material are
// write-only: the JSON request carries them in plaintext (over TLS, like
// every other credential this API accepts), the handler builds a JSON secret
// blob and passes it to the store to seal, and no response, log line, or
// audit event ever carries the plaintext or the sealed ciphertext.
type MutationHandler struct {
	Store  MutationStore
	Tester ConnectionTester
	Audit  audit.Appender
	Now    func() time.Time
	// ReadStore backs the enable-time login-readiness guard (issue #5604): it
	// is used ONLY to re-read the provider config's stored non-secret
	// configuration so handleStatusChange can check for fields
	// ResolveSealedProviderConfig requires for login (redirect_url, and for
	// SAML also the SP endpoint / metadata_xml fields) but that are optional
	// at create/test-connection time — see
	// readiness.go. It is deliberately optional:
	// a nil ReadStore (or a lookup that errors or finds nothing) makes this
	// secondary guard a no-op rather than a new hard dependency for the
	// primary enable path, which is still gated by the mandatory
	// test-connection pass above. In production this is always wired
	// alongside Store, backed by the same database as
	// ReadHandler.Store (see cmd/api/wiring.go).
	ReadStore ReadStore
}

// Mount registers the admin provider-config mutation routes.
func (h *MutationHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/auth/admin/provider-configs", h.handleCreate)
	mux.HandleFunc("POST /api/v0/auth/admin/provider-configs/{provider_config_id}", h.handleUpdate)
	mux.HandleFunc("POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert", h.handleRevert)
	mux.HandleFunc("POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable", h.handleEnable)
	mux.HandleFunc("POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable", h.handleDisable)
	mux.HandleFunc("POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection", h.handleTestConnection)
}

// storeReady reports whether h.Store is usable, auditing the denial before
// writing 503 when it is not — matching handleTestConnection's identical
// nil-Tester audit pattern (every allowed and denied provider-config attempt
// must be governance-audited). Guarded for h==nil (h.audit is itself
// nil-safe, but h.audit(r, ...) cannot even be called on a nil receiver
// without evaluating h first) so a nil handler still degrades to a plain 503
// rather than panicking.
func (h *MutationHandler) storeReady(
	w http.ResponseWriter,
	r *http.Request,
	eventType governanceaudit.EventType,
) bool {
	if h == nil || h.Store == nil {
		if h != nil {
			h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_store_unavailable", "")
		}
		querycontract.WriteError(w, http.StatusServiceUnavailable, "admin provider config mutation store is unavailable")
		return false
	}
	return true
}

func (h *MutationHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *MutationHandler) adminScope(
	w http.ResponseWriter,
	r *http.Request,
	eventType governanceaudit.EventType,
) (tenantID string, ok bool) {
	auth, found := queryauth.AuthContextFromContext(r.Context())
	auth = queryauth.NormalizeAuthContext(auth)
	if !found || !auth.AllScopes {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "admin_scope_required", "")
		querycontract.WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return "", false
	}
	if auth.TenantID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "admin_tenant_required", "")
		querycontract.WriteError(w, http.StatusForbidden, "admin tenant scope is required")
		return "", false
	}
	return auth.TenantID, true
}

func (h *MutationHandler) requirePermission(
	w http.ResponseWriter, r *http.Request, eventType governanceaudit.EventType, capability string,
) bool {
	if queryauth.AllowsPermissionFeature(r.Context(), queryauth.PermissionFeatureIdentityAdmin) {
		return true
	}
	h.audit(r, eventType, governanceaudit.DecisionDenied, "permission_catalog_denied", "")
	audit.WritePermissionDeniedEnvelope(w, capability)
	return false
}

func (h *MutationHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	const eventType = governanceaudit.EventTypeIDPConfigChange
	if !h.storeReady(w, r, eventType) {
		return
	}
	if !h.requirePermission(w, r, eventType, "identity_admin.provider_config_create") {
		return
	}
	tenantID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	var body providerConfigWriteRequest
	if err := querycontract.ReadJSON(r, &body); err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_invalid_request", "")
		querycontract.WriteError(w, http.StatusBadRequest, "invalid provider config request")
		return
	}
	built, err := buildProviderConfigWrite(body)
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_validation_failed", "")
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	providerConfigID := strings.TrimSpace(body.ProviderConfigID)
	if providerConfigID == "" {
		var err error
		providerConfigID, err = newProviderConfigID()
		if err != nil {
			h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_id_generation_failed", "")
			querycontract.WriteError(w, http.StatusInternalServerError, "failed to generate provider config id")
			return
		}
	}
	revisionID, err := newProviderConfigRevisionID()
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_revision_id_generation_failed", "")
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to generate provider config revision id")
		return
	}

	result, err := h.Store.CreateProviderConfig(r.Context(), CreateRequest{
		ProviderConfigID:  providerConfigID,
		TenantID:          tenantID,
		ProviderKind:      built.kind,
		ProviderKeyHash:   built.keyHash,
		IssuerHash:        built.issuerHash,
		ClientIDHash:      built.clientIDHash,
		MetadataURLHash:   built.metadataURLHash,
		EntityIDHash:      built.entityIDHash,
		RevisionID:        revisionID,
		Configuration:     built.configurationJSON,
		ConfigurationHash: audit.IdentityHash(built.configurationJSON),
		MetadataHash:      audit.IdentityHash(built.metadataForHash),
		PlaintextSecret:   built.secretJSON,
		Now:               h.now(),
	})
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, providerConfigWriteErrorReason(err), "")
		writeProviderConfigWriteError(w, err)
		return
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, "provider_config_created", "")
	querycontract.WriteJSON(w, http.StatusOK, providerConfigWriteResponse(result))
}

func (h *MutationHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	const eventType = governanceaudit.EventTypeIDPConfigChange
	if !h.storeReady(w, r, eventType) {
		return
	}
	if !h.requirePermission(w, r, eventType, "identity_admin.provider_config_update") {
		return
	}
	tenantID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	providerConfigID := strings.TrimSpace(querycontract.PathParam(r, "provider_config_id"))
	if providerConfigID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_id_required", "")
		querycontract.WriteError(w, http.StatusBadRequest, "provider_config_id is required")
		return
	}
	var body providerConfigWriteRequest
	if err := querycontract.ReadJSON(r, &body); err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_invalid_request", "")
		querycontract.WriteError(w, http.StatusBadRequest, "invalid provider config request")
		return
	}
	built, err := buildProviderConfigWrite(body)
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_validation_failed", "")
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	revisionID, err := newProviderConfigRevisionID()
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_revision_id_generation_failed", "")
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to generate provider config revision id")
		return
	}

	result, err := h.Store.UpdateProviderConfig(r.Context(), UpdateRequest{
		ProviderConfigID:  providerConfigID,
		TenantID:          tenantID,
		ProviderKind:      built.kind,
		RevisionID:        revisionID,
		Configuration:     built.configurationJSON,
		ConfigurationHash: audit.IdentityHash(built.configurationJSON),
		MetadataHash:      audit.IdentityHash(built.metadataForHash),
		PlaintextSecret:   built.secretJSON,
		Now:               h.now(),
	})
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, providerConfigWriteErrorReason(err), "")
		writeProviderConfigWriteError(w, err)
		return
	}
	if !result.Found {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_not_found", "")
		querycontract.WriteError(w, http.StatusNotFound, "provider config not found")
		return
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, "provider_config_updated", "")
	querycontract.WriteJSON(w, http.StatusOK, providerConfigWriteResponse(result))
}

func (h *MutationHandler) handleRevert(w http.ResponseWriter, r *http.Request) {
	const eventType = governanceaudit.EventTypeIDPConfigChange
	if !h.storeReady(w, r, eventType) {
		return
	}
	if !h.requirePermission(w, r, eventType, "identity_admin.provider_config_revert") {
		return
	}
	tenantID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	providerConfigID := strings.TrimSpace(querycontract.PathParam(r, "provider_config_id"))
	var body providerConfigRevertRequest
	if err := querycontract.ReadJSON(r, &body); err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_revert_invalid_request", "")
		querycontract.WriteError(w, http.StatusBadRequest, "invalid revert request")
		return
	}
	targetRevisionID := strings.TrimSpace(body.RevisionID)
	if providerConfigID == "" || targetRevisionID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_revert_missing_fields", "")
		querycontract.WriteError(w, http.StatusBadRequest, "provider_config_id and revision_id are required")
		return
	}
	result, err := h.Store.RevertProviderConfig(r.Context(), RevertRequest{
		ProviderConfigID: providerConfigID,
		TenantID:         tenantID,
		TargetRevisionID: targetRevisionID,
		Now:              h.now(),
	})
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, providerConfigWriteErrorReason(err), "")
		writeProviderConfigWriteError(w, err)
		return
	}
	if !result.Found {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_not_found", "")
		querycontract.WriteError(w, http.StatusNotFound, "provider config not found")
		return
	}
	reason := "provider_config_reverted"
	if !result.Changed {
		reason = "provider_config_revert_noop"
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, reason, "")
	querycontract.WriteJSON(w, http.StatusOK, providerConfigWriteResponse(result))
}

func (h *MutationHandler) handleEnable(w http.ResponseWriter, r *http.Request) {
	h.handleStatusChange(w, r, "identity_admin.provider_config_enable", true)
}

func (h *MutationHandler) handleDisable(w http.ResponseWriter, r *http.Request) {
	h.handleStatusChange(w, r, "identity_admin.provider_config_disable", false)
}

// handleStatusChange implements enable/disable. Enable requires a passing
// test-connection result for the CURRENT active revision, re-run
// synchronously at enable time rather than trusted from a prior call — this
// is the "provider cannot be enabled without a passing test" gate, enforced
// here rather than by persisting a trust flag (see the package doc comment
// on why no new column tracks test state).
//
// The test-connection call and the EnableProviderConfig call are still two
// separate calls, but they are no longer allowed to disagree: the tested
// revision id (testResult.RevisionID) is passed to EnableProviderConfig as a
// required compare-and-swap guard. EnableProviderConfig row-locks the
// provider config and rejects (ErrRevisionChanged, mapped
// to 409) if a concurrent Update or Revert changed the active revision
// between the test and this call — so Enable can never activate a revision
// that was not the one just tested.
func (h *MutationHandler) handleStatusChange(w http.ResponseWriter, r *http.Request, capability string, enable bool) {
	const eventType = governanceaudit.EventTypeIDPConfigChange
	if !h.storeReady(w, r, eventType) {
		return
	}
	if !h.requirePermission(w, r, eventType, capability) {
		return
	}
	tenantID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	providerConfigID := strings.TrimSpace(querycontract.PathParam(r, "provider_config_id"))
	if providerConfigID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_id_required", "")
		querycontract.WriteError(w, http.StatusBadRequest, "provider_config_id is required")
		return
	}
	var testedRevisionID string
	if enable {
		if h.Tester == nil {
			h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_connection_tester_unavailable", "")
			querycontract.WriteError(w, http.StatusServiceUnavailable, "connection test capability is unavailable")
			return
		}
		testResult, err := h.Tester.TestProviderConnection(r.Context(), providerConfigID, tenantID)
		if err != nil || !testResult.OK {
			h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_enable_test_failed", "")
			querycontract.WriteError(w, http.StatusBadRequest, "provider cannot be enabled: connection test did not pass")
			return
		}
		testedRevisionID = testResult.RevisionID

		if missingField, ok := h.loginReadinessGap(r, providerConfigID, tenantID); ok {
			h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_enable_missing_login_field", "")
			querycontract.WriteError(w, http.StatusBadRequest, "provider cannot be enabled: "+missingField+
				" is required to resolve this provider for login (it is optional at create/test-connection time, but every login attempt would 503 without it)")
			return
		}
	}
	var result WriteResult
	var err error
	if enable {
		result, err = h.Store.EnableProviderConfig(r.Context(), providerConfigID, tenantID, testedRevisionID)
	} else {
		result, err = h.Store.DisableProviderConfig(r.Context(), providerConfigID, tenantID)
	}
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, providerConfigWriteErrorReason(err), "")
		writeProviderConfigWriteError(w, err)
		return
	}
	if !result.Found {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_not_found", "")
		querycontract.WriteError(w, http.StatusNotFound, "provider config not found")
		return
	}
	reason := "provider_config_status_changed"
	h.audit(r, eventType, governanceaudit.DecisionAllowed, reason, "")
	querycontract.WriteJSON(w, http.StatusOK, providerConfigWriteResponse(result))
}

// loginReadinessGap re-reads providerConfigID's current stored configuration
// via h.ReadStore and reports the first field missing for login (see
// readiness.go's providerConfigMissingLoginField
// for the per-kind field list and the doc comment on the ReadStore field for
// why this fails open — never blocking the enable path — when ReadStore is
// nil or the lookup itself fails or finds nothing: those are the same
// conditions under which Store.EnableProviderConfig below is left to report
// its own, authoritative not-found/error outcome.
func (h *MutationHandler) loginReadinessGap(
	r *http.Request, providerConfigID, tenantID string,
) (field string, missing bool) {
	if h.ReadStore == nil {
		return "", false
	}
	detail, found, err := h.ReadStore.GetProviderConfigDetail(r.Context(), providerConfigID, tenantID)
	if err != nil || !found {
		return "", false
	}
	missingField := providerConfigMissingLoginField(detail.ProviderKind, detail.Configuration)
	return missingField, missingField != ""
}

func (h *MutationHandler) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	const eventType = governanceaudit.EventTypeIDPConfigChange
	if !h.requirePermission(w, r, eventType, "identity_admin.provider_config_test_connection") {
		return
	}
	tenantID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	if h.Tester == nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_connection_tester_unavailable", "")
		querycontract.WriteError(w, http.StatusServiceUnavailable, "connection test capability is unavailable")
		return
	}
	providerConfigID := strings.TrimSpace(querycontract.PathParam(r, "provider_config_id"))
	if providerConfigID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_id_required", "")
		querycontract.WriteError(w, http.StatusBadRequest, "provider_config_id is required")
		return
	}
	result, err := h.Tester.TestProviderConnection(r.Context(), providerConfigID, tenantID)
	if err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "provider_config_test_connection_error", "")
		querycontract.WriteError(w, http.StatusInternalServerError, "connection test failed to run")
		return
	}
	reason := "provider_config_test_connection_passed"
	decision := governanceaudit.DecisionAllowed
	if !result.OK {
		reason = "provider_config_test_connection_failed"
		decision = governanceaudit.DecisionDenied
	}
	h.audit(r, eventType, decision, reason, "")
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"provider_config_id": providerConfigID,
		"ok":                 result.OK,
		"detail":             result.Detail,
	})
}

// See audit.go for audit, providerConfigWriteResponse,
// providerConfigWriteErrorReason, and writeProviderConfigWriteError.
