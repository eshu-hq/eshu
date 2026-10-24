// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
)

// legacyWorkloadRetirementFailedMessage answers a failed retirement request
// with a server fault. A graph error quotes hosts and constraint names, so
// err.Error() never reaches the body; the cause goes on the span and the log.
const legacyWorkloadRetirementFailedMessage = "legacy workload retirement failed"

// maxLegacyWorkloadRetirementBody bounds the request body: a limit, up to 100
// ids, and a reason.
const maxLegacyWorkloadRetirementBody = 64 << 10

// LegacyWorkloadRetirer runs one retirement request. The reducer's
// *retire.Runner satisfies it; this package imports only the contract types
// in graph/legacy/workload, never the reducer.
type LegacyWorkloadRetirer interface {
	Run(ctx context.Context, req legacyworkload.RetireRequest) (legacyworkload.RetireResponse, error)
}

// LegacyWorkloadRetirementHandler serves POST /admin/retire-legacy-workloads
// on a runtime admin listener (#8795). Like the other admin routes it is
// unauthenticated and must be reachable only on the admin port. A dry run only
// reads; an execute needs a reason and deletes only the nodes the retirement
// statement itself re-confirms.
type LegacyWorkloadRetirementHandler struct {
	retirer LegacyWorkloadRetirer
}

// NewLegacyWorkloadRetirementHandler constructs the handler.
func NewLegacyWorkloadRetirementHandler(retirer LegacyWorkloadRetirer) (*LegacyWorkloadRetirementHandler, error) {
	if retirer == nil {
		return nil, fmt.Errorf("legacy workload retirer is required")
	}
	return &LegacyWorkloadRetirementHandler{retirer: retirer}, nil
}

// Mount registers the route on the given mux.
func (h *LegacyWorkloadRetirementHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/admin/retire-legacy-workloads", h.handle)
}

// legacyWorkloadRetirementRequest is the wire body. DryRun is a pointer so a
// missing field is a 400 instead of a silent execute.
type legacyWorkloadRetirementRequest struct {
	DryRun *bool    `json:"dry_run"`
	Limit  int      `json:"limit"`
	IDs    []string `json:"ids"`
	Reason string   `json:"reason"`
}

func (h *LegacyWorkloadRetirementHandler) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body legacyWorkloadRetirementRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLegacyWorkloadRetirementBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: unexpected data after the JSON object")
		return
	}
	if body.DryRun == nil {
		writeJSONError(w, http.StatusBadRequest, "dry_run is required: send true to list candidates or false to retire them")
		return
	}

	resp, err := h.retirer.Run(r.Context(), legacyworkload.RetireRequest{
		DryRun: *body.DryRun,
		Limit:  body.Limit,
		IDs:    body.IDs,
		Reason: body.Reason,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, resp)
	case errors.Is(err, legacyworkload.ErrInvalidRequest):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, legacyworkload.ErrBackendUnvalidated):
		writeJSONError(w, http.StatusNotImplemented, legacyworkload.ErrBackendUnvalidated.Error())
	case errors.Is(err, legacyworkload.ErrWriterUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, legacyworkload.ErrWriterUnavailable.Error())
	default:
		writeRecoveryFailure(w, r, err, legacyWorkloadRetirementFailedMessage)
	}
}
