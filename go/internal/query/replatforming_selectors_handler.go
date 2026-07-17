// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	replatformingSelectorDefaultLimit = 100
	replatformingSelectorMaxLimit     = 200
)

var replatformingFindingKinds = []string{
	findingKindAmbiguousCloudResource,
	findingKindOrphanedCloudResource,
	findingKindUnmanagedCloudResource,
	findingKindUnknownCloudResource,
}

// handleReplatformingSelectors returns active AWS collector scopes that can
// safely anchor the existing bounded plan routes. The route is intentionally
// not enabled for scoped tokens: until tenant/source-scope filtering is wired,
// the shared auth middleware rejects scoped callers before this handler runs.
func (h *IaCHandler) handleReplatformingSelectors(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryReplatformingSelectors,
		"GET /api/v0/replatforming/selectors",
		replatformingSelectorInventoryCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), replatformingSelectorInventoryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"replatforming selector inventory requires active AWS collector scope and drift read models",
			ErrorCodeUnsupportedCapability,
			replatformingSelectorInventoryCapability,
			h.profile(),
			requiredProfile(replatformingSelectorInventoryCapability),
		)
		return
	}
	limit, ok := replatformingSelectorLimit(w, r)
	if !ok {
		return
	}
	store, ok := h.Management.(ReplatformingSelectorStore)
	if !ok || store == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"replatforming selector inventory requires the Postgres AWS collector-scope read model",
			ErrorCodeBackendUnavailable,
			replatformingSelectorInventoryCapability,
			h.profile(),
			requiredProfile(replatformingSelectorInventoryCapability),
		)
		return
	}
	page, err := store.ListReplatformingSelectors(r.Context(), limit)
	if err != nil {
		WriteContractError(
			w,
			r,
			http.StatusInternalServerError,
			"replatforming selector inventory failed",
			ErrorCodeInternalError,
			replatformingSelectorInventoryCapability,
			h.profile(),
			requiredProfile(replatformingSelectorInventoryCapability),
		)
		return
	}
	WriteSuccess(w, r, http.StatusOK, replatformingSelectorResponse(page, limit), BuildTruthEnvelope(
		h.profile(),
		replatformingSelectorInventoryCapability,
		TruthBasisSemanticFacts,
		"resolved from active AWS collector scopes and reducer-materialized drift finding counts",
	))
}

func replatformingSelectorLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return replatformingSelectorDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > replatformingSelectorMaxLimit {
		WriteErrorEnvelope(w, r, http.StatusBadRequest, &ErrorEnvelope{
			Code:       ErrorCodeInvalidArgument,
			Message:    "limit must be an integer between 1 and 200",
			Capability: replatformingSelectorInventoryCapability,
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
			"label":         replatformingSelectorLabel(scope),
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

func replatformingSelectorLabel(scope ReplatformingSelectorScope) string {
	accountSuffix := scope.AccountID
	if len(accountSuffix) > 4 {
		accountSuffix = accountSuffix[len(accountSuffix)-4:]
	}
	return fmt.Sprintf("%s in %s (account ...%s)", scope.Service, scope.Region, accountSuffix)
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
