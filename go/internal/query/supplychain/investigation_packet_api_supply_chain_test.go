// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestGetImpactPacketWithoutResponderFailsClosed pins the fail-closed
// branch: a handler wired without a PacketResponder answers the packet
// route with 503 rather than composing a packet without the lane-B
// envelope. Every production wiring injects the responder, so this is
// the only nil reference repo-wide by construction.
func TestGetImpactPacketWithoutResponderFailsClosed(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{Profile: querycontract.ProfileProduction}
	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1", nil)
	rec := httptest.NewRecorder()

	handler.getImpactPacket(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("getImpactPacket() without responder status = %d, want %d: %s",
			rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	// Branch order pins this to the responder check: the identical
	// ImpactExplanations-nil branch below is unreachable here.
	if !strings.Contains(rec.Body.String(), "supply-chain impact packets require the Postgres reducer read model") {
		t.Fatalf("getImpactPacket() without responder body missing branch message: %s",
			rec.Body.String())
	}
}
