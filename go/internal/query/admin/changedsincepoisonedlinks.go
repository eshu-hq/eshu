// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	changedSincePoisonedLinkSchemaVersion = "eshu.admin.changed_since_poisoned_links.v1"
	changedSincePoisonedLinkMaxLimit      = 500
	changedSincePoisonedLinkMaxTimeout    = 30 * time.Second

	// changedSinceLinkStatusPoisoned and changedSinceLinkStatusRetrying are
	// the two derived statuses this read exposes, and the two values the
	// request's optional status filter accepts (#7290, #7127 ruling 8.10).
	changedSinceLinkStatusPoisoned = "poisoned"
	changedSinceLinkStatusRetrying = "retrying"
)

// ChangedSincePoisonedLink is a bounded operator-facing view of one durable
// changed_since_scope_cursor row that is either poisoned (a link that hit its
// counting-failure limit, BreakLinkPoisoned) or retrying (a counted failure is
// pending on the scope's head activation, backing off toward its next
// attempt). #7290 gives this state a read surface: the writer is a runner
// over its own ledger, not a fact_work_items dead letter
// (go/internal/storage/postgres/freshness/links, #7127 ruling 8.10 item 10),
// so it never appears in list_dead_letter_work_items.
type ChangedSincePoisonedLink struct {
	ScopeID string `json:"scope_id"`
	// Status is changedSinceLinkStatusPoisoned or changedSinceLinkStatusRetrying.
	Status string `json:"status"`
	// ActivationSeq is the head activation the scope is stuck on: the
	// poisoned activation when Status is poisoned, otherwise the activation
	// currently backing off.
	ActivationSeq    int64      `json:"activation_seq"`
	AttemptCount     int        `json:"attempt_count"`
	LastFailureClass *string    `json:"last_failure_class,omitempty"`
	NextAttemptAt    *time.Time `json:"next_attempt_at,omitempty"`
	PoisonedAt       *time.Time `json:"poisoned_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// ChangedSincePoisonedLinkFilter constrains the bounded, deterministic
// changed-since poisoned/retrying link read (#7290). With no ScopeID or
// Cursor it is a fleet-wide page ordered by scope_id; Cursor carries the
// last scope_id of a prior page for forward keyset pagination.
type ChangedSincePoisonedLinkFilter struct {
	// Status narrows to "poisoned" or "retrying"; empty returns both.
	Status               string
	ScopeID              string
	Cursor               string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	Limit                int
	Timeout              time.Duration
}

// ChangedSincePoisonedLinksHandler mounts only the bounded changed-since
// poisoned/retrying link read surface, mirroring DeadLetterListHandler and
// InputInvalidFactListHandler's read-only mounts for cmd/mcp-server (which
// has no full Handler exposing mutations).
type ChangedSincePoisonedLinksHandler struct {
	Store       Store
	Instruments *telemetry.Instruments
}

// Mount registers the changed-since poisoned-links list read without
// exposing admin mutations.
func (h *ChangedSincePoisonedLinksHandler) Mount(mux *http.ServeMux) {
	admin := &Handler{Store: h.Store, Instruments: h.Instruments}
	mux.HandleFunc("POST /api/v0/admin/changed-since/poisoned-links/query", admin.listChangedSincePoisonedLinks)
}

// listChangedSincePoisonedLinks returns a bounded, scoped page of durable
// changed_since_scope_cursor rows that are poisoned or retrying (#7290).
// POST /api/v0/admin/changed-since/poisoned-links/query
func (h *Handler) listChangedSincePoisonedLinks(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if h.Store == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		Status    string `json:"status"`
		ScopeID   string `json:"scope_id"`
		Cursor    string `json:"cursor"`
		Limit     int    `json:"limit"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Limit <= 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required and must be between 1 and 500")
		return
	}
	if req.Limit > changedSincePoisonedLinkMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be <= 500")
		return
	}
	if req.TimeoutMS <= 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "timeout_ms is required and must be between 1 and 30000")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout > changedSincePoisonedLinkMaxTimeout {
		querycontract.WriteError(w, http.StatusBadRequest, "timeout_ms must be <= 30000")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && status != changedSinceLinkStatusPoisoned && status != changedSinceLinkStatusRetrying {
		querycontract.WriteError(w, http.StatusBadRequest, "status must be poisoned or retrying")
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		writeChangedSincePoisonedLinkList(w, req.Limit, false, nil)
		return
	}

	filter := ChangedSincePoisonedLinkFilter{
		Status:               status,
		ScopeID:              strings.TrimSpace(req.ScopeID),
		Cursor:               strings.TrimSpace(req.Cursor),
		AllowedRepositoryIDs: access.GrantedRepositoryIDs(),
		AllowedScopeIDs:      access.GrantedScopeIDs(),
		Limit:                req.Limit + 1,
		Timeout:              timeout,
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, "query.changed_since_poisoned_links",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.sql.table", "changed_since_scope_cursor"),
			attribute.String("changed_since.poisoned_links.status_filter", status),
		),
	)
	defer span.End()

	items, err := h.Store.ListChangedSincePoisonedLinks(ctx, filter)
	h.recordChangedSincePoisonedLinksQuery(ctx, start, err)
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			querycontract.WriteError(w, http.StatusGatewayTimeout, "changed-since poisoned-links query timed out")
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list changed-since poisoned links: %v", err))
		return
	}
	truncated := len(items) > req.Limit
	if truncated {
		items = items[:req.Limit]
	}
	span.SetAttributes(attribute.Int("changed_since.poisoned_links.count", len(items)))
	writeChangedSincePoisonedLinkList(w, req.Limit, truncated, items)
}

// recordChangedSincePoisonedLinksQuery records the query-duration histogram
// and, on failure, the reason-labeled error counter for the bounded
// changed-since poisoned-links read (#7290). A nil h.Instruments (the default
// for callers that have not wired telemetry) makes this a no-op.
func (h *Handler) recordChangedSincePoisonedLinksQuery(ctx context.Context, start time.Time, err error) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.QueryChangedSincePoisonedLinksDuration != nil {
		h.Instruments.QueryChangedSincePoisonedLinksDuration.Record(ctx, time.Since(start).Seconds())
	}
	if err == nil || h.Instruments.QueryChangedSincePoisonedLinksErrors == nil {
		return
	}
	reason := "store_error"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "timeout"
	}
	h.Instruments.QueryChangedSincePoisonedLinksErrors.Add(ctx, 1, metric.WithAttributes(telemetry.AttrReason(reason)))
}

func writeChangedSincePoisonedLinkList(w http.ResponseWriter, limit int, truncated bool, items []ChangedSincePoisonedLink) {
	response := map[string]any{
		"schema_version": changedSincePoisonedLinkSchemaVersion,
		"limit":          limit,
		"count":          len(items),
		"truncated":      truncated,
		"items":          nonNilChangedSincePoisonedLinks(items),
	}
	if truncated && len(items) > 0 {
		response["next_cursor"] = items[len(items)-1].ScopeID
	}
	querycontract.WriteJSON(w, http.StatusOK, response)
}

func nonNilChangedSincePoisonedLinks(items []ChangedSincePoisonedLink) []ChangedSincePoisonedLink {
	if items == nil {
		return []ChangedSincePoisonedLink{}
	}
	return items
}
