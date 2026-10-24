// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import (
	"slices"
	"testing"
)

// paramSource returns the list a derived list is computed from: a normalized
// twin from its base list, a resolved scope list from its repository list,
// and a generation list from the scope list it is paired with. Each derived
// list is empty exactly when its source is, which is why a branch may gate on
// the source alone. Base lists and the suppression cap have no source.
func paramSource(p Param) (Param, bool) {
	for base := PackageIDs; base < paramEnd; base++ {
		if twin, ok := base.LowerTwin(); ok && twin == p {
			return base, true
		}
	}
	switch p {
	case RepositoryScopeIDs:
		return RepositoryIDs, true
	case RepositoryGenerationIDs:
		return RepositoryScopeIDs, true
	case FileRepositoryScopeIDs:
		return FileRepositoryIDs, true
	case FileRepositoryGenerationIDs:
		return FileRepositoryScopeIDs, true
	default:
		return 0, false
	}
}

// TestEveryParamIsClassified pins the base/derived split that baseParams and
// RunWhen rely on. Every Param is exactly one of: a base list (IsBase), a
// derived list with a source, or the suppression cap, which is not an input.
// A list added to the const block without a classification fails here.
func TestEveryParamIsClassified(t *testing.T) {
	t.Parallel()
	for p := PackageIDs; p < paramEnd; p++ {
		_, derived := paramSource(p)
		classes := 0
		for _, in := range []bool{p.IsBase(), derived, p == SuppressionLimit} {
			if in {
				classes++
			}
		}
		if classes != 1 {
			t.Errorf("Param %d: base=%v derived=%v cap=%v, want exactly one", p, p.IsBase(), derived, p == SuppressionLimit)
		}
		if twin, ok := p.LowerTwin(); ok && !p.IsBase() {
			t.Errorf("Param %d has a normalized twin %d but is not a base list", p, twin)
		}
	}
}

// TestBranchRunWhenCoversEveryBoundInput is the gating invariant: the loader
// skips a branch when every RunWhen list is empty, so every list a branch
// binds must be covered by RunWhen, directly or through its source. A base
// list missing from RunWhen would let the branch skip while that list alone
// is non-empty and drop its rows.
func TestBranchRunWhenCoversEveryBoundInput(t *testing.T) {
	t.Parallel()
	for _, branch := range Branches() {
		if len(branch.RunWhen) == 0 {
			t.Errorf("%s: RunWhen is empty", branch.Name)
		}
		for _, p := range branch.RunWhen {
			if !slices.Contains(branch.Params, p) {
				t.Errorf("%s: RunWhen gates on Param %d, which the statement does not bind", branch.Name, p)
			}
		}
		for _, p := range branch.Params {
			if p == SuppressionLimit {
				continue
			}
			covered := p
			for !slices.Contains(branch.RunWhen, covered) {
				source, ok := paramSource(covered)
				if !ok {
					t.Errorf("%s: binds Param %d without gating on it or on its source", branch.Name, p)
					break
				}
				covered = source
			}
		}
	}
}
