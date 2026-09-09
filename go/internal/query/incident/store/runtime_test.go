// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"strings"
	"testing"
)

// TestRuntimeEvidenceRequiresAttachedPorts pins the loud failures the S2 move
// introduced: the old store built concretes lazily, so a bare store always
// worked, while the moved store fails fast naming the missing port. None of
// these calls reach the database, so a nil queryer suffices.
func TestRuntimeEvidenceRequiresAttachedPorts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewStore(nil)

	if _, err := store.readIncidentServiceCatalogCorrelations(ctx, incidentServiceCatalogOperationalLink{}); err == nil ||
		!strings.Contains(err.Error(), "incident service catalog store is required") {
		t.Errorf("catalog read without port = %v, want the required-store error", err)
	}
	if _, err := store.readIncidentContainerImageIdentities(ctx, "repo-1"); err == nil ||
		!strings.Contains(err.Error(), "incident container image store is required") {
		t.Errorf("image read without port = %v, want the required-store error", err)
	}
	if _, err := store.readIncidentCICDRunCorrelations(ctx, incidentContainerImageIdentity{Digest: "sha256:abc"}); err == nil ||
		!strings.Contains(err.Error(), "incident CI/CD run correlation store is required") {
		t.Errorf("cicd read without port = %v, want the required-store error", err)
	}
}
