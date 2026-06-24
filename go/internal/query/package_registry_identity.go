// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

const packageRegistryPackageIDMissingReason = "package_id_missing"

// PackageRegistryIdentityIssue describes a package-registry graph row that
// could not be returned as a valid package identity.
type PackageRegistryIdentityIssue struct {
	Reason           string   `json:"reason"`
	MissingEvidence  []string `json:"missing_evidence"`
	Ecosystem        string   `json:"ecosystem,omitempty"`
	Registry         string   `json:"registry,omitempty"`
	Namespace        string   `json:"namespace,omitempty"`
	NormalizedName   string   `json:"normalized_name,omitempty"`
	PURL             string   `json:"purl,omitempty"`
	BOMRef           string   `json:"bom_ref,omitempty"`
	PackageManager   string   `json:"package_manager,omitempty"`
	SourcePath       string   `json:"source_path,omitempty"`
	SourceSpecificID string   `json:"source_specific_id,omitempty"`
	Visibility       string   `json:"visibility,omitempty"`
	SourceConfidence string   `json:"source_confidence,omitempty"`
	VersionCount     int      `json:"version_count"`
}

func packageRegistryPackageResultFromRow(
	row map[string]any,
) (PackageRegistryPackageResult, *PackageRegistryIdentityIssue) {
	packageID := strings.TrimSpace(StringVal(row, "package_id"))
	if packageID == "" {
		issue := packageRegistryIdentityIssueFromRow(row, packageRegistryPackageIDMissingReason)
		return PackageRegistryPackageResult{}, &issue
	}
	return PackageRegistryPackageResult{
		PackageID:        packageID,
		Ecosystem:        StringVal(row, "ecosystem"),
		Registry:         StringVal(row, "registry"),
		Namespace:        StringVal(row, "namespace"),
		NormalizedName:   StringVal(row, "normalized_name"),
		PURL:             StringVal(row, "purl"),
		BOMRef:           StringVal(row, "bom_ref"),
		PackageManager:   StringVal(row, "package_manager"),
		SourcePath:       StringVal(row, "source_path"),
		SourceSpecificID: StringVal(row, "source_specific_id"),
		Visibility:       StringVal(row, "visibility"),
		SourceConfidence: StringVal(row, "source_confidence"),
		VersionCount:     IntVal(row, "version_count"),
	}, nil
}

func packageRegistryIdentityIssueFromRow(row map[string]any, reason string) PackageRegistryIdentityIssue {
	return PackageRegistryIdentityIssue{
		Reason:           reason,
		MissingEvidence:  []string{"package_id"},
		Ecosystem:        StringVal(row, "ecosystem"),
		Registry:         StringVal(row, "registry"),
		Namespace:        StringVal(row, "namespace"),
		NormalizedName:   StringVal(row, "normalized_name"),
		PURL:             StringVal(row, "purl"),
		BOMRef:           StringVal(row, "bom_ref"),
		PackageManager:   StringVal(row, "package_manager"),
		SourcePath:       StringVal(row, "source_path"),
		SourceSpecificID: StringVal(row, "source_specific_id"),
		Visibility:       StringVal(row, "visibility"),
		SourceConfidence: StringVal(row, "source_confidence"),
		VersionCount:     IntVal(row, "version_count"),
	}
}
