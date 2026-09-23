// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestHandlerListServesCatalogAtItsOwnRoute drives Handler.list through the
// route Handler.Mount registers, without the root APIRouter. The router-level
// tests stay in package query because they need APIRouter, which imports this
// package; this one proves the handler serves its own route correctly on its
// own, which is the contract this package owns.
func TestHandlerListServesCatalogAtItsOwnRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	(&Handler{Profile: querycontract.ProfileProduction}).Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/capabilities", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		Capabilities []struct {
			ID string `json:"capability"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body %s", err, rec.Body.String())
	}
	if len(got.Capabilities) == 0 {
		t.Fatal("catalog returned no capabilities; the handler must serve the embedded catalog")
	}

	// Every entry must carry a non-empty, unique id: the OpenAPI fragment
	// declares id non-nullable, and a duplicate would mean the registry
	// double-registered a capability.
	seen := make(map[string]bool, len(got.Capabilities))
	for i, c := range got.Capabilities {
		if c.ID == "" {
			t.Fatalf("capability %d has an empty id", i)
		}
		if seen[c.ID] {
			t.Fatalf("capability id %q appears twice", c.ID)
		}
		seen[c.ID] = true
	}
}
