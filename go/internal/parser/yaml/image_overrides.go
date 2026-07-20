// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"path/filepath"
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

// collectHelmImageOverrides walks a Helm values document for maps nested
// under an "image" parent key, mirroring collectHelmImageRepositories's walk
// exactly (helm.go), and emits one image_overrides row per declared image.
// Row order is caller-visible only after the caller sorts the appended
// bucket (shared.SortNamedBucket) -- Go map iteration order is
// nondeterministic, so this function's own return order is not stable
// across calls.
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
// a kustomization document's images[] list, capturing the newTag/digest
// fields collectKustomizeObjectRefs (kustomize_semantics.go) never reads. This
// function's own return preserves the source images[] list order, but -- like
// every parser payload bucket (see the package README's "Output ordering is
// part of the parser fact contract" invariant) -- the caller sorts the
// appended bucket (shared.SortNamedBucket, by line_number then name) before
// Parse() returns, so the final image_overrides rows are NOT in declaration
// order; do not rely on list-declaration order downstream.
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
		repository := cleanString(object["newName"])
		if repository == "" {
			repository = name
		}
		rows = append(rows, map[string]any{
			"name":        name,
			"repository":  repository,
			"tag":         cleanString(object["newTag"]),
			"digest":      cleanString(object["digest"]),
			"environment": environment,
			"source":      "kustomize",
			"path":        path,
			"line_number": lineNumber,
			"lang":        "yaml",
		})
	}
	return dedupeImageOverrideRows(rows)
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
func dedupeImageOverrideRows(rows []map[string]any) []map[string]any {
	if len(rows) < 2 {
		return rows
	}
	seen := make(map[string]struct{}, len(rows))
	deduped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		key := fmt.Sprintf(
			"%v\x00%v\x00%v\x00%v\x00%v\x00%v\x00%v\x00%v\x00%v",
			row["name"], row["repository"], row["tag"], row["digest"],
			row["environment"], row["source"], row["path"],
			row["line_number"], row["lang"],
		)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, row)
	}
	return deduped
}

// helmImageOverrideEnvironmentTokens is the closed set of filename suffixes
// helmValuesEnvironment accepts as a real deployment environment. It mirrors
// isDeploymentEnvironmentToken
// (go/internal/query/repository_deployment_evidence_read_model.go:331-338)
// so the two "which words mean environment" answers agree.
//
// The token set is deliberately duplicated here rather than imported: the
// query package imports internal/parser, so importing query from this
// package would be an import cycle. This duplication is accepted for #5440;
// issue #5444 owns hoisting both call sites onto one shared implementation.
// Keep any edit to the query-side list mirrored here by hand until then.
var helmImageOverrideEnvironmentTokens = map[string]struct{}{
	"dev":         {},
	"development": {},
	"test":        {},
	"qa":          {},
	"stage":       {},
	"staging":     {},
	"uat":         {},
	"preprod":     {},
	"prod":        {},
	"production":  {},
	"sandbox":     {},
	"preview":     {},
}

// helmValuesEnvironment infers a Helm values override file's environment
// from its filename -- "values-prod.yaml" or "values.prod.yaml" -> "prod" --
// returning "" for the base values.yaml/values.yml and for any name it
// cannot confidently parse.
//
// Accuracy guardrail (#5440 P1): a bare "values-<X>.yaml"/"values.<X>.yaml"
// split is not enough -- "values.schema.yaml" (a values-schema convention),
// "values.example.yaml" (documentation), and "values.template.yaml"
// (scaffolding) all match that shape without being an environment. The
// parsed suffix is therefore accepted ONLY when it is a member of
// helmImageOverrideEnvironmentTokens; an unrecognized suffix returns "",
// never a guess. This is a deliberately narrow, filename-only inference on
// top of that gate: it does not scan arbitrary path segments for
// environment-like keywords. Issue #5444 owns broader environment detection;
// this stays the conservative #5440 subset.
func helmValuesEnvironment(filename string) string {
	lower := strings.ToLower(filename)
	ext := filepath.Ext(lower)
	if ext != ".yaml" && ext != ".yml" {
		return ""
	}
	base := strings.TrimSuffix(lower, ext)
	for _, sep := range []string{"values-", "values."} {
		env, cut := strings.CutPrefix(base, sep)
		if !cut || env == "" {
			continue
		}
		if _, known := helmImageOverrideEnvironmentTokens[env]; known {
			return env
		}
	}
	return ""
}

// helmImageOverrideEnvironment resolves the environment for a Helm values
// file. The two signals are deliberately asymmetric:
//
//   - The ".../environments/<env>/..." path segment (environmentFromPath,
//     observability_helpers.go) is an explicit AUTHOR DECLARATION -- someone
//     chose to lay the repo out with an "environments" directory naming this
//     environment -- so it takes priority and stays UNGATED: it returns
//     whatever the author wrote, even a name outside
//     helmImageOverrideEnvironmentTokens.
//   - The values-<env>.yaml/values.<env>.yaml filename fallback is an
//     INFERENCE from a filename convention that also matches non-environment
//     files (values.schema.yaml, values.example.yaml), so it is GATED by the
//     token allowlist above and answers "" rather than guessing.
func helmImageOverrideEnvironment(path string) string {
	if env := environmentFromPath(path); env != "" {
		return env
	}
	return helmValuesEnvironment(filepath.Base(path))
}
