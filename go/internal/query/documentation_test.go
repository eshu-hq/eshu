package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocumentationHandlerListsFindings(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFindingsModel: documentationFindingListReadModel{
				Findings: []map[string]any{
					{
						"finding_id":          "finding:service-deployment:1",
						"finding_version":     "2026-05-09T19:00:00Z",
						"finding_type":        "service_deployment_drift",
						"status":              "conflict",
						"truth_level":         "derived",
						"freshness_state":     "fresh",
						"source_id":           "doc-source:confluence:platform",
						"document_id":         "doc:confluence:123",
						"section_id":          "body",
						"summary":             "payment-service deployment source drifted",
						"evidence_packet_url": "/api/v0/documentation/findings/finding:service-deployment:1/evidence-packet",
					},
				},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings?finding_type=service_deployment_drift&status=conflict", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	findings := resp["findings"].([]any)
	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
	finding := findings[0].(map[string]any)
	if got, want := finding["finding_type"], "service_deployment_drift"; got != want {
		t.Fatalf("finding_type = %#v, want %#v", got, want)
	}
	if got, want := finding["status"], "conflict"; got != want {
		t.Fatalf("status = %#v, want %#v", got, want)
	}
}

func TestDocumentationHandlerReturnsEvidencePacketStates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		status         string
		staleReason    string
		ambiguity      []any
		unsupported    string
		wantHTTPStatus int
	}{
		{
			name:           "success",
			status:         "conflict",
			wantHTTPStatus: http.StatusOK,
		},
		{
			name:           "stale",
			status:         "stale",
			staleReason:    "graph truth observed before latest documentation revision",
			wantHTTPStatus: http.StatusOK,
		},
		{
			name:           "ambiguous",
			status:         "ambiguous",
			ambiguity:      []any{"multiple deployment sources are equally current"},
			wantHTTPStatus: http.StatusOK,
		},
		{
			name:           "unsupported",
			status:         "unsupported",
			unsupported:    "missing_graph_truth",
			wantHTTPStatus: http.StatusOK,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			packet := documentationPacketFixture("finding:service-deployment:"+tc.name, tc.status)
			packet["truth"].(map[string]any)["ambiguity"] = tc.ambiguity
			packet["states"].(map[string]any)["unsupported_reason"] = tc.unsupported
			packet["states"].(map[string]any)["stale_reason"] = tc.staleReason
			handler := &DocumentationHandler{
				Content: fakePortContentStore{
					documentationPacketModel: documentationEvidencePacketReadModel{
						Available: true,
						Packet:    packet,
					},
				},
				Profile: ProfileProduction,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/"+packet["finding_id"].(string)+"/evidence-packet", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, tc.wantHTTPStatus; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, want nil", err)
			}
			finding := resp["finding"].(map[string]any)
			if got, want := finding["status"], tc.status; got != want {
				t.Fatalf("finding.status = %#v, want %#v", got, want)
			}
			states := resp["states"].(map[string]any)
			if got, want := states["finding_state"], tc.status; got != want {
				t.Fatalf("states.finding_state = %#v, want %#v", got, want)
			}
			if got, want := states["unsupported_reason"], tc.unsupported; got != want {
				t.Fatalf("states.unsupported_reason = %#v, want %#v", got, want)
			}
			if got, want := states["stale_reason"], tc.staleReason; got != want {
				t.Fatalf("states.stale_reason = %#v, want %#v", got, want)
			}
		})
	}
}

func TestDocumentationHandlerDeniesEvidencePacketWhenVisibilityIsBlocked(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationPacketModel: documentationEvidencePacketReadModel{
				Denied:       true,
				DeniedReason: "caller cannot read documentation source",
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/finding:service-deployment:denied/evidence-packet", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestDocumentationHandlerReturnsPacketFreshness(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFreshnessModel: documentationEvidencePacketFreshnessReadModel{
				Available:           true,
				PacketID:            "doc-packet:service-deployment:1",
				PacketVersion:       "1",
				FreshnessState:      "fresh",
				LatestPacketVersion: "1",
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/evidence-packets/doc-packet:service-deployment:1/freshness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["freshness_state"], "fresh"; got != want {
		t.Fatalf("freshness_state = %#v, want %#v", got, want)
	}
	if got, want := resp["latest_packet_version"], "1"; got != want {
		t.Fatalf("latest_packet_version = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationFindingsFiltersAndBuildsPacketURL(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"finding_id": "finding:service-deployment:1",
		"finding_version": "2026-05-09T19:00:00Z",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"truth_level": "derived",
		"freshness_state": "fresh",
		"source_id": "doc-source:confluence:platform",
		"document_id": "doc:confluence:123",
		"section_id": "body",
		"summary": "payment-service deployment source drifted"
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{payload}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{
		FindingType: "service_deployment_drift",
		Status:      "conflict",
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	if got, want := len(got.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	finding := got.Findings[0]
	if got, want := finding["finding_id"], "finding:service-deployment:1"; got != want {
		t.Fatalf("finding_id = %#v, want %#v", got, want)
	}
	if got, want := finding["evidence_packet_url"], "/api/v0/documentation/findings/finding:service-deployment:1/evidence-packet"; got != want {
		t.Fatalf("evidence_packet_url = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationEvidencePacketDeniesBlockedVisibility(t *testing.T) {
	t.Parallel()

	packet := []byte(`{
		"packet_id": "doc-packet:service-deployment:1",
		"packet_version": "1",
		"finding_id": "finding:service-deployment:1",
		"permissions": {
			"viewer_can_read_source": false,
			"denied_reason": "caller cannot read source document"
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationEvidencePacket(t.Context(), "finding:service-deployment:1")
	if err != nil {
		t.Fatalf("documentationEvidencePacket() error = %v, want nil", err)
	}
	if !got.Denied {
		t.Fatalf("documentationEvidencePacket().Denied = false, want true")
	}
	if got, want := got.DeniedReason, "caller cannot read source document"; got != want {
		t.Fatalf("DeniedReason = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationEvidencePacketFreshness(t *testing.T) {
	t.Parallel()

	packet := []byte(`{
		"packet_id": "doc-packet:service-deployment:1",
		"packet_version": "2",
		"states": {
			"freshness_state": "stale"
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationEvidencePacketFreshness(t.Context(), "doc-packet:service-deployment:1")
	if err != nil {
		t.Fatalf("documentationEvidencePacketFreshness() error = %v, want nil", err)
	}
	if got, want := got.PacketID, "doc-packet:service-deployment:1"; got != want {
		t.Fatalf("PacketID = %#v, want %#v", got, want)
	}
	if got, want := got.FreshnessState, "stale"; got != want {
		t.Fatalf("FreshnessState = %#v, want %#v", got, want)
	}
	if got, want := got.LatestPacketVersion, "2"; got != want {
		t.Fatalf("LatestPacketVersion = %#v, want %#v", got, want)
	}
}

func documentationPacketFixture(findingID, status string) map[string]any {
	return map[string]any{
		"packet_id":      "doc-packet:service-deployment:1",
		"packet_version": "1",
		"generated_at":   "2026-05-09T19:00:00Z",
		"finding_id":     findingID,
		"finding": map[string]any{
			"finding_id":      findingID,
			"finding_version": "2026-05-09T19:00:00Z",
			"finding_type":    "service_deployment_drift",
			"status":          status,
		},
		"document": map[string]any{
			"source_id":     "doc-source:confluence:platform",
			"document_id":   "doc:confluence:123",
			"external_id":   "123",
			"canonical_uri": "https://example.atlassian.net/wiki/spaces/PLAT/pages/123",
			"revision_id":   "17",
			"title":         "Payment Service Deployment",
		},
		"section": map[string]any{
			"section_id":   "body",
			"heading_text": "Deployment",
			"text_hash":    "sha256:section",
		},
		"bounded_excerpt": map[string]any{
			"text":             "payment-service deploys from platform/payment-chart",
			"text_hash":        "sha256:excerpt",
			"source_start_ref": "storage:body",
			"source_end_ref":   "storage:body",
		},
		"linked_entities": []any{
			map[string]any{
				"entity_id":    "service:payment-service",
				"entity_type":  "service",
				"match_status": "exact",
				"confidence":   "observed",
			},
		},
		"current_truth": map[string]any{
			"claim_key":        "deployment_source",
			"documented_value": "platform/payment-chart",
			"current_value":    "platform/payment-service/deploy",
			"truth_level":      "derived",
			"freshness_state":  "fresh",
		},
		"evidence_refs": []any{
			map[string]any{
				"fact_id":          "fact:deploy",
				"source_system":    "git",
				"source_uri":       "https://github.com/example/platform-deployments",
				"source_record_id": "payment-service/deploy.yaml",
			},
		},
		"truth": map[string]any{
			"label":     "derived",
			"basis":     "deployment graph evidence",
			"ambiguity": []any{},
		},
		"permissions": map[string]any{
			"viewer_can_read_source":    true,
			"packet_redacted":           false,
			"write_permission_decision": "external_updater_must_check",
		},
		"states": map[string]any{
			"finding_state":       status,
			"unsupported_reason":  "",
			"stale_reason":        "",
			"freshness_state":     "fresh",
			"permission_decision": "allowed",
		},
	}
}
