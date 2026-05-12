package terraformstate_test

// This file locks the contract that the state-snapshot scope ID built by
// scope.NewTerraformStateSnapshotScope and the canonical join key returned by
// terraformstate.ScopeLocatorHash agree byte-for-byte for the same backend +
// locator. The drift resolver compares these two hashes byte-for-byte at
// go/internal/storage/postgres/tfstate_backend_canonical.go vs the state-side
// scope hash parsed at go/internal/reducer/terraform_config_state_drift.go.
//
// Issue #203: hashStateLocator (scope side) and the original LocatorHash
// (collector side) hashed the same backend+locator differently for empty
// VersionID. The drift handler used the former to build the state-snapshot
// scope ID and the latter (via the canonical adapter) to find the owning
// config repo, so on real S3-backed deployments every drift candidate
// silently rejected with failure_class="no_config_repo_owns_backend".
//
// The fix introduces ScopeLocatorHash for the version-agnostic join key and
// keeps LocatorHash as the per-version identity that backs CandidatePlanningID
// and the persisted terraform_state_snapshot.payload->>'locator_hash' field.
// scope cannot import terraformstate, so the alignment is enforced by this
// cross-package contract test, not a shared helper.

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestScopeLocatorHashAgreesWithStateSnapshotScopeID asserts that
// ScopeLocatorHash equals the hex value embedded in the durable
// state-snapshot scope ID for the same backend+locator. This is the
// load-bearing contract for the drift resolver join: if the two diverge,
// every drift intent for that backend silently rejects.
func TestScopeLocatorHashAgreesWithStateSnapshotScopeID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		backendKind terraformstate.BackendKind
		locator     string
	}{
		{
			name:        "s3 single-segment key",
			backendKind: terraformstate.BackendS3,
			locator:     "s3://eshu-drift-b/prod/terraform.tfstate",
		},
		{
			name:        "s3 nested key",
			backendKind: terraformstate.BackendS3,
			locator:     "s3://tfstate-prod/services/api/envs/prod/terraform.tfstate",
		},
		{
			name:        "s3 minimal key",
			backendKind: terraformstate.BackendS3,
			locator:     "s3://b/k",
		},
		{
			name:        "local absolute path",
			backendKind: terraformstate.BackendLocal,
			locator:     "/workspace/terraform.tfstate",
		},
		{
			name:        "local nested path",
			backendKind: terraformstate.BackendLocal,
			locator:     "/srv/eshu/state/envs/prod/terraform.tfstate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scopeValue, err := scope.NewTerraformStateSnapshotScope(
				"repo-scope-test",
				string(tc.backendKind),
				tc.locator,
				nil,
			)
			if err != nil {
				t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
			}

			expectedPrefix := "state_snapshot:" + string(tc.backendKind) + ":"
			scopeHash, ok := strings.CutPrefix(scopeValue.ScopeID, expectedPrefix)
			if !ok {
				t.Fatalf("ScopeID %q does not start with %q", scopeValue.ScopeID, expectedPrefix)
			}

			joinHash := terraformstate.ScopeLocatorHash(tc.backendKind, tc.locator)

			if scopeHash != joinHash {
				t.Fatalf(
					"scope-side hash %q != canonical join hash %q for backend=%s locator=%s; "+
						"drift resolver join would silently reject every candidate. "+
						"See go/internal/scope/tfstate.go:hashStateLocator and "+
						"go/internal/collector/terraformstate/identity.go:ScopeLocatorHash.",
					scopeHash, joinHash, tc.backendKind, tc.locator,
				)
			}

			if scopeHash != scopeValue.Metadata["locator_hash"] {
				t.Fatalf(
					"scope ID locator hash %q disagrees with scope metadata locator_hash %q",
					scopeHash, scopeValue.Metadata["locator_hash"],
				)
			}
		})
	}
}

// TestScopeLocatorHashCollapsesAcrossVersionIDVariation guards the
// version-agnostic property of the join key by demonstrating it against its
// per-version sibling. The state-snapshot scope is intentionally
// version-agnostic — multiple S3 versions of the same state file collapse
// into one scope, with per-version generations carrying the lineage and
// serial — so the join key MUST follow the same identity rule.
//
// The function signature already excludes VersionID structurally;
// ScopeLocatorHash(BackendKind, Locator) takes no version input. This test
// pairs it with LocatorHash, which DOES vary by VersionID, to prove the
// behavioral contract: two LocatorHash inputs that differ only in VersionID
// produce different per-version identities while the corresponding
// ScopeLocatorHash for the same backend+locator stays fixed. If the scope
// hash ever started depending on VersionID, the drift resolver join would
// shatter into per-version buckets and silently reject every candidate.
func TestScopeLocatorHashCollapsesAcrossVersionIDVariation(t *testing.T) {
	t.Parallel()

	const locator = "s3://tfstate-prod/services/api/terraform.tfstate"

	scopeHashFirst := terraformstate.ScopeLocatorHash(terraformstate.BackendS3, locator)
	scopeHashSecond := terraformstate.ScopeLocatorHash(terraformstate.BackendS3, locator)
	if scopeHashFirst != scopeHashSecond {
		t.Fatalf("ScopeLocatorHash is non-deterministic for the same backend+locator: %q vs %q",
			scopeHashFirst, scopeHashSecond)
	}

	versionA := terraformstate.LocatorHash(terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     locator,
		VersionID:   "version-a",
	})
	versionB := terraformstate.LocatorHash(terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     locator,
		VersionID:   "version-b",
	})
	if versionA == versionB {
		t.Fatalf("LocatorHash failed to distinguish VersionID values; cannot prove the "+
			"scope-hash collapse property without a version-sensitive baseline. got %q for both",
			versionA)
	}

	// The scope hash for the same backend+locator must remain identical even
	// though the per-version identities for that backend+locator span two
	// distinct VersionID values. This is the load-bearing property: the
	// drift-resolver join is one row per backend+locator, not per S3 object
	// version.
	if got := terraformstate.ScopeLocatorHash(terraformstate.BackendS3, locator); got != scopeHashFirst {
		t.Fatalf("ScopeLocatorHash drifted while LocatorHash spanned two VersionID values: "+
			"got %q, want %q", got, scopeHashFirst)
	}
}

// TestLocatorHashRetainsVersionIdentity guards the per-version property of
// LocatorHash. CandidatePlanningID (candidate_identity.go) and the workflow
// coordinator depend on two S3 versions of the same state file producing
// distinct planning identities so each version becomes its own work item.
// If LocatorHash ever drops VersionID, the workflow coordinator silently
// conflates two distinct work items.
func TestLocatorHashRetainsVersionIdentity(t *testing.T) {
	t.Parallel()

	base := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
		VersionID:   "version-a",
	}
	withDifferentVersion := base
	withDifferentVersion.VersionID = "version-b"

	if terraformstate.LocatorHash(base) == terraformstate.LocatorHash(withDifferentVersion) {
		t.Fatalf("LocatorHash must distinguish S3 object versions; got %q for both",
			terraformstate.LocatorHash(base))
	}
}

// TestLocatorHashAndScopeLocatorHashAreDistinctFunctions documents the
// intentional split: LocatorHash digests (BackendKind, Locator, VersionID)
// for per-candidate identity (CandidatePlanningID, persisted snapshot fact
// payload). ScopeLocatorHash digests (BackendKind, Locator) for the
// version-agnostic drift-resolver join key. The two are distinct hash
// functions and produce different outputs for the same inputs even when
// VersionID is empty (because LocatorHash always appends a trailing
// separator + VersionID, even when VersionID is the empty string). Mixing
// the two at the join layer is exactly what caused issue #203.
func TestLocatorHashAndScopeLocatorHashAreDistinctFunctions(t *testing.T) {
	t.Parallel()

	const locator = "s3://tfstate-prod/services/api/terraform.tfstate"
	withoutVersion := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     locator,
	}
	withVersion := withoutVersion
	withVersion.VersionID = "v-1"

	scopeHash := terraformstate.ScopeLocatorHash(terraformstate.BackendS3, locator)

	// Even with empty VersionID, LocatorHash appends "\x00" + "" and so
	// produces a different digest than ScopeLocatorHash. This is the exact
	// divergence that broke production: tfstate_backend_canonical.go used to
	// compute the canonical join key with LocatorHash(StateKey{VersionID:""})
	// and the drift handler parsed the scope hash built by
	// scope.NewTerraformStateSnapshotScope; the two never matched.
	if got := terraformstate.LocatorHash(withoutVersion); got == scopeHash {
		t.Fatalf("LocatorHash(empty VersionID) %q == ScopeLocatorHash %q; this is "+
			"unexpected. The two functions are intentionally distinct so the per-version "+
			"identity stays separate from the scope-level join key. If the formulas "+
			"converge, audit every caller that depends on the version split.",
			got, scopeHash)
	}
	if got := terraformstate.LocatorHash(withVersion); got == scopeHash {
		t.Fatalf("LocatorHash(non-empty VersionID) %q must NOT equal ScopeLocatorHash %q; "+
			"per-version identity collapsing into the scope hash defeats the workflow "+
			"coordinator's per-version work item dispatch", got, scopeHash)
	}
}

// TestScopeLocatorHashIsDeterministicAcrossBackendKinds is a small
// belt-and-suspenders test that exercises every supported BackendKind and
// confirms ScopeLocatorHash is stable under repeated calls with the same
// input.
func TestScopeLocatorHashIsDeterministicAcrossBackendKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		backendKind terraformstate.BackendKind
		locator     string
	}{
		{terraformstate.BackendS3, "s3://b/k"},
		{terraformstate.BackendLocal, "/srv/state/terraform.tfstate"},
	}

	for _, tc := range cases {
		first := terraformstate.ScopeLocatorHash(tc.backendKind, tc.locator)
		second := terraformstate.ScopeLocatorHash(tc.backendKind, tc.locator)
		if first != second {
			t.Fatalf("ScopeLocatorHash(%s, %q) is non-deterministic: %q vs %q",
				tc.backendKind, tc.locator, first, second)
		}
		if len(first) != 64 {
			t.Fatalf("ScopeLocatorHash(%s, %q) length = %d, want 64 hex chars",
				tc.backendKind, tc.locator, len(first))
		}
	}
}
