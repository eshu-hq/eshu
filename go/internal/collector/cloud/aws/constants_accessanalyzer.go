// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAccessAnalyzer identifies the regional IAM Access Analyzer
	// metadata-only scan slice.
	ServiceAccessAnalyzer = "accessanalyzer"
)

const (
	// ResourceTypeAccessAnalyzerAnalyzer identifies an IAM Access Analyzer
	// analyzer metadata resource.
	ResourceTypeAccessAnalyzerAnalyzer = "aws_accessanalyzer_analyzer"
	// ResourceTypeAccessAnalyzerArchiveRule identifies an Access Analyzer
	// archive-rule metadata resource.
	ResourceTypeAccessAnalyzerArchiveRule = "aws_accessanalyzer_archive_rule"
	// ResourceTypeAccessAnalyzerFindingCount identifies an aggregate Access
	// Analyzer finding-count resource.
	ResourceTypeAccessAnalyzerFindingCount = "aws_accessanalyzer_finding_count"
	// ResourceTypeAccessAnalyzerUnusedAccessSummary identifies a metadata-only
	// per-resource unused-access summary.
	ResourceTypeAccessAnalyzerUnusedAccessSummary = "aws_accessanalyzer_unused_access_summary"
)

const (
	// RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount records the
	// organization-account scope for an organization Access Analyzer analyzer.
	RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount = "accessanalyzer_analyzer_scopes_organization_account"
	// RelationshipAccessAnalyzerAnalyzerHasArchiveRule records archive-rule
	// membership on an Access Analyzer analyzer.
	RelationshipAccessAnalyzerAnalyzerHasArchiveRule = "accessanalyzer_analyzer_has_archive_rule"
)
