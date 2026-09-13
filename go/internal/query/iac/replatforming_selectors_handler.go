// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	replatformingSelectorDefaultLimit = 100
	replatformingSelectorMaxLimit     = 200
)

var replatformingFindingKinds = []string{
	FindingKindAmbiguousCloudResource,
	FindingKindOrphanedCloudResource,
	FindingKindUnmanagedCloudResource,
	FindingKindUnknownCloudResource,
}

var replatformingAWSSelectorScopeIDPattern = regexp.MustCompile(
	`^aws:[0-9]{12}:[a-z0-9-]+:[a-z0-9-]+$`,
)

// handleReplatformingSelectors returns active AWS collector scopes that can
// safely anchor the existing bounded plan routes. Scoped callers see only
// exact AWS scope grants; repository-only or empty grants fail closed without
// reading the selector store because no repository-to-AWS-scope mapping is
// authoritative on this path.
func (h *Handler) handleReplatformingSelectors(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryReplatformingSelectors,
		"GET /api/v0/replatforming/selectors",
		ReplatformingSelectorInventoryCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), ReplatformingSelectorInventoryCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"replatforming selector inventory requires active AWS collector scope and drift read models",
			querycontract.ErrorCodeUnsupportedCapability,
			ReplatformingSelectorInventoryCapability,
			h.profile(),
			querycontract.RequiredProfile(ReplatformingSelectorInventoryCapability),
		)
		return
	}
	limit, ok := replatformingSelectorLimit(w, r)
	if !ok {
		return
	}
	store, ok := h.Management.(ReplatformingSelectorStore)
	if !ok || store == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"replatforming selector inventory requires the Postgres AWS collector-scope read model",
			querycontract.ErrorCodeBackendUnavailable,
			ReplatformingSelectorInventoryCapability,
			h.profile(),
			querycontract.RequiredProfile(ReplatformingSelectorInventoryCapability),
		)
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	allowedScopeIDs := replatformingAWSSelectorScopeIDs(access.GrantedScopeIDs())
	if access.Scoped() && len(allowedScopeIDs) == 0 {
		querycontract.WriteSuccess(w, r, http.StatusOK, replatformingSelectorScopedEmptyResponse(limit), querycontract.BuildTruthEnvelope(
			h.profile(),
			ReplatformingSelectorInventoryCapability,
			querycontract.TruthBasisSemanticFacts,
			"resolved from active AWS collector scopes authorized by exact scope grants; no authorized AWS scopes were granted",
		))
		return
	}
	page, err := store.ListReplatformingSelectors(r.Context(), limit, allowedScopeIDs)
	if err != nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusInternalServerError,
			"replatforming selector inventory failed",
			querycontract.ErrorCodeInternalError,
			ReplatformingSelectorInventoryCapability,
			h.profile(),
			querycontract.RequiredProfile(ReplatformingSelectorInventoryCapability),
		)
		return
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, replatformingSelectorResponse(page, limit), querycontract.BuildTruthEnvelope(
		h.profile(),
		ReplatformingSelectorInventoryCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from active AWS collector scopes and reducer-materialized drift finding counts",
	))
}

func replatformingSelectorScopedEmptyResponse(limit int) map[string]any {
	response := replatformingSelectorResponse(ReplatformingSelectorPage{}, limit)
	response["readiness"] = map[string]any{
		"state":       "no_authorized_scopes",
		"detail":      "No AWS collector scopes are authorized for this session.",
		"next_action": "Request an exact AWS collector scope grant, then reload this page.",
	}
	return response
}

func replatformingSelectorLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return replatformingSelectorDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > replatformingSelectorMaxLimit {
		querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeInvalidArgument,
			Message:    "limit must be an integer between 1 and 200",
			Capability: ReplatformingSelectorInventoryCapability,
		})
		return 0, false
	}
	return limit, true
}

func replatformingSelectorResponse(page ReplatformingSelectorPage, limit int) map[string]any {
	scopes := make([]map[string]any, 0, len(page.Scopes))
	emptyScopeCount := 0
	for _, scope := range page.Scopes {
		if scope.FindingCount == 0 {
			emptyScopeCount++
		}
		scopes = append(scopes, map[string]any{
			"scope_id":      scope.ScopeID,
			"account_id":    scope.AccountID,
			"region":        scope.Region,
			"service":       scope.Service,
			"label":         ReplatformingSelectorLabel(scope),
			"finding_count": scope.FindingCount,
		})
	}
	return map[string]any{
		"scopes":                scopes,
		"count":                 len(scopes),
		"limit":                 limit,
		"truncated":             page.Truncated,
		"empty_scope_count":     emptyScopeCount,
		"supported_scope_kinds": []string{"account", "region", "service"},
		"finding_kinds":         append([]string(nil), replatformingFindingKinds...),
		"page_sizes":            []int{25, 50, 100, 200},
		"readiness":             replatformingSelectorReadiness(scopes),
	}
}

func ReplatformingSelectorLabel(scope ReplatformingSelectorScope) string {
	accountSuffix := scope.AccountID
	if accountSuffix == "" {
		return fmt.Sprintf("%s in %s (account unknown)", scope.Service, scope.Region)
	}
	if len(accountSuffix) > 4 {
		accountSuffix = accountSuffix[len(accountSuffix)-4:]
	}
	return fmt.Sprintf("%s in %s (account ...%s)", scope.Service, scope.Region, accountSuffix)
}

func replatformingAWSSelectorScopeIDs(scopeIDs []string) []string {
	awsScopeIDs := make([]string, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		if replatformingAWSSelectorScopeIDPattern.MatchString(scopeID) {
			awsScopeIDs = append(awsScopeIDs, scopeID)
		}
	}
	return awsScopeIDs
}

func replatformingSelectorReadiness(scopes []map[string]any) map[string]any {
	if len(scopes) == 0 {
		return map[string]any{
			"state":       "collector_evidence_absent",
			"detail":      "No active AWS collector scopes are available for replatforming review.",
			"next_action": "Run or repair the AWS runtime collector, then wait for its scope generation to become active.",
		}
	}
	return map[string]any{
		"state":       "ready",
		"detail":      fmt.Sprintf("%d active AWS collector scope(s) are available.", len(scopes)),
		"next_action": "Choose an account, region, or source scope to review a bounded plan.",
	}
}
