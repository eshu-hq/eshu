// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
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
	return rows
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
	return rows
}

// helmValuesEnvironment infers a Helm values override file's environment
// from its filename -- "values-prod.yaml" or "values.prod.yaml" -> "prod" --
// returning "" for the base values.yaml/values.yml and for any name it
// cannot confidently parse. This is a deliberately narrow, filename-only
// inference: it does not scan arbitrary path segments for environment-like
// keywords. Issue #5444 owns broader environment detection; this stays the
// conservative #5440 subset.
func helmValuesEnvironment(filename string) string {
	lower := strings.ToLower(filename)
	ext := filepath.Ext(lower)
	if ext != ".yaml" && ext != ".yml" {
		return ""
	}
	base := strings.TrimSuffix(lower, ext)
	for _, sep := range []string{"values-", "values."} {
		if strings.HasPrefix(base, sep) {
			if env := strings.TrimPrefix(base, sep); env != "" {
				return env
			}
		}
	}
	return ""
}

// helmImageOverrideEnvironment resolves the environment for a Helm values
// file: the existing ".../environments/<env>/..." path-segment signal
// (environmentFromPath, observability_helpers.go) takes priority as the
// stronger, already-trusted source of truth, falling back to the
// values-<env>.yaml/values.<env>.yaml filename inference above.
func helmImageOverrideEnvironment(path string) string {
	if env := environmentFromPath(path); env != "" {
		return env
	}
	return helmValuesEnvironment(filepath.Base(path))
}
