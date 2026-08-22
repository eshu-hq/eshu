// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflowimage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitmodel"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflowimage"
)

// currentWorkflowImageFileMetas returns body-free metadata for current workflow
// files outside the ordinary delta targets. The full discovered set is already
// path-sorted, so the returned metadata keeps deterministic order.
func CurrentWorkflowImageFileMetas(
	repoPath string,
	narrowed []discovery.FileWithSize,
	full []discovery.FileWithSize,
) []gitmodel.ContentFileMeta {
	narrowedPaths := make(map[string]struct{}, len(narrowed))
	for _, file := range narrowed {
		narrowedPaths[filepath.Clean(file.Path)] = struct{}{}
	}
	metas := make([]gitmodel.ContentFileMeta, 0)
	for _, file := range full {
		relativePath, err := filepath.Rel(repoPath, file.Path)
		if err != nil || !isGitHubActionsWorkflowPath(relativePath) {
			continue
		}
		if _, targeted := narrowedPaths[filepath.Clean(file.Path)]; targeted {
			continue
		}
		digest, ok := workflowImageDigestForFile(file.Path)
		if !ok {
			continue
		}
		metas = append(metas, gitmodel.ContentFileMeta{
			RelativePath: filepath.ToSlash(filepath.Clean(relativePath)),
			Digest:       digest,
			Language:     "yaml",
			ArtifactType: "github_actions_workflow",
		})
	}
	return metas
}

func workflowImageDigestForFile(filePath string) (string, bool) {
	body, err := os.ReadFile(filePath) // #nosec G304 -- reads an admitted workflow path from repository discovery
	if err != nil {
		return "", false
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), true
}

func EmitWorkflowImageEvidenceFactsForContentFile(
	w gitmodel.FactStreamWriter,
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
		w.Send(workflowImageEvidenceFactEnvelope(repoID, scopeID, generationID, observedAt, commitSHA, evidence))
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
	envelope := gitmodel.FactEnvelope(
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
