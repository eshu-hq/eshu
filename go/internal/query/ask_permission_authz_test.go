package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postAskWithAuth(h *AskHandler, body string, auth AuthContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuthContext(req.Context(), auth))
	w := httptest.NewRecorder()
	h.handleAsk(w, req)
	return w
}

func TestAskHandlerGeneratedTokenRequiresAskSearchFeature(t *testing.T) {
	t.Parallel()

	asker := &fakeAsker{answer: AskAnswer{Packets: []AnswerPacket{{Summary: "should not run"}}}}
	h := &AskHandler{Asker: asker}
	w := postAskWithAuth(h, `{"question":"what services do I have?"}`, AuthContext{
		Mode:                         AuthModeScoped,
		TenantID:                     "tenant-a",
		WorkspaceID:                  "workspace-a",
		PermissionCatalogEnforced:    true,
		AllowedRepositoryIDs:         []string{"repo-payments"},
		AllowedPermissionFeatures:    []string{"repository_content"},
		AllowedPermissionDataClasses: []string{"source"},
	})

	if got, want := w.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, w.Body.String())
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodePermissionDenied {
		t.Fatalf("error = %#v, want permission_denied", envelope.Error)
	}
}

func TestAskHandlerGeneratedTokenRequiresAskSearchDataClasses(t *testing.T) {
	t.Parallel()

	asker := &fakeAsker{answer: AskAnswer{Packets: []AnswerPacket{{Summary: "should not run"}}}}
	h := &AskHandler{Asker: asker}
	w := postAskWithAuth(h, `{"question":"what services do I have?"}`, AuthContext{
		Mode:                         AuthModeScoped,
		TenantID:                     "tenant-a",
		WorkspaceID:                  "workspace-a",
		PermissionCatalogEnforced:    true,
		AllowedRepositoryIDs:         []string{"repo-payments"},
		AllowedPermissionFeatures:    []string{"ask_search"},
		AllowedPermissionDataClasses: []string{"ask_reasoning"},
	})

	if got, want := w.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, w.Body.String())
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodePermissionDenied {
		t.Fatalf("error = %#v, want permission_denied", envelope.Error)
	}
}
