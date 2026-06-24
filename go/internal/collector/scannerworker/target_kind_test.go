// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestNewClaimInputAcceptsSBOMGenerationTargetKinds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		kind TargetKind
	}{
		{name: "repository", kind: TargetRepository},
		{name: "image", kind: TargetImage},
		{name: "artifact", kind: TargetArtifact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testScannerWorkItemForTargetKind(tc.kind)
			claim := testScannerClaim(item)
			target := testTargetScope(item)
			target.Kind = tc.kind

			input, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, target, testResourceLimits())
			if err != nil {
				t.Fatalf("NewClaimInput(%q) error = %v, want nil", tc.kind, err)
			}
			if input.Target.Kind != tc.kind {
				t.Fatalf("Target.Kind = %q, want %q", input.Target.Kind, tc.kind)
			}
		})
	}
}

func TestTargetScopeFromWorkItemDerivesImageAndArtifactTargets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		kind TargetKind
	}{
		{name: "repository", kind: TargetRepository},
		{name: "image", kind: TargetImage},
		{name: "artifact", kind: TargetArtifact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testScannerWorkItemForTargetKind(tc.kind)
			target, err := TargetScopeFromWorkItem(item)
			if err != nil {
				t.Fatalf("TargetScopeFromWorkItem(%q) error = %v, want nil", tc.kind, err)
			}
			if target.Kind != tc.kind {
				t.Fatalf("Kind = %q, want %q", target.Kind, tc.kind)
			}
			if target.ScopeID != item.ScopeID {
				t.Fatalf("ScopeID = %q, want %q", target.ScopeID, item.ScopeID)
			}
		})
	}
}

func TestTargetScopeFromWorkItemDerivesTargetKindFromSourceURI(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		scopeID string
		want    TargetKind
	}{
		{name: "image uri", scopeID: "image://registry.example/team/app@sha256:abc", want: TargetImage},
		{name: "artifact uri", scopeID: "artifact://builds.example/releases/app.tar.gz", want: TargetArtifact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testScannerWorkItem()
			item.ScopeID = tc.scopeID
			item.AcceptanceUnitID = "subject:sha256:abc"
			target, err := TargetScopeFromWorkItem(item)
			if err != nil {
				t.Fatalf("TargetScopeFromWorkItem(%q) error = %v, want nil", tc.scopeID, err)
			}
			if target.Kind != tc.want {
				t.Fatalf("Kind = %q, want %q", target.Kind, tc.want)
			}
		})
	}
}

func testScannerWorkItemForTargetKind(kind TargetKind) workflow.WorkItem {
	item := testScannerWorkItem()
	item.ScopeID = "scanner-worker://" + string(kind) + "/target-private-name"
	item.AcceptanceUnitID = string(kind) + ":target-123"
	item.FairnessKey = "scanner_worker:collector-scanner:" + string(kind)
	return item
}
