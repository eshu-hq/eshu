// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"testing"
)

// Dedup and row-order determinism tests split out of image_overrides_test.go
// to keep that file under the repo's 500-line package-file cap (issue
// #5440, following the same split precedent as engine_yaml_semantics_test.go
// / engine_yaml_semantics_kustomize_test.go).

// TestParseHelmValuesImageOverridesDedupesExactDuplicateRows decides and pins
// the duplicate-row question (issue #5440 review): when a Helm values file
// declares the SAME repository under two different "image:" blocks with an
// identical tag/digest, the two resulting rows are byte-for-byte identical --
// image_overrides carries no "declared under" field to distinguish them, so
// shipping both would be pure phantom noise. helm_values[].image_repositories
// already dedupes (deduplicateStrings, helm.go); image_overrides follows the
// same principle: dedupe exact-identical rows, but keep rows that differ in
// ANY field (a second block declaring the same repository under a different
// tag is a genuinely distinct declaration, not noise).
func TestParseHelmValuesImageOverridesDedupesExactDuplicateRows(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values.yaml", `
serviceA:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "1.0.0"
serviceB:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "1.0.0"
serviceC:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "2.0.0"
`)
	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	overrides := yamlBucketForTest(t, got, "image_overrides")
	// Exactly 2 rows: the serviceA/serviceB pair collapses to one (identical
	// repository AND tag), serviceC survives as a distinct row (same
	// repository, different tag).
	if len(overrides) != 2 {
		t.Fatalf("len(image_overrides) = %d, want 2 (exact duplicate collapsed, differing tag kept): %#v", len(overrides), overrides)
	}
	tags := map[string]int{}
	for _, row := range overrides {
		name, _ := row["name"].(string)
		if name != "ghcr.io/example/shared-sidecar" {
			t.Fatalf("unexpected row name = %q in %#v", name, overrides)
		}
		tag, _ := row["tag"].(string)
		tags[tag]++
	}
	if tags["1.0.0"] != 1 {
		t.Fatalf("tag 1.0.0 count = %d, want 1 (deduped): %#v", tags["1.0.0"], overrides)
	}
	if tags["2.0.0"] != 1 {
		t.Fatalf("tag 2.0.0 count = %d, want 1 (kept, distinct tag): %#v", tags["2.0.0"], overrides)
	}
}

// TestImageOverrideKeyStaysInSyncWithRowShape is a structural drift guard
// (issue #5440 review): dedupeImageOverrideRows detects an exact-duplicate
// row by comparing every field named in imageOverrideRowFields
// (image_overrides.go), read individually rather than formatted into a
// string. If a row builder ever grows a new field with no matching addition
// to that list, dedup would silently ignore the new field and could wrongly
// collapse two rows that actually differ. This test cannot catch a field
// RENAME, but it catches the far more common drift: a field ADDED to a row
// with no matching addition to imageOverrideRowFields, by asserting the two
// field counts stay equal.
func TestImageOverrideKeyStaysInSyncWithRowShape(t *testing.T) {
	t.Parallel()

	row := helmImageOverrideRow(
		map[string]any{"repository": "ghcr.io/example/checkout-service", "tag": "1.2.3"},
		"values.yaml",
		"prod",
	)
	if row == nil {
		t.Fatal("helmImageOverrideRow() = nil, want a row for a valid image map")
	}

	if got, want := len(imageOverrideRowFields), len(row); got != want {
		t.Fatalf(
			"imageOverrideRowFields has %d entries but an image_overrides row has %d keys -- "+
				"dedupeImageOverrideRows's field list (image_overrides.go) is out of sync with "+
				"the row shape; add the missing field to imageOverrideRowFields or a new row "+
				"field will silently escape duplicate-row comparison",
			got, want,
		)
	}
}

// TestParseHelmValuesImageOverridesRowOrderIsDeterministic is a regression
// guard for a P1 nondeterminism defect (independent review, issue #5440):
// two Helm image_overrides rows that tie on the ONLY two keys
// shared.SortNamedBucket sorts by -- line_number, which Helm hardcodes to 1
// (helmImageOverrideRow), and name, which is identical here since both
// "image:" blocks declare the SAME repository under different sibling
// `tag:` keys -- used to fall through to whatever order they arrived in
// from collectHelmImageOverrides's map walk. Go deliberately randomizes map
// iteration order per call, so that arrival order varied run to run before
// this fix (reviewer reproduction: 300 parses of the byte-identical input
// produced 5 different row orderings).
//
// slices.SortFunc (shared.SortNamedMaps) is documented as NOT stable, so it
// does not resolve the tie itself -- it only produces a repeatable OUTPUT
// once its INPUT order is already repeatable, which
// sortImageOverrideRowsByContent (image_overrides.go) now guarantees. This
// parses the identical tied-name fixture 100 times in one process and
// asserts every parse produced the identical row order.
func TestParseHelmValuesImageOverridesRowOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values.yaml", `
serviceA:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "1.0.0"
serviceB:
  image:
    repository: ghcr.io/example/shared-sidecar
    tag: "2.0.0"
`)

	const iterations = 100
	var firstOrder [2]string
	for i := range iterations {
		got, err := Parse(filePath, false, Options{})
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil (iteration %d)", err, i)
		}
		rows := yamlBucketForTest(t, got, "image_overrides")
		if len(rows) != 2 {
			t.Fatalf(
				"len(image_overrides) = %d, want 2 (fixture must tie without deduping) (iteration %d): %#v",
				len(rows), i, rows,
			)
		}
		var order [2]string
		for j, row := range rows {
			tag, _ := row["tag"].(string)
			order[j] = tag
		}
		if i == 0 {
			firstOrder = order
			continue
		}
		if order != firstOrder {
			t.Fatalf(
				"row order changed across parses of the identical file: iteration 0 tag order = %v, "+
					"iteration %d tag order = %v -- image_overrides row order must be a deterministic "+
					"function of content, not of Go's per-call-randomized map iteration order",
				firstOrder, i, order,
			)
		}
	}
}
