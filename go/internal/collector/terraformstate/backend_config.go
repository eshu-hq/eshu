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
		if candidate, ok := backendConfigCandidate(repoID, backend, resolution); ok {
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		result.Warnings = append(result.Warnings, backendExpressionWarnings(repoID, backend, resolution)...)
	}
	return result
}

func backendConfigCandidate(
	repoID string,
	backend map[string]any,
	resolution backendResolutionContext,
) (DiscoveryCandidate, bool) {
	if strings.TrimSpace(backendStringValue(backend, "backend_kind", "name")) != string(BackendS3) {
		return DiscoveryCandidate{}, false
	}
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
	backendKind := strings.TrimSpace(backendStringValue(backend, "backend_kind", "name"))
	if backendKind != string(BackendS3) {
		return nil
	}

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
	value := strings.TrimSpace(backendStringValue(backend, attributeName))
	return BackendExpressionWarning{
		RepoID:             strings.TrimSpace(repoID),
		BackendKind:        strings.TrimSpace(backendStringValue(backend, "backend_kind", "name")),
		AttributeName:      attributeName,
		Reason:             decision.reason,
		ExpressionKind:     decision.expressionKind,
		ConfidenceTier:     "name_only",
		NotCandidateReason: backendNotCandidateReason,
		SourcePath:         cleanBackendConfigRelativePath(backendStringValue(backend, "path")),
		LineNumber:         backendIntValue(backend, attributeName+"_line_number"),
		ExpressionHash: facts.StableID("TerraformBackendExpression", map[string]any{
			"expression": value,
		}),
	}
}
