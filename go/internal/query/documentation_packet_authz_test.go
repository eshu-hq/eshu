// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentationEvidencePacketScopedEmptyGrantReturnsNotFoundWithoutRead(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationPacketErr:    errors.New("broad documentation packet read"),
			documentationFreshnessErr: errors.New("broad documentation freshness read"),
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name string
		path string
	}{
		{
			name: "packet",
			path: "/api/v0/documentation/findings/finding:docs:1/evidence-packet",
		},
		{
			name: "freshness",
			path: "/api/v0/documentation/evidence-packets/doc-packet:1/freshness?packet_version=1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
				Mode:        AuthModeScoped,
				TenantID:    "tenant_a",
				WorkspaceID: "workspace_a",
			}))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusNotFound; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			for _, leaked := range []string{"broad documentation", "doc-packet:1", "packet_version", "freshness_state"} {
				if strings.Contains(body, leaked) {
					t.Fatalf("not-found response leaked %q: %s", leaked, body)
				}
			}
		})
	}
}

func TestDocumentationEvidencePacketHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	var packetFilter documentationEvidencePacketFilter
	var freshnessFilter documentationEvidencePacketFreshnessFilter
	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationPacketModel: documentationEvidencePacketReadModel{
				Available: true,
				Packet: map[string]any{
					"packet_id": "doc-packet:1",
				},
			},
			documentationPacketFilter: &packetFilter,
			documentationFreshnessModel: documentationEvidencePacketFreshnessReadModel{
				Available:           true,
				PacketID:            "doc-packet:1",
				PacketVersion:       "1",
				FreshnessState:      "fresh",
				LatestPacketVersion: "1",
			},
			documentationFreshnessFilter: &freshnessFilter,
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	doScopedPacketRequest(t, mux, "/api/v0/documentation/findings/finding:docs:1/evidence-packet")
	if got, want := packetFilter.FindingID, "finding:docs:1"; got != want {
		t.Fatalf("packet filter FindingID = %q, want %q", got, want)
	}
	if got, want := packetFilter.AllowedRepositoryIDs, []string{"repository:team-a"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("packet filter AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := packetFilter.AllowedScopeIDs, []string{"scope:team-a"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("packet filter AllowedScopeIDs = %#v, want %#v", got, want)
	}

	doScopedPacketRequest(t, mux, "/api/v0/documentation/evidence-packets/doc-packet:1/freshness?packet_version=1")
	if got, want := freshnessFilter.PacketID, "doc-packet:1"; got != want {
		t.Fatalf("freshness filter PacketID = %q, want %q", got, want)
	}
	if got, want := freshnessFilter.SavedPacketVersion, "1"; got != want {
		t.Fatalf("freshness filter SavedPacketVersion = %q, want %q", got, want)
	}
	if got, want := freshnessFilter.AllowedRepositoryIDs, []string{"repository:team-a"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("freshness filter AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := freshnessFilter.AllowedScopeIDs, []string{"scope:team-a"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("freshness filter AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

func TestDocumentationEvidencePacketSQLAppliesScopedAuthorizationBeforeOrder(t *testing.T) {
	t.Parallel()

	findingSQL, findingArgs := buildDocumentationEvidencePacketByFindingSQL(documentationEvidencePacketFilter{
		FindingID:            "finding:docs:1",
		AllowedRepositoryIDs: []string{"repository:team-a", "repository:team-a"},
		AllowedScopeIDs:      []string{"scope:team-a"},
	})
	freshnessSQL, freshnessArgs := buildDocumentationEvidencePacketByPacketSQL(documentationEvidencePacketFreshnessFilter{
		PacketID:             "doc-packet:1",
		AllowedRepositoryIDs: []string{"repository:team-a", "repository:team-a"},
		AllowedScopeIDs:      []string{"scope:team-a"},
	})
	for _, tc := range []struct {
		name string
		sql  string
		args []any
	}{
		{
			name: "packet by finding",
			sql:  findingSQL,
			args: findingArgs,
		},
		{
			name: "packet freshness",
			sql:  freshnessSQL,
			args: freshnessArgs,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertDocumentationAuthorizationPredicate(t, tc.sql, "fact_records", "ingestion_scopes")
			if got, want := len(tc.args), 3; got != want {
				t.Fatalf("args len = %d, want %d", got, want)
			}
			if strings.Index(tc.sql, "fact_records.scope_id IN (") > strings.Index(tc.sql, "ORDER BY") {
				t.Fatalf("scoped authorization predicate appears after ORDER BY:\n%s", tc.sql)
			}
		})
	}
}

func doScopedPacketRequest(t *testing.T, handler http.Handler, path string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant_a",
		WorkspaceID:          "workspace_a",
		AllowedRepositoryIDs: []string{"repository:team-a"},
		AllowedScopeIDs:      []string{"scope:team-a"},
	}))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; body = %s", err, rec.Body.String())
	}
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing: %#v", envelope)
	}
}

func equalPacketStringSlices(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
