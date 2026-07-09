// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func terraformStateBackendExpressionWarningFactCount(repoID string, fileData []map[string]any) int {
	return len(terraformStateBackendExpressionWarnings(repoID, fileData))
}

func emitTerraformStateBackendExpressionWarnings(
	w factStreamWriter,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	fileData []map[string]any,
) {
	for _, warning := range terraformStateBackendExpressionWarnings(repoID, fileData) {
		w.send(terraformStateBackendExpressionWarningEnvelope(scopeID, generationID, observedAt, warning))
	}
}

func terraformStateBackendExpressionWarnings(
	repoID string,
	fileData []map[string]any,
) []terraformstate.BackendExpressionWarning {
	contextValue := terraformStateBackendConfigContext(fileData)
	if len(contextValue.Backends) == 0 {
		return nil
	}
	return terraformstate.EvaluateBackendConfig(repoID, contextValue).Warnings
}

func terraformStateBackendConfigContext(fileData []map[string]any) terraformstate.BackendConfigContext {
	contextValue := terraformstate.BackendConfigContext{}
	for _, file := range fileData {
		contextValue.Backends = append(contextValue.Backends, terraformBackendConfigRows(file["terraform_backends"])...)
		contextValue.Variables = append(contextValue.Variables, terraformBackendConfigRows(file["terraform_variables"])...)
		contextValue.Locals = append(contextValue.Locals, terraformBackendConfigRows(file["terraform_locals"])...)
	}
	return contextValue
}

func terraformBackendConfigRows(value any) []map[string]any {
	switch rows := value.(type) {
	case []map[string]any:
		return rows
	case []any:
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			if item, ok := row.(map[string]any); ok {
				out = append(out, item)
			}
		}
		return out
	default:
		return nil
	}
}

func terraformStateBackendExpressionWarningEnvelope(
	scopeID string,
	generationID string,
	observedAt time.Time,
	warning terraformstate.BackendExpressionWarning,
) facts.Envelope {
	payload := map[string]any{
		"warning_kind":         terraformstate.BackendWarningKindUnresolvedExpression,
		"reason":               strings.TrimSpace(warning.Reason),
		"source":               terraformstate.BackendWarningSourceTerraformBackend,
		"repo_id":              strings.TrimSpace(warning.RepoID),
		"backend_kind":         strings.TrimSpace(warning.BackendKind),
		"attribute_name":       strings.TrimSpace(warning.AttributeName),
		"expression_kind":      strings.TrimSpace(warning.ExpressionKind),
		"confidence_tier":      strings.TrimSpace(warning.ConfidenceTier),
		"not_candidate_reason": strings.TrimSpace(warning.NotCandidateReason),
		"source_path":          strings.TrimSpace(warning.SourcePath),
		"line_number":          warning.LineNumber,
		"expression_hash":      strings.TrimSpace(warning.ExpressionHash),
	}
	if classification, ok := terraformstate.ClassifyWarning(
		terraformstate.BackendWarningKindUnresolvedExpression,
		warning.Reason,
	); ok {
		payload["severity"] = classification.Severity
		payload["actionability"] = classification.Actionability
	}

	factKey := strings.Join([]string{
		"terraform_state_warning",
		"backend_expression",
		strings.TrimSpace(warning.RepoID),
		strings.TrimSpace(warning.SourcePath),
		strings.TrimSpace(warning.BackendKind),
		strings.TrimSpace(warning.AttributeName),
		strings.TrimSpace(warning.Reason),
		strings.TrimSpace(warning.ExpressionHash),
		strconv.Itoa(warning.LineNumber),
	}, ":")
	sourceURI := "git://" + strings.TrimSpace(warning.RepoID)
	if sourcePath := strings.TrimSpace(warning.SourcePath); sourcePath != "" {
		sourceURI += "/" + sourcePath
	}
	return facts.Envelope{
		FactID: facts.StableID(
			"GoGitCollectorFact",
			map[string]any{
				"fact_key":      factKey,
				"fact_kind":     facts.TerraformStateWarningFactKind,
				"generation_id": generationID,
				"scope_id":      scopeID,
			},
		),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.TerraformStateWarningFactKind,
		StableFactKey:    factKey,
		SchemaVersion:    facts.TerraformStateWarningSchemaVersion,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        factKey,
			SourceURI:      sourceURI,
			SourceRecordID: factKey,
		},
	}
}
