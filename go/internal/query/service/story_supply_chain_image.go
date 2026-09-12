// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file hosts the pure supply-chain image-reference helpers behind the
// service story (Issue #6060, lane B B4). They moved here from the query
// root (service_story_supply_chain.go), whose *EntityHandler evidence
// method must stay in package query: Go requires methods to live with their
// receiver type. The staying enricher and the service-story seam keep
// working through the exported homes. Bodies are unchanged modulo package
// qualifiers and the export renames below.

// StorySupplyChainImagePackage returns the image package the supply
// chain enricher attached to the workload context, if any. Pinned by the
// staying service-story seam (service_story_seam.go) and the code-to-runtime
// trace path in this package.
func StorySupplyChainImagePackage(workloadContext map[string]any) map[string]any {
	return querycontract.MapValue(querycontract.MapValue(workloadContext, "supply_chain_evidence"), "image_package")
}

// StoryMatchedImageRef extracts a container image reference from a
// deployment-evidence row when the row's kind marks it as image-shaped.
// Pinned by the staying deployment-image collector
// (service_story_supply_chain.go) and the code-to-runtime trace path in
// this package.
func StoryMatchedImageRef(row map[string]any) string {
	value := strings.TrimSpace(querycontract.StringVal(row, "matched_value"))
	if value == "" {
		return ""
	}
	kind := strings.ToLower(strings.Join([]string{
		querycontract.StringVal(row, "evidence_kind"),
		querycontract.StringVal(row, "artifact_family"),
		querycontract.StringVal(row, "extractor"),
	}, " "))
	if strings.Contains(kind, "image") || strings.Contains(kind, "oci") {
		return value
	}
	if ref := serviceStoryExplicitImageRef(value); ref != "" {
		return ref
	}
	if strings.Contains(kind, "helm") {
		return serviceStoryRegistryImageRepository(value)
	}
	return ""
}

func serviceStoryExplicitImageRef(raw string) string {
	ref := doctruth.NormalizeContainerImageRefClaim(raw)
	if ref == "" {
		return ""
	}
	repository := ref
	if digestIndex := strings.Index(repository, "@sha256:"); digestIndex >= 0 {
		repository = repository[:digestIndex]
	} else if tagIndex := strings.LastIndex(repository, ":"); tagIndex >= 0 {
		repository = repository[:tagIndex]
	}
	if !strings.Contains(repository, "/") && !strings.Contains(repository, ".") {
		return ""
	}
	return ref
}

func serviceStoryRegistryImageRepository(raw string) string {
	repository := strings.Trim(strings.TrimSpace(raw), `"'`)
	if repository == "" ||
		strings.ContainsAny(repository, " \t\n\r${}") ||
		strings.Contains(repository, "://") ||
		strings.Contains(repository, "@") {
		return ""
	}
	if tagIndex := strings.LastIndex(repository, ":"); tagIndex >= 0 {
		if tagIndex > strings.LastIndex(repository, "/") {
			return ""
		}
	}
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return ""
	}
	registry := parts[0]
	if !strings.Contains(registry, ".") && !strings.Contains(registry, ":") && !strings.EqualFold(registry, "localhost") {
		return ""
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".yaml") || strings.HasSuffix(part, ".yml") {
			return ""
		}
	}
	return repository
}
