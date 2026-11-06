// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"testing"
)

// TestManifestTriggerReopensSecurityAlertHolder is the #10061 reducer
// regression: a lockfile change that bumps lodash reopens the security-alert
// scope whose active repository alert names lodash — a candidate with no
// purl, the shape an alert fact reduces to — and the holder read is asked
// for the changed keys' repository as well as their ecosystems. An alert
// candidate for react, present unchanged on both sides, is not reopened.
func TestManifestTriggerReopensSecurityAlertHolder(t *testing.T) {
	t.Parallel()

	port := &stubManifestPort{
		stubFanOut: &stubFanOut{candidates: []PackageHolderCandidate{
			{ScopeID: "security-alert:github:acme/api", Ecosystem: "npm", PackageName: "lodash", PackageID: "npm://registry.npmjs.org/lodash"},
			{ScopeID: "security-alert:github:acme/web", Ecosystem: "npm", PackageName: "react", PackageID: "npm://registry.npmjs.org/react"},
		}},
		generations: []ManifestDependencyGeneration{activeFullChange()},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"security-alert:github:acme/api"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v (react is unchanged)", port.reopened, want)
	}
	wantQueries := []PackageHolderQuery{{Ecosystems: []string{"npm"}, RepositoryIDs: []string{"repository:r_checkout"}}}
	if !reflect.DeepEqual(port.candidateQueries, wantQueries) {
		t.Fatalf("holder read queries = %+v, want %+v", port.candidateQueries, wantQueries)
	}
}
