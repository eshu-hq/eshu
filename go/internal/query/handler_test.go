package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("body status = %q, want %q", got["status"], "ok")
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusNotFound, "repo not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["detail"] != "repo not found" {
		t.Errorf("detail = %q, want %q", got["detail"], "repo not found")
	}
}

func TestWriteSuccessEnvelopeNegotiation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	WriteSuccess(w, req, http.StatusOK, map[string]any{"matches": []any{}}, &TruthEnvelope{
		Level:      TruthLevelDerived,
		Capability: "code_search.fuzzy_symbol",
		Profile:    ProfileLocalLightweight,
		Basis:      TruthBasisContentIndex,
		Freshness:  TruthFreshness{State: FreshnessFresh},
		Reason:     "resolved from content index",
	})

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["data"]; !ok {
		t.Fatalf("data missing from envelope: %#v", got)
	}
	if _, ok := got["truth"]; !ok {
		t.Fatalf("truth missing from envelope: %#v", got)
	}
	if got["error"] != nil {
		t.Fatalf("error = %#v, want nil", got["error"])
	}
}

func TestWriteContractErrorEnvelopeNegotiation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	WriteContractError(
		w,
		req,
		http.StatusNotImplemented,
		"call-chain analysis requires authoritative graph mode",
		"unsupported_capability",
		"call_graph.call_chain_path",
		ProfileLocalLightweight,
		ProfileLocalFullStack,
	)

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["data"] != nil {
		t.Fatalf("data = %#v, want nil", got["data"])
	}
	if truth, ok := got["truth"]; !ok {
		t.Fatalf("truth missing from envelope: %#v", got)
	} else if truth != nil {
		t.Fatalf("truth = %#v, want nil", truth)
	}
	errBody, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %#v, want map[string]any", got["error"])
	}
	if errBody["code"] != "unsupported_capability" {
		t.Fatalf("error.code = %#v, want unsupported_capability", errBody["code"])
	}
}

func TestReadJSON(t *testing.T) {
	body := bytes.NewBufferString(`{"name":"test","count":42}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	r.Header.Set("Content-Type", "application/json")

	var got struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := ReadJSON(r, &got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if got.Name != "test" || got.Count != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestReadJSON_NilBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = nil

	var got struct{}
	if err := ReadJSON(r, &got); err == nil {
		t.Error("expected error for nil body")
	}
}

func TestQueryParam(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?name=test&empty=", nil)
	if got := QueryParam(r, "name"); got != "test" {
		t.Errorf("QueryParam(name) = %q", got)
	}
	if got := QueryParam(r, "empty"); got != "" {
		t.Errorf("QueryParam(empty) = %q", got)
	}
	if got := QueryParam(r, "missing"); got != "" {
		t.Errorf("QueryParam(missing) = %q", got)
	}
}

func TestQueryParamInt(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=25&bad=abc", nil)
	if got := QueryParamInt(r, "limit", 10); got != 25 {
		t.Errorf("QueryParamInt(limit) = %d, want 25", got)
	}
	if got := QueryParamInt(r, "bad", 10); got != 10 {
		t.Errorf("QueryParamInt(bad) = %d, want 10 (default)", got)
	}
	if got := QueryParamInt(r, "missing", 10); got != 10 {
		t.Errorf("QueryParamInt(missing) = %d, want 10", got)
	}
}

func TestAPIRouter_HealthEndpoint(t *testing.T) {
	router := &APIRouter{}
	mux := http.NewServeMux()
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("health status = %q", got["status"])
	}
}
