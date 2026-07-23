// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// gomodRequireMetadata builds the entity_metadata a go.mod require row
// (gomod/parser.go's goRequireRow) contributes. "value" is the row's raw
// declared version — the field dependencyIdentityDiscriminator's "go" case
// uses to disambiguate two same-section, same-module duplicate requires.
func gomodRequireMetadata(section, value string) map[string]any {
	return map[string]any{
		"config_kind":     "dependency",
		"package_manager": "go",
		"section":         section,
		"value":           value,
	}
}

// TestCanonicalEntityIDWithMetadataGomodAdmitsInScopeRow proves an ordinary
// go.mod require row routes to the section-keyed scheme.
func TestCanonicalEntityIDWithMetadataGomodAdmitsInScopeRow(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "go.mod"
		name   = "github.com/pkg/errors"
		line   = 6
	)
	metadata := gomodRequireMetadata("require", "v0.9.1")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope gomod row", got)
	}
}

// TestCanonicalEntityIDWithMetadataGomodReorderNoChurn proves a require row's
// identity is stable when its line moves (e.g. an unrelated require added
// above it in the same require(...) block).
func TestCanonicalEntityIDWithMetadataGomodReorderNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "go.mod"
		name   = "github.com/pkg/errors"
	)
	metadata := gomodRequireMetadata("require", "v0.9.1")

	before := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 4, metadata)
	after := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 40, metadata)
	if before != after {
		t.Fatalf("reordering changed the gomod require id: line 4 = %q, line 40 = %q", before, after)
	}
}

// TestCanonicalEntityIDWithMetadataGomodDuplicateRequireVersionDistinctness is
// the P1 regression this test guards: golang.org/x/mod's modfile.Parse (the
// parser gomod/parser.go:59 calls) does NOT de-duplicate require directives —
// its own docs say that is left to higher-level MVS logic — so a
// hand-edited or merge-conflicted go.mod that has not been run through
// `go mod tidy` can legitimately contain
//
//	require (
//	    github.com/pkg/errors v0.9.1
//	    github.com/pkg/errors v0.8.0
//	)
//
// producing two rows that share (section="require", name="github.com/pkg/
// errors"). Under the pre-#5507 line-keyed scheme these had different line
// numbers and therefore different ids, so both survived. Without a
// discriminator here, #5507's section-keyed scheme would silently collapse
// them into ONE id — and internal/storage/postgres's content_writer.go
// dedupe would then drop one of the two declarations with no error or
// telemetry, a real regression. The "value" (raw declared version)
// discriminator keeps them distinct.
func TestCanonicalEntityIDWithMetadataGomodDuplicateRequireVersionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "go.mod"
		name   = "github.com/pkg/errors"
	)

	newer := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 4,
		gomodRequireMetadata("require", "v0.9.1"))
	older := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 5,
		gomodRequireMetadata("require", "v0.8.0"))

	if newer == older {
		t.Fatalf("two duplicate go.mod requires of the same module at different versions collapsed into one id: %q", newer)
	}
}

// TestCanonicalEntityIDWithMetadataGomodSameVersionDuplicateCollapses is the
// positive counterpart: two require rows for the same module at the exact
// same version (a redundant, content-identical duplicate — the only case
// where nothing distinguishes the declarations) still collapse to one id,
// same as every other no-more-information-available merge case in this
// package. This is not a regression: identical declarations carry no
// distinguishing evidence to preserve.
func TestCanonicalEntityIDWithMetadataGomodSameVersionDuplicateCollapses(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "go.mod"
		name   = "github.com/pkg/errors"
	)

	first := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 4,
		gomodRequireMetadata("require", "v0.9.1"))
	second := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 9,
		gomodRequireMetadata("require", "v0.9.1"))

	if first != second {
		t.Fatalf("two identical (module, version) require declarations unexpectedly diverged: %q vs %q", first, second)
	}
}

// TestCanonicalEntityIDWithMetadataGomodCrossSectionDistinctness proves a
// module required directly ("require") stays distinct from the same module
// path appearing in "require-indirect".
func TestCanonicalEntityIDWithMetadataGomodCrossSectionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "go.mod"
		name   = "golang.org/x/sys"
		value  = "v0.20.0"
	)

	direct := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 4,
		gomodRequireMetadata("require", value))
	indirect := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 4,
		gomodRequireMetadata("require-indirect", value))

	if direct == indirect {
		t.Fatalf("require and require-indirect sections collapsed into one id: %q", direct)
	}
}

// TestCanonicalEntityIDWithMetadataGomodReplaceStaysLineKeyed proves that
// go.mod `replace` rows (config_kind=="dependency_replace", not
// "dependency") are correctly excluded from the section-keyed scheme by
// condition 2 of the gate — they were never in scope for #5507 in the first
// place, since a replace directive's target module/version, not just its old
// path, is what makes two replace declarations distinct, and the row's
// config_kind already keeps it off this migration's surface entirely.
func TestCanonicalEntityIDWithMetadataGomodReplaceStaysLineKeyed(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "go.mod"
		name   = "github.com/pkg/errors"
		line   = 12
	)
	metadata := map[string]any{
		"config_kind":     "dependency_replace",
		"package_manager": "go",
		"section":         "replace",
	}

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	want := CanonicalEntityID(repoID, path, "Variable", name, line)
	if got != want {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q, want legacy CanonicalEntityID() = %q for a dependency_replace row", got, want)
	}
}
