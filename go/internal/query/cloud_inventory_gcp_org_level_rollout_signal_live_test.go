// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive is the
// #5238 follow-up review finding: a real GCP org- or folder-level asset has
// no derivable project segment, so its resolved AccountID is genuinely
// blank -- cloudInventoryAdmissionBasePayload still writes the "account_id"
// key, just with an empty string value (see
// TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider's
// blank-AccountID case in go/internal/reducer). That payload shape -- key
// PRESENT, value blank -- must NOT be mistaken by
// cloudInventoryRolloutGapWarningFlags for a pre-#5238 row, which has NO
// "account_id" key at all. A false account_alias_rollout_gap here would be a
// differently-wrong operator signal: unlike a real rollout gap it would
// never resolve on the next collector sync, because the org-level asset
// permanently has no project id.
//
// Reuses seedCloudInventoryGCPOrgLevelAssetLiveCorpus
// (cloud_inventory_gcp_project_gap_live_test.go), the existing fixture for
// exactly this present-but-blank account_id shape, rather than duplicating
// it. This exercises the full production path -- the HTTP handler, not
// CloudInventoryIdentities directly -- so it proves the same code a real
// GET /api/v0/cloud/inventory?provider=gcp&project_id=... request runs.
func TestCloudInventoryGCPOrgLevelAssetDoesNotFalselyWarnRolloutGapLive(t *testing.T) {
	db, ctx, cancel := openCloudInventoryLiveDB(t)
	defer cancel()
	seedCloudInventoryGCPOrgLevelAssetLiveCorpus(t, ctx, db)

	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/cloud/inventory?provider=gcp&project_id=no-such-project",
		nil,
	).WithContext(ctx)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; body = %s", err, w.Body.String())
	}
	data := resp.Data.(map[string]any)
	if got, want := len(data["resources"].([]any)), 0; got != want {
		t.Fatalf("resources = %d, want %d (no-such-project genuinely matches nothing)", got, want)
	}
	if flags, present := data["warning_flags"]; present {
		t.Fatalf(
			"warning_flags = %#v, want absent -- the only gcp row in scope has account_id present-but-blank "+
				"(a genuine org-level asset), which must never be mistaken for a pre-#5238 row with no account_id "+
				"key at all",
			flags,
		)
	}
}
