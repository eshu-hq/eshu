// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	pgdb "github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// liveDBQueryer adapts *sql.DB to the advisory store's read-only port so
// the handler-level proof below runs the production store and SQL.
type liveDBQueryer struct{ db *sql.DB }

func (q liveDBQueryer) QueryContext(ctx context.Context, query string, args ...any) (pgdb.Rows, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// TestAdvisoryEvidenceRecognizerHandlerLive drives the HTTP evidence routes
// against the live proof schema: CVE seeding, lowercase normalization, and
// the single-advisory route.
func TestAdvisoryEvidenceRecognizerHandlerLive(t *testing.T) {
	db := openRecognizerLiveDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	seedRecognizerLiveFixtures(t, ctx, db)

	store := advisory.NewPostgresEvidenceStoreWithReadStore(liveDBQueryer{db: db})
	handler := &SupplyChainHandler{AdvisoryEvidence: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	get := func(target string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body = %s", target, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", target, err)
		}
		return body
	}
	advisoryIDs := func(body map[string]any) []string {
		t.Helper()
		out := []string{}
		advisories, _ := body["advisories"].([]any)
		for _, item := range advisories {
			row, _ := item.(map[string]any)
			for _, key := range []string{"osv_ids", "cve_ids", "ghsa_ids"} {
				ids, _ := row[key].([]any)
				for _, id := range ids {
					if text, ok := id.(string); ok {
						out = append(out, text)
					}
				}
			}
			pkgs, _ := row["affected_packages"].([]any)
			for _, pkg := range pkgs {
				if pkgRow, ok := pkg.(map[string]any); ok {
					if id, ok := pkgRow["advisory_id"].(string); ok {
						out = append(out, id)
					}
				}
			}
		}
		return out
	}
	contains := func(values []string, want string) bool {
		for _, value := range values {
			if value == want {
				return true
			}
		}
		return false
	}

	t.Run("CVE seed serves PYSEC facts over HTTP", func(t *testing.T) {
		body := get("/api/v0/supply-chain/advisories/evidence?cve_id=CVE-2026-5101&limit=10")
		if ids := advisoryIDs(body); !contains(ids, "PYSEC-2026-5101") {
			t.Fatalf("HTTP CVE seed missing PYSEC-2026-5101, got %v", ids)
		}
	})

	t.Run("lowercase pysec lookup resolves uppercase record", func(t *testing.T) {
		body := get("/api/v0/supply-chain/advisories/evidence?advisory_id=pysec-2026-5101&limit=10")
		if ids := advisoryIDs(body); !contains(ids, "PYSEC-2026-5101") {
			t.Fatalf("lowercase lookup missing PYSEC-2026-5101, got %v", ids)
		}
	})

	t.Run("lowercase distro lookup resolves uppercase record", func(t *testing.T) {
		body := get("/api/v0/supply-chain/advisories/evidence?advisory_id=debian-cve-2026-5103&limit=10")
		if ids := advisoryIDs(body); !contains(ids, "DEBIAN-CVE-2026-5103") {
			t.Fatalf("lowercase distro lookup missing DEBIAN-CVE-2026-5103, got %v", ids)
		}
	})

	t.Run("single advisory route serves PYSEC record", func(t *testing.T) {
		body := get("/api/v0/supply-chain/vulnerabilities/PYSEC-2026-5101")
		if got, _ := body["canonical_id"].(string); got != "CVE-2026-5101" {
			t.Fatalf("canonical_id = %q, want CVE-2026-5101 grouping", got)
		}
	})
}
