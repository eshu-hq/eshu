// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestRefOverlayFactKindsAndSchemaVersions pins the Phase 1 (#5393)
// isolated-evidence fact kinds: the branch observation kind and the bounded
// config_key_divergence kind. Both are contract-only in this leaf: emission
// (ref extraction) and the read surface land in sibling Phase 1 leaves.
func TestRefOverlayFactKindsAndSchemaVersions(t *testing.T) {
	t.Parallel()

	kinds := RefOverlayFactKinds()
	wantKinds := []string{
		RefOverlayBranchObservationFactKind,
		ConfigKeyDivergenceFactKind,
	}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("len(RefOverlayFactKinds()) = %d, want %d", len(kinds), len(wantKinds))
	}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Fatalf("RefOverlayFactKinds()[%d] = %q, want %q", i, kinds[i], want)
		}
		version, ok := RefOverlaySchemaVersion(want)
		if !ok || version != RefOverlaySchemaVersionV1 {
			t.Fatalf("RefOverlaySchemaVersion(%q) = %q, %v, want %q, true", want, version, ok, RefOverlaySchemaVersionV1)
		}
	}
}

func TestRefOverlayFactKindsReturnsCopy(t *testing.T) {
	t.Parallel()

	kinds := RefOverlayFactKinds()
	kinds[0] = "mutated"

	if got := RefOverlayFactKinds()[0]; got != RefOverlayBranchObservationFactKind {
		t.Fatalf("RefOverlayFactKinds()[0] = %q, want %q", got, RefOverlayBranchObservationFactKind)
	}
}

// TestRefOverlayBranchObservationKindIsNamespaced pins the branch
// observation kind to the ref_overlay family namespace: branch data is a
// distinct evidence class and must never alias a canonical kind string.
func TestRefOverlayBranchObservationKindIsNamespaced(t *testing.T) {
	t.Parallel()

	if !strings.HasPrefix(RefOverlayBranchObservationFactKind, "ref_overlay.") {
		t.Fatalf("branch observation kind %q is not in the ref_overlay namespace", RefOverlayBranchObservationFactKind)
	}
}

// TestConfigKeyDivergenceKindName pins the exact wire string the epic names:
// config_key_divergence, bare, so sibling leaves and the registry agree
// byte-for-byte.
func TestConfigKeyDivergenceKindName(t *testing.T) {
	t.Parallel()

	if ConfigKeyDivergenceFactKind != "config_key_divergence" {
		t.Fatalf("ConfigKeyDivergenceFactKind = %q, want %q", ConfigKeyDivergenceFactKind, "config_key_divergence")
	}
}

// TestRefOverlaySchemaVersionAdmitsV1 proves the new kinds are enrolled in
// schema-version admission: ValidateSchemaVersion must accept 1.0.0 for both
// and reject an unknown kind.
func TestRefOverlaySchemaVersionAdmitsV1(t *testing.T) {
	t.Parallel()

	for _, kind := range RefOverlayFactKinds() {
		if err := ValidateSchemaVersion(kind, RefOverlaySchemaVersionV1); err != nil {
			t.Fatalf("ValidateSchemaVersion(%q, %q) error = %v, want nil", kind, RefOverlaySchemaVersionV1, err)
		}
		if got := ClassifySchemaVersion(kind, RefOverlaySchemaVersionV1); got != CompatibilitySupported {
			t.Fatalf("ClassifySchemaVersion(%q, %q) = %v, want CompatibilitySupported", kind, RefOverlaySchemaVersionV1, got)
		}
	}
}

// TestRefOverlayScopeID pins the ref-qualified scope from the #5393
// isolation contract: the canonical scope prefix plus repo_id plus "@<ref>.
// The scope carries the branch dimension so overlay facts can never join
// canonical (bare repo_id) rows. The want side is built from the same
// single-sourced prefix so this test cannot drift from it.
func TestRefOverlayScopeID(t *testing.T) {
	t.Parallel()

	got := RefOverlayScopeID("github.com/eshu-hq/eshu", "env/prod")
	want := scope.GitRepositoryScopePrefix + "github.com/eshu-hq/eshu@env/prod"
	if got != want {
		t.Fatalf("RefOverlayScopeID = %q, want %q", got, want)
	}
}

// TestRefOverlayScopeIDNeverCollidesWithBareRepoScope proves the overlay
// scope is never equal to the bare repo_id canonical rows key on: without
// the @<ref> qualifier an overlay fact could join canonical truth.
func TestRefOverlayScopeIDNeverCollidesWithBareRepoScope(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"main", "env/prod", "v1.2.3", "a1b2c3d4"} {
		got := RefOverlayScopeID("repo-1", ref)
		if got == scope.GitRepositoryScopePrefix+"repo-1" {
			t.Fatalf("RefOverlayScopeID(repo-1, %q) collides with the bare repo scope", ref)
		}
		if !strings.Contains(got, "@"+ref) {
			t.Fatalf("RefOverlayScopeID(repo-1, %q) = %q, missing the @<ref> qualifier", ref, got)
		}
		if !strings.HasPrefix(got, scope.GitRepositoryScopePrefix) {
			t.Fatalf("RefOverlayScopeID(repo-1, %q) = %q, missing the canonical scope prefix", ref, got)
		}
	}
}
