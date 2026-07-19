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

// redactPackageRegistryIdentityIssueMetadata blanks every field on issue that
// could describe a private/unknown package's registry metadata, in place.
// Ecosystem and NormalizedName are left untouched: the scoped name+ecosystem
// branch's caller already supplied both as request query parameters, so
// echoing them back is not a new disclosure. Every other field could belong
// to a package the caller has no grant to see and this row's missing
// package_id makes its grant status unverifiable, so this fails closed on
// all of them rather than the source_path field alone.
func redactPackageRegistryIdentityIssueMetadata(issue *PackageRegistryIdentityIssue) {
	issue.Registry = ""
	issue.Namespace = ""
	issue.PURL = ""
	issue.BOMRef = ""
	issue.PackageManager = ""
	issue.SourcePath = ""
	issue.SourceSpecificID = ""
	issue.Visibility = ""
	issue.SourceConfidence = ""
	issue.VersionCount = 0
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
