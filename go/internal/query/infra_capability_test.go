package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInfraRelationshipsLocalAuthoritativeUsesGraphInsteadOfCapabilityGate(t *testing.T) {
	t.Parallel()

	graphCalled := false
	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				graphCalled = true
				if !strings.Contains(cypher, "MATCH (n) WHERE n.id = $entity_id") {
					t.Fatalf("cypher = %q, want entity relationship query", cypher)
				}
				if got, want := params["entity_id"], "workload:eshu"; got != want {
					t.Fatalf("entity_id param = %#v, want %#v", got, want)
				}
				return map[string]any{
					"id":       "workload:eshu",
					"name":     "eshu",
					"labels":   []any{"Workload"},
					"outgoing": []any{},
					"incoming": []any{},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(`{"entity_id":"workload:eshu"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.getRelationships(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !graphCalled {
		t.Fatal("handler did not query the graph")
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("error = %#v, want nil", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("truth = nil, want truth envelope")
	}
	if got, want := envelope.Truth.Profile, ProfileLocalAuthoritative; got != want {
		t.Fatalf("truth.profile = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Capability, "platform_impact.deployment_chain"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
}
