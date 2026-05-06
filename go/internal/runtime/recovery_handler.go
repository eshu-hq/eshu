package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// RecoveryHandler provides HTTP endpoints for write-plane recovery operations.
// It replaces the Python admin refinalize and replay surfaces with Go-owned
// queue replay rather than direct graph mutation.
type RecoveryHandler struct {
	handler *recovery.Handler
}

// NewRecoveryHandler constructs the HTTP recovery handler.
func NewRecoveryHandler(handler *recovery.Handler) (*RecoveryHandler, error) {
	if handler == nil {
		return nil, fmt.Errorf("recovery handler is required")
	}

	return &RecoveryHandler{handler: handler}, nil
}

// Mount registers recovery routes on the given mux.
func (h *RecoveryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/admin/replay", h.handleReplay)
	mux.HandleFunc("/admin/refinalize", h.handleRefinalize)
}

type replayRequest struct {
	Stage        string   `json:"stage"`
	ScopeIDs     []string `json:"scope_ids"`
	FailureClass string   `json:"failure_class"`
	Limit        int      `json:"limit"`
}

type replayResponse struct {
	Status      string   `json:"status"`
	Stage       string   `json:"stage"`
	Replayed    int      `json:"replayed"`
	WorkItemIDs []string `json:"work_item_ids"`
}

// handleReplay replays failed projector or reducer work items.
func (h *RecoveryHandler) handleReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req replayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	filter := recovery.ReplayFilter{
		Stage:        recovery.Stage(req.Stage),
		ScopeIDs:     req.ScopeIDs,
		FailureClass: req.FailureClass,
		Limit:        req.Limit,
	}

	result, err := h.handler.ReplayFailed(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, replayResponse{
		Status:      "replayed",
		Stage:       string(result.Stage),
		Replayed:    result.Replayed,
		WorkItemIDs: result.WorkItemIDs,
	})
}

type refinalizeRequest struct {
	ScopeIDs []string `json:"scope_ids"`
}

type refinalizeResponse struct {
	Status   string   `json:"status"`
	Enqueued int      `json:"enqueued"`
	ScopeIDs []string `json:"scope_ids"`
}

// handleRefinalize re-enqueues projector work for the specified scopes.
func (h *RecoveryHandler) handleRefinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req refinalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	filter := recovery.RefinalizeFilter{
		ScopeIDs: req.ScopeIDs,
	}

	result, err := h.handler.Refinalize(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, refinalizeResponse{
		Status:   "enqueued",
		Enqueued: result.Enqueued,
		ScopeIDs: result.ScopeIDs,
	})
}

type jsonErrorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, jsonErrorResponse{Error: message})
}
