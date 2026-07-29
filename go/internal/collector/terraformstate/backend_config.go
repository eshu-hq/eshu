// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	// BackendWarningKindUnresolvedExpression marks backend config that could not
	// be reduced to an exact Terraform-state locator.
	BackendWarningKindUnresolvedExpression = "unresolved_backend_expression"

	// BackendWarningSourceTerraformBackend identifies parser-side Terraform
	// backend evidence.
	BackendWarningSourceTerraformBackend = "terraform_backend"
)

const (
	backendWarningReasonMissingVariableDefault   = "missing_variable_default"
	backendWarningReasonAmbiguousVariableDefault = "ambiguous_variable_default"
	backendWarningReasonMissingLocalValue        = "missing_local_value"
	backendWarningReasonAmbiguousLocalValue      = "ambiguous_local_value"
	backendWarningReasonCyclicLocalValue         = "cyclic_local_value"
	backendWarningReasonUnsupportedReference     = "unsupported_reference"
	backendWarningReasonUnresolvedInterpolation  = "unresolved_interpolation"
	backendWarningReasonWorkspaceInterpolation   = "workspace_interpolation"
	backendWarningReasonFunctionCall             = "function_call"
	backendWarningReasonWorkspaceKeyPrefix       = "workspace_key_prefix"
	backendWarningReasonNonExactValue            = "non_exact_value"
)

const backendNotCandidateReason = "backend attribute did not resolve to an exact locator"

// BackendConfigContext carries parser-emitted Terraform backend evidence from
// one active repository generation.
type BackendConfigContext struct {
	Backends  []map[string]any
	Variables []map[string]any
	Locals    []map[string]any
	// RepoLocalPath is the absolute filesystem root the repository checkout
	// used to produce Backends/Variables/Locals was cloned into, mirrored
	// from the durable `repository` fact's `local_path` payload field for the
	// same active generation (see
	// go/internal/storage/postgres/tfstate_backend_canonical.go). A
	// BackendLocal candidate's locator must be an absolute path that
	// byte-for-byte matches the absolute path
	// terraformstate.LocalStateSource used when it actually opened the same
	// on-disk state file (scope.NewTerraformStateSnapshotScope hashes that
	// absolute path) — RepoLocalPath is the prefix that makes the two agree
	// (issue #5594). Callers that only need Warnings (for example the git
	// collector's EvaluateBackendConfig(...).Warnings call, which discards
	// Candidates) may leave this blank; a blank value makes every
	// BackendLocal candidate resolve to false rather than guess a locator.
	RepoLocalPath string
}

// BackendConfigResult is the shared decision output for Git-observed Terraform
// backend config.
type BackendConfigResult struct {
	Candidates []DiscoveryCandidate
	Warnings   []BackendExpressionWarning
}

// BackendExpressionWarning describes one unresolved backend attribute without
// carrying the raw expression value.
type BackendExpressionWarning struct {
	RepoID             string
	BackendKind        string
	AttributeName      string
	Reason             string
	ExpressionKind     string
	ConfidenceTier     string
	NotCandidateReason string
	SourcePath         string
	LineNumber         int
	ExpressionHash     string
}

// EvaluateBackendConfig returns exact state candidates plus source-backed
// warnings for backend attributes that could not become exact candidates.
func EvaluateBackendConfig(repoID string, contextValue BackendConfigContext) BackendConfigResult {
	result := BackendConfigResult{
		Candidates: make([]DiscoveryCandidate, 0, len(contextValue.Backends)),
	}
	for _, backend := range contextValue.Backends {
		resolution := newBackendResolutionContext(contextValue, backendStringValue(backend, "path"))
		if candidate, ok := backendConfigCandidate(repoID, backend, resolution, contextValue.RepoLocalPath); ok {
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		result.Warnings = append(result.Warnings, backendExpressionWarnings(repoID, backend, resolution)...)
	}
	return result
}

// backendConfigCandidate dispatches to the per-backend-kind candidate
// derivation. Every Terraform backend kind Eshu does not model here (gcs,
// azurerm, remote, http, ...) falls through to the default case: no candidate
// and no warning, the same silent-zero behavior BackendLocal had before issue
// #5594.
func backendConfigCandidate(
	repoID string,
	backend map[string]any,
	resolution backendResolutionContext,
	repoLocalPath string,
) (DiscoveryCandidate, bool) {
	switch strings.TrimSpace(backendStringValue(backend, "backend_kind", "name")) {
	case string(BackendS3):
		return backendConfigS3Candidate(repoID, backend, resolution)
	case string(BackendLocal):
		return backendConfigLocalCandidate(repoID, backend, resolution, repoLocalPath)
	default:
		return DiscoveryCandidate{}, false
	}
}

func backendConfigS3Candidate(
	repoID string,
	backend map[string]any,
	resolution backendResolutionContext,
) (DiscoveryCandidate, bool) {
	if strings.TrimSpace(backendStringValue(backend, "workspace_key_prefix")) != "" {
		return DiscoveryCandidate{}, false
	}

	dynamoDBTable := resolveOptionalBackendConfigAttribute(backend, "dynamodb_table", resolution)
	resolvedBucket, bucketOK := resolveBackendConfigAttribute(backend, "bucket", resolution)
	resolvedKey, keyOK := resolveBackendConfigAttribute(backend, "key", resolution)
	resolvedRegion, regionOK := resolveBackendConfigAttribute(backend, "region", resolution)
	if !bucketOK || !keyOK || !regionOK {
		return DiscoveryCandidate{}, false
	}
	if strings.HasSuffix(resolvedKey, "/") {
		return DiscoveryCandidate{}, false
	}

	return DiscoveryCandidate{
		State: StateKey{
			BackendKind: BackendS3,
			Locator:     "s3://" + resolvedBucket + "/" + resolvedKey,
		},
		Source:        DiscoveryCandidateSourceGraph,
		RepoID:        strings.TrimSpace(repoID),
		Region:        resolvedRegion,
		DynamoDBTable: dynamoDBTable,
	}, true
}

func backendExpressionWarnings(
	repoID string,
	backend map[string]any,
	resolution backendResolutionContext,
) []BackendExpressionWarning {
	switch strings.TrimSpace(backendStringValue(backend, "backend_kind", "name")) {
	case string(BackendS3):
		return backendConfigS3ExpressionWarnings(repoID, backend, resolution)
	case string(BackendLocal):
		return backendConfigLocalExpressionWarnings(repoID, backend, resolution)
	default:
		return nil
	}
}

func backendConfigS3ExpressionWarnings(
	repoID string,
	backend map[string]any,
	resolution backendResolutionContext,
) []BackendExpressionWarning {
	warnings := make([]BackendExpressionWarning, 0, 3)
	if strings.TrimSpace(backendStringValue(backend, "workspace_key_prefix")) != "" {
		warnings = append(warnings, backendExpressionWarningForAttribute(
			repoID,
			backend,
			"workspace_key_prefix",
			backendAttributeDecision{
				ok:             false,
				reason:         backendWarningReasonWorkspaceKeyPrefix,
				expressionKind: backendExpressionKind(backendStringValue(backend, "workspace_key_prefix")),
			},
		))
	}
	for _, attributeName := range []string{"bucket", "key", "region"} {
		value := strings.TrimSpace(backendStringValue(backend, attributeName))
		if value == "" {
			continue
		}
		decision := resolveBackendConfigAttributeDecision(backend, attributeName, resolution)
		if decision.ok {
			continue
		}
		warnings = append(warnings, backendExpressionWarningForAttribute(repoID, backend, attributeName, decision))
	}
	return warnings
}

func backendExpressionWarningForAttribute(
	repoID string,
	backend map[string]any,
	attributeName string,
	decision backendAttributeDecision,
) BackendExpressionWarning {
	return backendExpressionWarningForRowAttribute(repoID, backend, attributeName, attributeName, decision)
}

// backendExpressionWarningForRowAttribute builds the warning for an attribute
// whose Terraform HCL name (attributeName, used for the reported
// attribute_name field operators recognize) differs from the parser row key
// it is stored under (rowKey). The two differ only for the local backend's
// "path" attribute, which the parser stores as row["state_path"] to avoid
// colliding with the row's own "path" field (the source .tf file path; see
// go/internal/parser/hcl/terraform_backend.go).
func backendExpressionWarningForRowAttribute(
	repoID string,
	backend map[string]any,
	attributeName string,
	rowKey string,
	decision backendAttributeDecision,
) BackendExpressionWarning {
	value := strings.TrimSpace(backendStringValue(backend, rowKey))
	return BackendExpressionWarning{
		RepoID:             strings.TrimSpace(repoID),
		BackendKind:        strings.TrimSpace(backendStringValue(backend, "backend_kind", "name")),
		AttributeName:      attributeName,
		Reason:             decision.reason,
		ExpressionKind:     decision.expressionKind,
		ConfidenceTier:     "name_only",
		NotCandidateReason: backendNotCandidateReason,
		SourcePath:         cleanBackendConfigRelativePath(backendStringValue(backend, "path")),
		LineNumber:         backendIntValue(backend, rowKey+"_line_number"),
		ExpressionHash: facts.StableID("TerraformBackendExpression", map[string]any{
			"expression": value,
		}),
	}
}
