// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cloudInventoryReadbackCapability is the conformance-matrix capability id for
// the canonical multi-cloud resource inventory readback. It is gated to
// reducer-owning profiles; local_lightweight returns unsupported_capability
// because it cannot materialize the reducer_cloud_resource_identity rows.
const cloudInventoryReadbackCapability = "cloud_inventory.readback.list"

const (
	cloudInventoryReadbackMaxLimit     = 200
	cloudInventoryReadbackDefaultLimit = 50
)

// cloudInventoryManagementOriginDeclared, cloudInventoryManagementOriginApplied,
// and cloudInventoryManagementOriginObserved mirror the reducer ManagementOrigin
// precedence (declared > applied > observed) that the canonical
// reducer_cloud_resource_identity payload records. The readback validates the
// management_origin filter against this closed set so an unrecognized value can
// never silently widen the query.
const (
	cloudInventoryManagementOriginDeclared = "declared"
	cloudInventoryManagementOriginApplied  = "applied"
	cloudInventoryManagementOriginObserved = "observed"
)

// cloudInventoryProviders is the closed set of providers the canonical inventory
// readback accepts as a filter. It matches the multi-cloud collector contract
// providers; an unrecognized provider is rejected as invalid input rather than
// silently ignored.
var cloudInventoryProviders = map[string]struct{}{
	"aws":   {},
	"gcp":   {},
	"azure": {},
}

// cloudInventoryManagementOrigins is the closed set of management_origin filter
// values, keyed for validation.
var cloudInventoryManagementOrigins = map[string]struct{}{
	cloudInventoryManagementOriginDeclared: {},
	cloudInventoryManagementOriginApplied:  {},
	cloudInventoryManagementOriginObserved: {},
}

// CloudInventoryHandler serves a bounded, paginated, truth-labeled readback of
// canonical multi-cloud resource identities from the reducer-owned
// reducer_cloud_resource_identity rows. It is read-only and never fabricates
// identity: it projects only the reducer-resolved canonical fields and never
// echoes raw provider locators, tags, or credentials.
type CloudInventoryHandler struct {
	// Content is the relational store; it must also implement
	// cloudInventoryReadModelStore (ContentReader does) for the readback to serve.
	Content ContentStore
	// Profile selects the active runtime profile for capability gating.
	Profile QueryProfile
}

// cloudInventoryReadModelStore reads canonical CloudResource identity rows from
// the durable fact store. ContentReader implements it; the handler type-asserts
// h.Content to this narrow interface so unit tests can supply a fixture-backed
// reader without a live database or graph backend.
type cloudInventoryReadModelStore interface {
	cloudInventoryIdentities(context.Context, cloudInventoryFilter) (cloudInventoryListReadModel, error)
	// cloudInventoryPreRolloutEvidenceExists reports whether the filter's
	// provider/access scope contains any canonical identity row predating the
	// #5238 account_id rollout (see cloud_inventory_rollout_signal.go). The
	// handler calls it only when an account-alias filter matched zero rows, so
	// it never costs a round trip on the hot unfiltered or non-empty-result
	// path.
	cloudInventoryPreRolloutEvidenceExists(context.Context, cloudInventoryFilter) (bool, error)
}

// cloudInventoryFilter holds the optional, bounded filters for the readback.
// Empty values mean "no filter". ScopeID is the literal canonical scope_id and
// matches exactly one ingestion scope. AccountAliasKey/AccountAliasValue carry
// a provider-flavored account selector (account_id, project_id, or
// subscription_id) instead: unlike scope_id, every provider's scope id is a
// derived, opaque per-shard identifier (for AWS, one shard per
// account+region+service; see go/internal/collector/awscloud/awsruntime) that
// is never literally equal to the raw provider account/project/subscription
// number, and one account can fan out into many scope ids. An alias therefore
// resolves against the canonical payload's normalized "account_id" field,
// which the reducer populates from the resolving provider source fact's own
// identity (aws_resource.account_id, gcp_cloud_resource.project_id,
// azure_cloud_resource.subscription_id -- see
// go/internal/reducer/cloud_inventory_admission_writer.go), rather than
// against scope_id itself (#5238 -- the prior code compared the alias value
// directly to scope_id, which silently matched zero rows for every real
// multi-shard account on every provider).
type cloudInventoryFilter struct {
	Provider          string
	ScopeID           string
	AccountAliasKey   string
	AccountAliasValue string
	ManagementOrigin  string
	Limit             int
	Offset            int
	// AllScopes, AllowedRepositoryIDs, and AllowedScopeIDs carry the #5167
	// access-scoping bound: AllScopes selects the admin/all-scopes path (no
	// row filtering, byte-identical to the pre-#5167 query). When AllScopes is
	// false, rows are restricted to fact_records.scope_id matching
	// AllowedRepositoryIDs or AllowedScopeIDs -- reducer_cloud_resource_identity
	// facts are keyed by ingestion scope (cloud account/project/subscription),
	// the same identifier space repositoryAccessFilter grants bind. listInventory
	// short-circuits to an empty page without a query when a scoped caller holds
	// no grants, matching the #5137 LiveActivityStore precedent.
	AllScopes            bool
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// cloudInventoryListReadModel is the bounded page of canonical identity payloads
// plus the keyset continuation offset.
type cloudInventoryListReadModel struct {
	Resources  []map[string]any
	NextCursor string
}

// Mount registers the canonical cloud inventory readback route.
func (h *CloudInventoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/cloud/inventory", h.listInventory)
}

func (h *CloudInventoryHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

// listInventory serves the bounded, filterable, paginated readback of canonical
// multi-cloud resource identities.
//
// GET /api/v0/cloud/inventory?provider=&scope_id=&management_origin=&limit=&cursor=
func (h *CloudInventoryHandler) listInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCloudInventoryReadback,
		"GET /api/v0/cloud/inventory",
		cloudInventoryReadbackCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cloudInventoryReadbackCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"cloud inventory readback requires reducer-owned canonical CloudResource identity rows",
			ErrorCodeUnsupportedCapability,
			cloudInventoryReadbackCapability,
			h.profile(),
			requiredProfile(cloudInventoryReadbackCapability),
		)
		return
	}

	filter, ok := h.filterFromRequest(w, r)
	if !ok {
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		WriteSuccess(w, r, http.StatusOK, cloudInventoryResponse(cloudInventoryListReadModel{}, filter, nil), BuildTruthEnvelope(
			h.profile(),
			cloudInventoryReadbackCapability,
			TruthBasisSemanticFacts,
			"scoped token grants authorize no repositories; cloud inventory is empty",
		))
		return
	}
	filter.AllScopes = !access.scoped()
	filter.AllowedRepositoryIDs = access.grantedRepositoryIDs()
	filter.AllowedScopeIDs = access.grantedScopeIDs()

	store, ok := h.store(w, r)
	if !ok {
		return
	}
	readModel, err := store.cloudInventoryIdentities(r.Context(), filter)
	if err != nil {
		WriteContractError(
			w,
			r,
			http.StatusInternalServerError,
			"cloud inventory readback failed",
			ErrorCodeInternalError,
			cloudInventoryReadbackCapability,
			h.profile(),
			requiredProfile(cloudInventoryReadbackCapability),
		)
		return
	}

	warningFlags := cloudInventoryRolloutGapWarningFlags(r.Context(), store, filter, readModel)

	WriteSuccess(w, r, http.StatusOK, cloudInventoryResponse(readModel, filter, warningFlags), BuildTruthEnvelope(
		h.profile(),
		cloudInventoryReadbackCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned canonical CloudResource identity facts (reducer_cloud_resource_identity)",
	))
}

// store resolves the canonical inventory read model from h.Content. It mirrors
// the documentation handler pattern: the Postgres-backed ContentReader satisfies
// the narrow read interface, and a missing or incompatible store is reported as
// an explicit read-model-unavailable error rather than a silent empty result.
func (h *CloudInventoryHandler) store(w http.ResponseWriter, r *http.Request) (cloudInventoryReadModelStore, bool) {
	if h.Content == nil {
		h.writeReadModelUnavailable(w, r)
		return nil, false
	}
	store, ok := h.Content.(cloudInventoryReadModelStore)
	if !ok {
		h.writeReadModelUnavailable(w, r)
		return nil, false
	}
	return store, true
}

func (h *CloudInventoryHandler) writeReadModelUnavailable(w http.ResponseWriter, r *http.Request) {
	WriteContractError(
		w,
		r,
		http.StatusNotImplemented,
		"cloud inventory readback requires the Postgres canonical identity read model",
		ErrorCodeReadModelUnavailable,
		cloudInventoryReadbackCapability,
		h.profile(),
		requiredProfile(cloudInventoryReadbackCapability),
	)
}

// filterFromRequest parses and validates the bounded request filters. Unknown
// provider or management_origin values are rejected as invalid input so an
// unrecognized filter never silently returns the full inventory.
func (h *CloudInventoryHandler) filterFromRequest(w http.ResponseWriter, r *http.Request) (cloudInventoryFilter, bool) {
	provider := strings.ToLower(strings.TrimSpace(QueryParam(r, "provider")))
	if provider != "" {
		if _, known := cloudInventoryProviders[provider]; !known {
			h.writeInvalidArgument(w, r, "provider must be one of aws, gcp, or azure")
			return cloudInventoryFilter{}, false
		}
	}
	managementOrigin := strings.ToLower(strings.TrimSpace(QueryParam(r, "management_origin")))
	if managementOrigin != "" {
		if _, known := cloudInventoryManagementOrigins[managementOrigin]; !known {
			h.writeInvalidArgument(w, r, "management_origin must be one of declared, applied, or observed")
			return cloudInventoryFilter{}, false
		}
	}
	limit, offset, ok := h.pagination(w, r)
	if !ok {
		return cloudInventoryFilter{}, false
	}
	scopeID, aliasKey, aliasValue := cloudInventoryScopeSelector(r)
	// An account_id/project_id/subscription_id alias resolves against the
	// single shared canonical payload key "account_id" with no provider
	// disambiguation baked into the value itself (buildCloudInventoryIdentitiesSQL
	// only ANDs a provider predicate when filter.Provider is non-empty). AWS
	// account ids and GCP project NUMBERS are both plain decimal strings, and
	// accountIDFallback can populate account_id from a numeric CAI
	// full_resource_name segment for some asset types, so a caller who omits
	// provider risks a genuine cross-provider numeric collision under an
	// AllScopes grant -- one account_id value silently matching another
	// provider's unrelated resource. Requiring provider whenever an alias is
	// present closes that at the input boundary rather than relying on every
	// caller to remember to scope it (#5238).
	if aliasKey != "" {
		if provider == "" {
			h.writeInvalidArgument(w, r, fmt.Sprintf(
				"%s requires provider (account_id/project_id/subscription_id resolve against a shared canonical key with no provider disambiguation)",
				aliasKey,
			))
			return cloudInventoryFilter{}, false
		}
		// Requiring SOME provider only narrows the collision blast radius; it
		// does not prevent it. account_id/project_id/subscription_id are
		// documented, provider-SPECIFIC aliases (aws/gcp/azure respectively --
		// see cloudInventoryAccountAliasRequiredProviders), so a caller who
		// supplies the wrong provider for the alias they used (e.g.
		// provider=gcp&account_id=...) can still resolve against another
		// provider's row sharing that numeric key. Reject the mismatch
		// explicitly rather than silently resolving it against the wrong
		// provider's keyspace (#5881 review follow-up).
		if requiredProvider := cloudInventoryAccountAliasRequiredProviders[aliasKey]; provider != requiredProvider {
			h.writeInvalidArgument(w, r, fmt.Sprintf(
				"%s requires provider=%s, got provider=%s",
				aliasKey, requiredProvider, provider,
			))
			return cloudInventoryFilter{}, false
		}
	}
	return cloudInventoryFilter{
		Provider:          provider,
		ScopeID:           scopeID,
		AccountAliasKey:   aliasKey,
		AccountAliasValue: aliasValue,
		ManagementOrigin:  managementOrigin,
		Limit:             limit,
		Offset:            offset,
	}, true
}

// cloudInventoryAccountAliasKeys is the closed, ordered set of provider-flavored
// account selector query parameters (account_id, project_id, subscription_id).
// Every alias resolves against the SAME normalized canonical payload field
// ("account_id" -- see buildCloudInventoryIdentitiesSQL); this slice exists
// only to recognize which request parameter the caller used, for the
// scope_id-takes-precedence rule and for echoing the response scope object
// back under the right key name. A caller-supplied alias name is never
// interpolated as free-form SQL, only compared against this fixed slice.
var cloudInventoryAccountAliasKeys = []string{"account_id", "project_id", "subscription_id"}

// cloudInventoryAccountAliasRequiredProviders is the closed mapping from each
// provider-flavored account alias to the ONE provider it is documented
// (OpenAPI, the MCP tool, http-api.md) to select: account_id is AWS-specific,
// project_id is GCP-specific, subscription_id is Azure-specific. Every alias
// resolves against the SAME shared canonical "account_id" payload key with no
// provider disambiguation baked into the value itself, so requiring merely
// SOME provider (rather than the matching one) still lets a numeric
// collision resolve against the wrong provider's row -- for example
// provider=gcp&account_id=123 would otherwise be accepted and could return
// the GCP resource whose normalized account_id happens to be "123", even
// though account_id is the AWS-specific selector. filterFromRequest rejects
// any (aliasKey, provider) pair not listed here as invalid input (#5881
// review follow-up to #5238).
var cloudInventoryAccountAliasRequiredProviders = map[string]string{
	"account_id":      "aws",
	"project_id":      "gcp",
	"subscription_id": "azure",
}

// cloudInventoryScopeSelector resolves the request's scope filter. scope_id is
// the literal canonical scope id and, when present, wins outright. Otherwise
// the first non-empty provider-flavored alias (account_id, project_id,
// subscription_id) is returned as (aliasKey, aliasValue) so the caller can
// resolve it against the canonical payload's normalized account_id field
// instead of against scope_id -- see cloudInventoryFilter's doc comment for
// why the two are not interchangeable (#5238).
func cloudInventoryScopeSelector(r *http.Request) (scopeID string, aliasKey string, aliasValue string) {
	if value := strings.TrimSpace(QueryParam(r, "scope_id")); value != "" {
		return value, "", ""
	}
	for _, key := range cloudInventoryAccountAliasKeys {
		if value := strings.TrimSpace(QueryParam(r, key)); value != "" {
			return "", key, value
		}
	}
	return "", "", ""
}

func (h *CloudInventoryHandler) writeInvalidArgument(w http.ResponseWriter, r *http.Request, message string) {
	WriteContractError(
		w,
		r,
		http.StatusBadRequest,
		message,
		ErrorCodeInvalidArgument,
		cloudInventoryReadbackCapability,
		h.profile(),
		requiredProfile(cloudInventoryReadbackCapability),
	)
}

// pagination parses the bounded limit and the keyset cursor (a non-negative
// integer offset). limit defaults to cloudInventoryReadbackDefaultLimit and is
// capped at cloudInventoryReadbackMaxLimit; out-of-range values are rejected.
func (h *CloudInventoryHandler) pagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit := cloudInventoryReadbackDefaultLimit
	if raw := strings.TrimSpace(QueryParam(r, "limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > cloudInventoryReadbackMaxLimit {
			h.writeInvalidArgument(w, r, "limit must be an integer between 1 and 200")
			return 0, 0, false
		}
		limit = parsed
	}
	offset := 0
	if raw := strings.TrimSpace(QueryParam(r, "cursor")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			h.writeInvalidArgument(w, r, "cursor must be a non-negative integer offset")
			return 0, 0, false
		}
		offset = parsed
	}
	return limit, offset, true
}

// cloudInventoryResponse builds the bounded list envelope body. Each resource is
// projected through cloudInventoryResourceView so raw provider locators never
// reach the wire and every row carries its provider-neutral source state.
// warningFlags is nil in the common case; see
// cloudInventoryRolloutGapWarningFlags for when it carries the #5238
// account-alias rollout-gap disambiguation signal.
func cloudInventoryResponse(readModel cloudInventoryListReadModel, filter cloudInventoryFilter, warningFlags []string) map[string]any {
	resources := make([]map[string]any, 0, len(readModel.Resources))
	for _, payload := range readModel.Resources {
		resources = append(resources, cloudInventoryResourceView(payload))
	}
	nextCursor := strings.TrimSpace(readModel.NextCursor)
	body := map[string]any{
		"resources": resources,
		"count":     len(resources),
		"limit":     filter.Limit,
		"truncated": nextCursor != "",
		"scope":     cloudInventoryScope(filter),
	}
	if nextCursor != "" {
		body["next_cursor"] = nextCursor
	}
	if len(warningFlags) > 0 {
		body["warning_flags"] = warningFlags
	}
	return body
}

// cloudInventoryScope reports the bounded, non-sensitive filter scope applied to
// the readback so a caller can confirm what was queried without echoing raw
// provider identity. Empty filters are omitted.
func cloudInventoryScope(filter cloudInventoryFilter) map[string]any {
	scope := map[string]any{}
	if filter.Provider != "" {
		scope["provider"] = filter.Provider
	}
	if filter.ScopeID != "" {
		scope["scope_id"] = filter.ScopeID
	} else if filter.AccountAliasKey != "" && filter.AccountAliasValue != "" {
		// Echo back under the alias name the caller actually used (account_id,
		// project_id, or subscription_id) rather than mislabeling a raw provider
		// account number as "scope_id".
		scope[filter.AccountAliasKey] = filter.AccountAliasValue
	}
	if filter.ManagementOrigin != "" {
		scope["management_origin"] = filter.ManagementOrigin
	}
	return scope
}
