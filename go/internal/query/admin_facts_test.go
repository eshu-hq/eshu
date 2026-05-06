package query

import (
	"net/http"
	"testing"
	"time"
)

func TestAdminHandler_WorkItemsQuery(t *testing.T) {
	store := &stubAdminStore{
		workItems: []AdminWorkItem{
			{
				WorkItemID: "wi-1",
				ScopeID:    "scope-1",
				Stage:      "projector",
				Status:     "failed",
			},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/work-items/query", map[string]any{
		"statuses": []string{"failed"},
		"limit":    50,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if int(got["count"].(float64)) != 1 {
		t.Errorf("count = %v, want 1", got["count"])
	}
	items := got["items"].([]any)
	first := items[0].(map[string]any)
	if first["work_item_id"] != "wi-1" {
		t.Errorf("work_item_id = %q, want %q", first["work_item_id"], "wi-1")
	}
}

func TestAdminHandler_WorkItemsQuery_NoStore(t *testing.T) {
	h := &AdminHandler{}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/work-items/query", map[string]any{})

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestAdminHandler_DecisionsQuery(t *testing.T) {
	now := time.Now().UTC()
	store := &stubAdminStore{
		decisions: []AdminDecisionRow{
			{
				DecisionID:   "dec-1",
				DecisionType: "project_workloads",
				RepositoryID: "repo-1",
				SourceRunID:  "run-1",
				WorkItemID:   "wi-1",
				Subject:      "workload-a",
				CreatedAt:    now,
			},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/decisions/query", map[string]any{
		"repository_id": "repo-1",
		"source_run_id": "run-1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if int(got["count"].(float64)) != 1 {
		t.Errorf("count = %v, want 1", got["count"])
	}
}

func TestAdminHandler_DecisionsQuery_MissingFields(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/decisions/query", map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminHandler_DeadLetter(t *testing.T) {
	store := &stubAdminStore{
		deadLettered: []AdminWorkItem{
			{WorkItemID: "wi-1", Status: "dead_letter"},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/dead-letter", map[string]any{
		"work_item_ids": []string{"wi-1"},
		"failure_class": "manual_override",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if int(got["count"].(float64)) != 1 {
		t.Errorf("count = %v, want 1", got["count"])
	}
}

func TestAdminHandler_DeadLetter_NoSelector(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/dead-letter", map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminHandler_Skip(t *testing.T) {
	store := &stubAdminStore{
		skipped: []AdminWorkItem{
			{WorkItemID: "wi-1", Status: "skipped"},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/skip", map[string]any{
		"repository_id": "repo-1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if int(got["count"].(float64)) != 1 {
		t.Errorf("count = %v, want 1", got["count"])
	}
}

func TestAdminHandler_Skip_EmptyRepoID(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/skip", map[string]any{
		"repository_id": "",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminHandler_Replay(t *testing.T) {
	store := &stubAdminStore{
		replayed: []AdminWorkItem{
			{WorkItemID: "wi-1", Status: "pending"},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/replay", map[string]any{
		"failure_class": "transient_error",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if got["status"] != "replayed" {
		t.Errorf("status = %q, want %q", got["status"], "replayed")
	}
}

func TestAdminHandler_Replay_NoSelector(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/replay", map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminHandler_Backfill(t *testing.T) {
	now := time.Now().UTC()
	store := &stubAdminStore{
		backfillRow: &AdminBackfillRequest{
			BackfillRequestID: "bf-1",
			ScopeID:           strPtr("scope-1"),
			OperatorNote:      strPtr("test backfill"),
			CreatedAt:         now,
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/backfill", map[string]any{
		"scope_id": "scope-1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if got["status"] != "accepted" {
		t.Errorf("status = %q, want %q", got["status"], "accepted")
	}
}

func TestAdminHandler_Backfill_NoSelector(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/backfill", map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminHandler_ReplayEventsQuery(t *testing.T) {
	now := time.Now().UTC()
	store := &stubAdminStore{
		replayEvents: []AdminReplayEvent{
			{
				ReplayEventID: "re-1",
				WorkItemID:    "wi-1",
				ScopeID:       "scope-1",
				GenerationID:  "gen-1",
				CreatedAt:     now,
			},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/replay-events/query", map[string]any{
		"limit": 50,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if int(got["count"].(float64)) != 1 {
		t.Errorf("count = %v, want 1", got["count"])
	}
}

func TestAdminHandler_ReplayEventsQuery_NoStore(t *testing.T) {
	h := &AdminHandler{}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/replay-events/query", map[string]any{})

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
