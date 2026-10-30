// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ssmHostInventoryReadbackCapability gates the SSM host inventory readback.
// It is the read surface the aws_ssm_host family declaration names once this
// handler lands (eshu-hq/eshu#9558).
const ssmHostInventoryReadbackCapability = "ssm_host_inventory.readback.list"

// ssmHostInventoryReadbackMaxLimit caps one host page. Hosts are the page
// unit; observation rows follow their host, so the row budget per response
// stays proportional to the page.
const ssmHostInventoryReadbackMaxLimit = 100

// ssmHostInventoryReadbackFailedMessage is the fixed 500 body for a failed
// host inventory read. The error text never reaches the body; it stays on
// the handler span.
const ssmHostInventoryReadbackFailedMessage = "ssm host inventory readback failed"

// ssmHostInventoryStatuses is the closed set of status filter values. An
// unrecognized status is rejected as invalid input so it can never silently
// return the full inventory.
var ssmHostInventoryStatuses = map[string]struct{}{
	ssmHostStatusSuccessful: {},
	ssmHostStatusPartial:    {},
	ssmHostStatusStale:      {},
	ssmHostStatusUnknown:    {},
}

// SSMHostInventoryHandler serves the bounded, filterable, paged readback of
// admitted SSM host inventory observations with their read-time capture
// status (eshu-hq/eshu#9558).
//
// GET /api/v0/cloud/ssm-host-inventory?instance_id=&account_id=&region=&status=&limit=&cursor=
type SSMHostInventoryHandler struct {
	// Content is the relational store; it must also implement
	// ssmHostFactsStore (ContentReader does) for the readback to serve.
	Content ContentStore
	// Graph reads the committed observation nodes. A nil graph serves no
	// committed rows: hosts read from admitted facts alone.
	Graph GraphQuery
	// Profile selects the active runtime profile for capability gating.
	Profile QueryProfile
	// Bounds carries the fail-closed freshness configuration. Zero bounds
	// answer 503 rather than deriving status under unbounded rules.
	Bounds ssmhost.Bounds
	// Now supplies the read time; defaults to time.Now.
	Now func() time.Time
}

// Mount registers the SSM host inventory readback route.
func (h *SSMHostInventoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/cloud/ssm-host-inventory", h.listHostInventory)
}

func (h *SSMHostInventoryHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *SSMHostInventoryHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// listHostInventory serves one bounded host page: committed observation
// nodes from the graph overlaid with the active generation's admitted facts.
func (h *SSMHostInventoryHandler) listHostInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySSMHostInventoryReadback,
		"GET /api/v0/cloud/ssm-host-inventory",
		ssmHostInventoryReadbackCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), ssmHostInventoryReadbackCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"ssm host inventory readback requires the graph-backed full-stack runtime",
			ErrorCodeUnsupportedCapability,
			ssmHostInventoryReadbackCapability,
			h.profile(),
			querycontract.RequiredProfile(ssmHostInventoryReadbackCapability),
		)
		return
	}

	if err := h.Bounds.Validate(); err != nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"ssm host inventory readback freshness bounds are not configured",
			ErrorCodeReadModelUnavailable,
			ssmHostInventoryReadbackCapability,
			h.profile(),
			querycontract.RequiredProfile(ssmHostInventoryReadbackCapability),
		)
		return
	}

	filter, ok := h.filterFromRequest(w, r)
	if !ok {
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		WriteSuccess(w, r, http.StatusOK, ssmHostInventoryResponse(ssmHostInventoryReadModel{}), BuildTruthEnvelope(
			h.profile(),
			ssmHostInventoryReadbackCapability,
			TruthBasisSemanticFacts,
			"scoped token grants authorize no repositories; ssm host inventory is empty",
		))
		return
	}
	filter.AllScopes = !access.Scoped()
	filter.AllowedRepositoryIDs = access.GrantedRepositoryIDs()
	filter.AllowedScopeIDs = access.GrantedScopeIDs()

	store, ok := h.factsStore(w, r)
	if !ok {
		return
	}
	readModel, err := ReadSSMHostInventory(r.Context(), h.Graph, store, filter, h.Bounds, h.now())
	if err != nil {
		h.writeReadbackFailure(w, r, err)
		return
	}

	WriteSuccess(w, r, http.StatusOK, ssmHostInventoryResponse(readModel), BuildTruthEnvelope(
		h.profile(),
		ssmHostInventoryReadbackCapability,
		TruthBasisSemanticFacts,
		"committed SSM host observation nodes overlaid with the active generation's admitted facts",
	))
}

// factsStore resolves the admitted-fact read from h.Content. It mirrors the
// documentation handler pattern: the Postgres-backed ContentReader satisfies
// the narrow read interface, and a missing or incompatible store is reported
// as an explicit read-model-unavailable error rather than a silent empty
// result.
func (h *SSMHostInventoryHandler) factsStore(w http.ResponseWriter, r *http.Request) (ssmHostFactsStore, bool) {
	if h.Content == nil {
		h.writeReadModelUnavailable(w, r)
		return nil, false
	}
	store, ok := h.Content.(ssmHostFactsStore)
	if !ok {
		h.writeReadModelUnavailable(w, r)
		return nil, false
	}
	return store, true
}

func (h *SSMHostInventoryHandler) writeReadModelUnavailable(w http.ResponseWriter, r *http.Request) {
	WriteContractError(
		w,
		r,
		http.StatusNotImplemented,
		"ssm host inventory readback requires the Postgres admitted-fact read model",
		ErrorCodeReadModelUnavailable,
		ssmHostInventoryReadbackCapability,
		h.profile(),
		querycontract.RequiredProfile(ssmHostInventoryReadbackCapability),
	)
}

func (h *SSMHostInventoryHandler) writeReadbackFailure(w http.ResponseWriter, r *http.Request, err error) {
	if WriteGraphReadError(w, r, err, ssmHostInventoryReadbackCapability) {
		return
	}
	status, errEnv := tracing.ServerFailureEnvelope(r.Context(), err, ssmHostInventoryReadbackFailedMessage, ssmHostInventoryReadbackCapability)
	errEnv.Profiles = &querycontract.ErrorProfiles{
		Current:  h.profile(),
		Required: querycontract.RequiredProfile(ssmHostInventoryReadbackCapability),
	}
	WriteErrorEnvelope(w, r, status, errEnv)
}

// filterFromRequest parses and validates the bounded request filters. An
// unrecognized status never silently returns the full inventory.
func (h *SSMHostInventoryHandler) filterFromRequest(w http.ResponseWriter, r *http.Request) (ssmHostInventoryFilter, bool) {
	status := strings.ToLower(strings.TrimSpace(QueryParam(r, "status")))
	if status != "" {
		if _, known := ssmHostInventoryStatuses[status]; !known {
			h.writeInvalidArgument(w, r, "status must be one of successful, partial, stale, or unknown")
			return ssmHostInventoryFilter{}, false
		}
	}
	limit, offset, ok := h.pagination(w, r)
	if !ok {
		return ssmHostInventoryFilter{}, false
	}
	return ssmHostInventoryFilter{
		InstanceID: strings.TrimSpace(QueryParam(r, "instance_id")),
		AccountID:  strings.TrimSpace(QueryParam(r, "account_id")),
		Region:     strings.TrimSpace(QueryParam(r, "region")),
		Status:     status,
		Limit:      limit,
		Offset:     offset,
	}, true
}

func (h *SSMHostInventoryHandler) pagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit := ssmHostInventoryDefaultLimit
	if raw := strings.TrimSpace(QueryParam(r, "limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > ssmHostInventoryReadbackMaxLimit {
			h.writeInvalidArgument(w, r, "limit must be an integer between 1 and 100")
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

func (h *SSMHostInventoryHandler) writeInvalidArgument(w http.ResponseWriter, r *http.Request, message string) {
	WriteContractError(
		w,
		r,
		http.StatusBadRequest,
		message,
		ErrorCodeInvalidArgument,
		ssmHostInventoryReadbackCapability,
		h.profile(),
		querycontract.RequiredProfile(ssmHostInventoryReadbackCapability),
	)
}

// ssmHostInventoryResponse builds the bounded list envelope body. Hosts carry
// the committed observation rows with the read-time status overlay; the
// response never synthesizes vulnerability or supply-chain findings.
func ssmHostInventoryResponse(readModel ssmHostInventoryReadModel) map[string]any {
	hosts := make([]any, 0, len(readModel.Hosts))
	for _, host := range readModel.Hosts {
		hosts = append(hosts, map[string]any{
			"partition":              host.Partition,
			"account_id":             host.AccountID,
			"region":                 host.Region,
			"instance_id":            host.InstanceID,
			"status":                 host.Status,
			"status_reason":          host.StatusReason,
			"capture_time":           host.CaptureTime,
			"scope_id":               host.ScopeID,
			"generation_id":          host.GenerationID,
			"applications":           host.Applications,
			"applications_truncated": host.ApplicationsTruncated,
			"platforms":              host.Platforms,
			"facts_truncated":        host.FactsTruncated,
		})
	}
	if hosts == nil {
		hosts = []any{}
	}
	return map[string]any{
		"hosts":           hosts,
		"malformed_facts": readModel.MalformedFacts,
		"next_cursor":     readModel.NextCursor,
	}
}
