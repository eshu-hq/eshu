// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import "strings"

// unknownLabel is the bounded fallback used when a normalized telemetry-safe
// value cannot be derived. It keeps metric labels low cardinality and avoids
// leaking raw provider input.
const unknownLabel = "unknown"

// globalLocation is the normalized location bucket for resources with no
// provider-reported region or zone.
const globalLocation = "global"

// Ancestry is the normalized Cloud Asset Inventory ancestor chain for one
// resource. ProjectNumber, FolderNumbers, and OrganizationNumber are extracted
// from the raw chain for reducer joins; Chain preserves the verbatim ordered
// ancestor list as source evidence.
type Ancestry struct {
	// ProjectNumber is the project ancestor identifier, empty when absent.
	ProjectNumber string
	// FolderNumbers lists folder ancestor identifiers from most to least specific.
	FolderNumbers []string
	// OrganizationNumber is the organization ancestor identifier, empty when
	// absent.
	OrganizationNumber string
	// Chain is the verbatim ordered CAI ancestor chain.
	Chain []string
}

// AssetTypeFamily derives the bounded asset family from a Cloud Asset Inventory
// asset type such as "compute.googleapis.com/Instance". It returns the service
// segment ("compute") which is safe as a telemetry label, or "unknown" when the
// asset type is blank or malformed.
func AssetTypeFamily(assetType string) string {
	trimmed := strings.TrimSpace(assetType)
	if trimmed == "" {
		return unknownLabel
	}
	host, _, ok := strings.Cut(trimmed, "/")
	if !ok || host == "" {
		return unknownLabel
	}
	service, _, found := strings.Cut(host, ".")
	if !found || service == "" {
		return unknownLabel
	}
	return strings.ToLower(service)
}

// LocationBucket normalizes a provider location into a bounded, lower-cased
// location bucket. A blank location normalizes to "global".
func LocationBucket(location string) string {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return globalLocation
	}
	return strings.ToLower(trimmed)
}

// NormalizeAncestry splits a Cloud Asset Inventory ancestor chain into project,
// folder, and organization identifiers while preserving the verbatim chain. The
// chain entries are of the form "projects/<id>", "folders/<id>", and
// "organizations/<id>".
func NormalizeAncestry(ancestors []string) Ancestry {
	result := Ancestry{Chain: cloneStrings(ancestors)}
	for _, raw := range ancestors {
		kind, id, ok := strings.Cut(strings.TrimSpace(raw), "/")
		if !ok || id == "" {
			continue
		}
		switch kind {
		case "projects":
			if result.ProjectNumber == "" {
				result.ProjectNumber = id
			}
		case "folders":
			result.FolderNumbers = append(result.FolderNumbers, id)
		case "organizations":
			if result.OrganizationNumber == "" {
				result.OrganizationNumber = id
			}
		}
	}
	return result
}

// ProjectIDFromFullName extracts the project id segment from a Cloud Asset
// Inventory full resource name such as
// "//compute.googleapis.com/projects/my-project/zones/.../instances/vm-1". It
// returns an empty string when the full resource name carries no projects
// segment.
func ProjectIDFromFullName(fullName string) string {
	trimmed := strings.TrimSpace(fullName)
	if trimmed == "" {
		return ""
	}
	segments := strings.Split(trimmed, "/")
	for i := 0; i < len(segments)-1; i++ {
		if segments[i] == "projects" {
			return segments[i+1]
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, len(input))
	copy(out, input)
	return out
}
