// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"net/http"
	"testing"
)

// TestListManifestDependenciesNilStoreReturns503 proves the manifest
// leg fails closed when the G2 ManifestDependenciesReadAdmission pin is
// unwired: the fixed 503 names the missing fact store instead of
// inventing an empty dependency page.
func TestListManifestDependenciesNilStoreReturns503(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	recorder := manifestServeRequest(t, h, "/api/v0/repository/manifest-dependencies?repository_id=repo-1&limit=1", nil)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store status = %d, want 503; body = %s", recorder.Code, recorder.Body.String())
	}
}
