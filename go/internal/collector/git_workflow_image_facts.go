// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflowimage"
)

func emitWorkflowImageEvidenceFactsForContentFile(
	w factStreamWriter,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	commitSHA string,
	body string,
) {
	if !isGitHubActionsWorkflowPath(relativePath) {
		return
	}
	for _, evidence := range workflowimage.ExtractGitHubActions(relativePath, body) {
		w.send(workflowImageEvidenceFactEnvelope(repoID, scopeID, generationID, observedAt, commitSHA, evidence))
	}
}

func workflowImageEvidenceFactEnvelope(
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	commitSHA string,
	evidence workflowimage.Evidence,
) facts.Envelope {
	payload := map[string]any{
		"repository_id":   repoID,
		"workflow_path":   evidence.WorkflowPath,
		"command_kind":    evidence.CommandKind,
		"evidence_class":  evidence.EvidenceClass,
		"source_category": "static_workflow",
	}
	if commitSHA != "" {
		payload["commit_sha"] = commitSHA
	}
	if evidence.JobName != "" {
		payload["job_name"] = evidence.JobName
	}
	if evidence.StepName != "" {
		payload["step_name"] = evidence.StepName
	}
	if evidence.ImageRef != "" {
		payload["image_ref"] = evidence.ImageRef
	}
	if len(evidence.ImageRefs) > 0 {
		payload["image_refs"] = append([]string(nil), evidence.ImageRefs...)
	}
	if evidence.Reason != "" {
		payload["reason"] = evidence.Reason
	}
	stableKey := facts.StableID(facts.CICDWorkflowImageEvidenceFactKind, map[string]any{
		"repository_id":  repoID,
		"workflow_path":  evidence.WorkflowPath,
		"job_name":       evidence.JobName,
		"step_name":      evidence.StepName,
		"command_kind":   evidence.CommandKind,
		"image_ref":      evidence.ImageRef,
		"image_refs":     evidence.ImageRefs,
		"evidence_class": evidence.EvidenceClass,
		"reason":         evidence.Reason,
	})
	envelope := factEnvelope(
		facts.CICDWorkflowImageEvidenceFactKind,
		scopeID,
		generationID,
		observedAt,
		stableKey,
		payload,
		evidence.WorkflowPath,
	)
	envelope.SchemaVersion = facts.CICDSchemaVersion
	return envelope
}

func isGitHubActionsWorkflowPath(relativePath string) bool {
	lower := strings.ToLower(filepath.ToSlash(relativePath))
	return (path.Ext(lower) == ".yml" || path.Ext(lower) == ".yaml") &&
		path.Dir(lower) == ".github/workflows"
}
