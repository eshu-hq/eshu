// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"slices"
	"strings"
)

// This file builds the image_overrides parsed_file_data bucket (issue
// #5440): per-image container tag/digest version truth for Helm values
// "image:" blocks and Kustomize kustomization.yaml "images:" lists. Both
// existing producers deliberately discard this information from their own
// buckets -- helm_values[].image_repositories strips tag/digest
// (normalizeContainerImageRepository, helm.go) because it is a stable
// image-identity list with downstream consumers, and
// kustomize_overlays[].image_refs (collectKustomizeObjectRefs,
// kustomize_semantics.go) never reads newTag/digest at all. image_overrides
// is additive and must never change either bucket's existing output.
//
// The `environment` field's resolution logic (helmImageOverrideEnvironment,
// imageOverrideDirectoryEnvironment, helmValuesEnvironment, and the token
// allowlist) lives in image_overrides_environment.go, split out to keep
// both files under the repo's 500-line package-file cap (issue #5440
// round-3 review).
//
// Consumer routing (verified against live issue bodies, round-4 review; an
// earlier round of this work cited issue #5441 here and at seven other
// sites -- #5441 is "iac: persist relationship Details and Terraform
// attributes at the graph write" and has nothing to do with this bucket, a
// misattribution corrected across all eight sites in the same commit as
// this comment): this bucket has no consumer yet. Issue #5445 ("contract
// the extraction surface: registry entries + typed accessors") governs the
// typed-accessor CONTRACT this bucket follows -- registering Helm/
// Kustomize/Terraform/ArgoCD parsed_file_data shapes behind typed
// accessors instead of raw map reads. Issue #5469 ("vuln: tiered
// deployed-version resolution") aims to judge a vulnerability finding's
// version from the strongest available tier, including branch-resolved
// manifest evidence; a Helm/Kustomize declared image tag/digest (this
// bucket's data) is the kind of evidence that tier would use, though #5469
// does not yet name this bucket explicitly -- do not read that as a
// commitment. Graph projection of this bucket (a node label and reducer
// materialization) has no assigned issue.

// collectHelmImageOverrides walks a Helm values document for maps nested
// under an "image" parent key, mirroring collectHelmImageRepositories's walk
// exactly (helm.go), and emits one image_overrides row per declared image.
//
// The walk itself visits map keys via Go's `for key, item := range typed`,
// whose iteration order Go deliberately randomizes per call -- so the raw
// walk order is NOT stable across repeated parses of the identical file.
// sortImageOverrideRowsByContent below fixes that before returning: see its
// doc comment for why an unstable pre-sort input defeats determinism even
// though the caller's own final bucket sort (shared.SortNamedBucket) is a
// deterministic algorithm.
func collectHelmImageOverrides(document map[string]any, path string, environment string) []map[string]any {
	var rows []map[string]any

	var walk func(parentKey string, value any)
	walk = func(parentKey string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			if strings.EqualFold(parentKey, "image") {
				if row := helmImageOverrideRow(typed, path, environment); row != nil {
					rows = append(rows, row)
				}
			}
			for key, item := range typed {
				walk(key, item)
			}
		case []any:
			for _, item := range typed {
				walk(parentKey, item)
			}
		}
	}

	walk("", document)
	sortImageOverrideRowsByContent(rows)
	return dedupeImageOverrideRows(rows)
}

// helmImageOverrideRow builds one image_overrides row from a single Helm
// "image:" map. It returns nil when the map carries no repository value --
// an "image" key with no repository is not a declared image override.
//
// name is the repository field exactly as declared (which may itself carry
// an inline tag or digest, e.g. "repo:v1"); repository is the resolved,
// version-stripped identity produced by the existing, unmodified
// normalizeContainerImageRepository. A sibling `tag:`/`digest:` key wins over
// a version parsed out of the inline repository string, matching how a real
// Helm chart's values resolve when both forms are present.
func helmImageOverrideRow(image map[string]any, path string, environment string) map[string]any {
	rawRepository := cleanString(image["repository"])
	if rawRepository == "" {
		return nil
	}
	repository := normalizeContainerImageRepository(rawRepository)
	if repository == "" {
		return nil
	}
	tag, digest := parseInlineImageVersion(rawRepository)
	if sibling := cleanString(image["tag"]); sibling != "" {
		tag = sibling
	}
	if sibling := cleanString(image["digest"]); sibling != "" {
		digest = sibling
	}
	return map[string]any{
		"name":        rawRepository,
		"repository":  repository,
		"tag":         tag,
		"digest":      digest,
		"environment": environment,
		"source":      "helm",
		"path":        path,
		"line_number": 1,
		"lang":        "yaml",
	}
}

// parseInlineImageVersion extracts a tag or digest embedded directly in a
// container image repository string ("repo:v1" or "repo@sha256:abc..."). It
// mirrors normalizeContainerImageRepository's own stripping rule (helm.go)
// so the two functions agree on where the repository identity ends, but is
// intentionally independent -- neither function calls the other -- so a
// future change to one can never silently change the other's pinned output.
// image_repositories must stay byte-identical (issue #5440).
func parseInlineImageVersion(raw string) (tag string, digest string) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, `"`)
	if trimmed == "" || trimmed == "<nil>" {
		return "", ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		digest = strings.TrimSpace(trimmed[at+1:])
		trimmed = trimmed[:at]
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon > lastSlash {
		tag = trimmed[lastColon+1:]
	}
	return tag, digest
}

// collectKustomizeImageOverrides builds one image_overrides row per entry in
// a kustomization document's images[] list that actually declares a version
// override, capturing the newTag/digest fields collectKustomizeObjectRefs
// (kustomize_semantics.go) never reads.
//
// An entry with only `name` and none of `newName`/`newTag`/`digest` is a
// no-op match-target declaration -- Kustomize itself performs no image
// substitution for it -- so it is skipped rather than emitting a row that
// would tell a consumer "yes, a version override is declared" when the
// honest answer is "no version was declared" (issue #5440 review).
//
// This function's own build order follows the source images[] list order
// (a real list, not a randomized map walk like the Helm producer), but
// sortImageOverrideRowsByContent below is still applied for symmetry with
// collectHelmImageOverrides: relying on stable list-order input for one
// producer and content order for the other is a trap for the next reader,
// and two Kustomize entries sharing a `name` (a repository declared twice
// with different overrides) would hit the identical shared.SortNamedBucket
// tie the Helm producer can hit.
func collectKustomizeImageOverrides(document map[string]any, path string, environment string, lineNumber int) []map[string]any {
	items, ok := document["images"].([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := cleanString(object["name"])
		if name == "" {
			continue
		}
		newName := cleanString(object["newName"])
		newTag := cleanString(object["newTag"])
		digest := cleanString(object["digest"])
		if newName == "" && newTag == "" && digest == "" {
			continue
		}
		repository := newName
		if repository == "" {
			repository = name
		}
		rows = append(rows, map[string]any{
			"name":        name,
			"repository":  repository,
			"tag":         newTag,
			"digest":      digest,
			"environment": environment,
			"source":      "kustomize",
			"path":        path,
			"line_number": lineNumber,
			"lang":        "yaml",
		})
	}
	sortImageOverrideRowsByContent(rows)
	return dedupeImageOverrideRows(rows)
}

// imageOverrideRowFields lists every key an image_overrides row carries.
// imageOverrideRowsEqual below iterates it to compare two rows field by
// field, and TestImageOverrideKeyStaysInSyncWithRowShape
// (image_overrides_test.go) asserts its length matches a real row's key
// count -- both derive from this single list, so a row field added to
// helmImageOverrideRow/collectKustomizeImageOverrides with no matching
// addition here fails that test instead of silently escaping comparison.
// "line_number" is the only int; every other field is a string.
var imageOverrideRowFields = []string{
	"name", "repository", "tag", "digest", "environment",
	"source", "path", "lang", "line_number",
}

// imageOverrideRowsEqual reports whether two image_overrides rows are
// identical on every field in imageOverrideRowFields.
func imageOverrideRowsEqual(a, b map[string]any) bool {
	for _, field := range imageOverrideRowFields {
		if field == "line_number" {
			if rowInt(a, field) != rowInt(b, field) {
				return false
			}
			continue
		}
		if rowString(a, field) != rowString(b, field) {
			return false
		}
	}
	return true
}

// compareImageOverrideRows orders two image_overrides rows by comparing
// every field in imageOverrideRowFields, in that order -- reusing the same
// list dedupeImageOverrideRows compares on, rather than hardcoding a second
// field list that could silently drift from it.
func compareImageOverrideRows(a, b map[string]any) int {
	for _, field := range imageOverrideRowFields {
		if field == "line_number" {
			if delta := rowInt(a, field) - rowInt(b, field); delta != 0 {
				return delta
			}
			continue
		}
		if delta := strings.Compare(rowString(a, field), rowString(b, field)); delta != 0 {
			return delta
		}
	}
	return 0
}

// sortImageOverrideRowsByContent sorts rows in place so their order is a
// pure function of ROW CONTENT (via compareImageOverrideRows) rather than of
// however they arrived from their producer -- specifically, Go's
// `for key, item := range typed` map walk in collectHelmImageOverrides,
// whose iteration order Go deliberately randomizes per call (issue #5440
// review: 300 parses of the byte-identical Helm input produced 5 different
// row orderings before this fix).
//
// This matters even though the caller (language.go) re-sorts the whole
// bucket afterward via shared.SortNamedBucket, which only compares
// (line_number, name): Helm rows hardcode line_number to 1, so two rows
// declaring the SAME repository under two different tags tie on both of
// shared.SortNamedBucket's keys. slices.SortFunc (shared.SortNamedMaps) is
// documented as NOT stable, so it does not resolve that tie itself -- Go's
// sort algorithms are deterministic FUNCTIONS OF THEIR INPUT, not sources of
// randomness on their own, so an unstable sort still produces a repeatable
// output for a repeatable input. The randomness was never in the sort; it
// was in the un-sorted map-walk order feeding it. Content-sorting here first
// removes that randomness before shared.SortNamedBucket ever runs, so the
// same file parsed any number of times always resolves the tie the same
// way.
func sortImageOverrideRowsByContent(rows []map[string]any) {
	slices.SortFunc(rows, compareImageOverrideRows)
}

// dedupeImageOverrideRows removes exact-duplicate image_overrides rows --
// identical on every field -- while preserving the first occurrence's
// position, and is applied by both producers above (issue #5440 review).
//
// A row that is byte-for-byte identical to another carries zero
// distinguishing information: image_overrides has no "declared under"
// field, so two Helm "image:" blocks (or two Kustomize images[] entries)
// naming the same repository with the same tag/digest/environment produce
// two rows a consumer cannot tell apart. Shipping both would be pure phantom
// noise, so they collapse to one -- mirroring
// helm_values[].image_repositories, which already deduplicates
// (deduplicateStrings, helm.go). A row that differs in ANY field (the same
// repository declared under a different tag, for example) is a genuinely
// distinct declaration and is kept.
//
// This scans deduped-so-far linearly for each row rather than hashing into a
// map (issue #5440 review, second pass). The first version built a
// fmt.Sprintf string key (reflection-based, allocates every call); the
// second version replaced it with a flat 9-field comparable struct key --
// but that struct measured 136 bytes, over the Go runtime's 128-byte
// map-key threshold for in-bucket storage, so the runtime silently fell
// back to indirect (pointer-boxed) key storage and allocs/op did not
// improve at all (measured: identical allocs/op to the Sprintf version).
// Bucketing on a small map key (for example just name+repository) and
// falling back to a per-bucket scan for the remaining fields would work
// around the threshold, but adds a map plus a growing per-bucket slice
// (itself an allocation per distinct key) for no real gain at this scale.
// A plain O(n^2) scan against the small, pre-sized deduped slice needs
// exactly ONE allocation total (deduped's own backing array, sized len(rows)
// up front) regardless of row count. image_overrides rows are scoped to one
// file's declared images -- realistically single-to-low-double-digits, never
// the file-count or repo-count scale this repo's performance contract
// actually governs -- so trading an unmeasurable quadratic constant for
// zero per-row allocations is the right tradeoff here. See the package
// README's Performance Evidence for the measured numbers backing this
// choice, including a larger (200-image) fixture confirming no superlinear
// blowup.
func dedupeImageOverrideRows(rows []map[string]any) []map[string]any {
	if len(rows) < 2 {
		return rows
	}
	deduped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		duplicate := false
		for _, existing := range deduped {
			if imageOverrideRowsEqual(existing, row) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			deduped = append(deduped, row)
		}
	}
	return deduped
}

// rowString and rowInt read one image_overrides row field as its known
// concrete type (helmImageOverrideRow and collectKustomizeImageOverrides
// always store a string, or for line_number an int). The comma-ok form is
// defensive, not load-bearing: a type mismatch here would be a bug in this
// file's own row builders, never a real runtime input, so it degrades to the
// zero value rather than panicking on a value this package fully controls.
func rowString(row map[string]any, key string) string {
	s, _ := row[key].(string)
	return s
}

func rowInt(row map[string]any, key string) int {
	n, _ := row[key].(int)
	return n
}
