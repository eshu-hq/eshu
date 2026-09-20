// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitmodel"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// ContentFactEnvelope builds the durable content fact for one snapshot file:
// body, digest, and the language/artifact/template metadata downstream content
// materialization reads. The stable key is repo+path scoped so re-emission of a
// generation is idempotent.
func ContentFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	fileSnapshot gitmodel.ContentFileSnapshot,
) facts.Envelope {
	payload := map[string]any{
		"content_path":   fileSnapshot.RelativePath,
		"content_body":   fileSnapshot.Body,
		"content_digest": fileSnapshot.Digest,
		"repo_id":        repoID,
	}
	if fileSnapshot.Language != "" {
		payload["language"] = fileSnapshot.Language
	}
	if fileSnapshot.CommitSHA != "" {
		payload["commit_sha"] = fileSnapshot.CommitSHA
	}
	if fileSnapshot.ArtifactType != "" {
		payload["artifact_type"] = fileSnapshot.ArtifactType
	}
	if fileSnapshot.TemplateDialect != "" {
		payload["template_dialect"] = fileSnapshot.TemplateDialect
	}
	if fileSnapshot.IACRelevant != nil {
		payload["iac_relevant"] = strings.ToLower(fmt.Sprintf("%t", *fileSnapshot.IACRelevant))
	}

	return gitmodel.FactEnvelope(
		"content",
		scopeID,
		generationID,
		observedAt,
		"content:"+repoID+":"+fileSnapshot.RelativePath,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(fileSnapshot.RelativePath)),
	)
}

// cloneMetadata copies an entity metadata map so envelope construction never
// aliases the snapshot's live map.
func cloneMetadata(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

// ContentEntityFactEnvelope builds the durable content-entity fact for one parsed
// entity (function, class, etc.): its identity, location, language, and any
// extra parser metadata. The stable key is the entity uid so re-emission of a
// generation is idempotent.
func ContentEntityFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	entitySnapshot gitmodel.ContentEntitySnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_id":      entitySnapshot.EntityID,
		"graph_kind":    "content_entity",
		"entity_id":     entitySnapshot.EntityID,
		"repo_id":       repoID,
		"relative_path": entitySnapshot.RelativePath,
		"entity_type":   entitySnapshot.EntityType,
		"entity_name":   entitySnapshot.EntityName,
		"start_line":    entitySnapshot.StartLine,
		"end_line":      entitySnapshot.EndLine,
		"language":      entitySnapshot.Language,
		"source_cache":  entitySnapshot.SourceCache,
		"indexed_at":    entitySnapshot.IndexedAt.UTC().Format(time.RFC3339Nano),
	}
	if entitySnapshot.StartByte != nil {
		payload["start_byte"] = *entitySnapshot.StartByte
	}
	if entitySnapshot.EndByte != nil {
		payload["end_byte"] = *entitySnapshot.EndByte
	}
	if entitySnapshot.ArtifactType != "" {
		payload["artifact_type"] = entitySnapshot.ArtifactType
	}
	if entitySnapshot.TemplateDialect != "" {
		payload["template_dialect"] = entitySnapshot.TemplateDialect
	}
	if entitySnapshot.IACRelevant != nil {
		payload["iac_relevant"] = *entitySnapshot.IACRelevant
	}
	if len(entitySnapshot.Metadata) > 0 {
		payload["entity_metadata"] = cloneMetadata(entitySnapshot.Metadata)
	}

	return gitmodel.FactEnvelope(
		"content_entity",
		scopeID,
		generationID,
		observedAt,
		"content_entity:"+entitySnapshot.EntityID,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(entitySnapshot.RelativePath)),
	)
}

// RepositoryFactEnvelope builds the durable repository fact for one
// generation: identity, parsed-file count, import map, git refs, and delta
// metadata. The stable key is the repo ID so re-emission of a generation is
// idempotent.
//
// defaultBranch and gitRefsPayload arrive precomputed by the caller: this is a
// leaf package and must never import gitrepo (which owns the GitRef type), so
// ref selection and payload shaping stay on the caller side.
func RepositoryFactEnvelope(
	repoPath string,
	repo repositoryidentity.Metadata,
	sourceRunID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	parsedFileCount int,
	importsMap map[string][]string,
	isDependency bool,
	defaultBranch string,
	gitRefsPayload []map[string]any,
	delta bool,
	deltaRelativePaths []string,
	deltaDeletedRelativePaths []string,
	reconcile bool,
) facts.Envelope {
	payload := map[string]any{
		"graph_id":          repo.ID,
		"graph_kind":        "repository",
		"name":              repo.Name,
		"repo_id":           repo.ID,
		"parsed_file_count": fmt.Sprintf("%d", parsedFileCount),
		"is_dependency":     isDependency,
	}
	if repo.RepoSlug != "" {
		payload["repo_slug"] = repo.RepoSlug
	}
	if repo.RemoteURL != "" {
		payload["remote_url"] = repo.RemoteURL
	}
	if repo.LocalPath != "" {
		payload["local_path"] = repo.LocalPath
	}
	if len(importsMap) > 0 {
		payload["imports_map"] = importsMap
	}
	if defaultBranch != "" {
		payload["default_branch"] = defaultBranch
	}
	if len(gitRefsPayload) > 0 {
		payload["git_refs"] = gitRefsPayload
	}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = append([]string(nil), deltaRelativePaths...)
		payload["delta_deleted_relative_paths"] = append([]string(nil), deltaDeletedRelativePaths...)
	}
	if reconcile {
		payload["reconciliation_generation"] = true
	}
	if strings.TrimSpace(sourceRunID) != "" {
		payload["source_run_id"] = sourceRunID
	}

	return gitmodel.FactEnvelope("repository", scopeID, generationID, observedAt, "repository:"+repo.ID, payload, repoPath)
}

// FileFactEnvelope builds the durable file fact for one parsed file: its
// identity, relative path, and parsed metadata. The stable key is
// repo+relative-path so re-emission of a generation is idempotent.
// collapseFingerprintWallClock returns a copy of a parsed file map with the
// fingerprint wall-clock observation collapsed to zero. MicrosTotal is
// wall-clock timing: persisting it verbatim would bake machine-specific
// noise into every durable file fact and break byte-deterministic
// parser/replay output. Collector telemetry already recorded the real
// timing at prescan from the uncollapsed map, and outcome counts are
// durable evidence, so only the timing field is collapsed.
func collapseFingerprintWallClock(fileData map[string]any) map[string]any {
	stats, ok := fileData[fingerprint.StatsKey].(map[string]any)
	if !ok {
		return fileData
	}
	if _, ok := stats["micros_total"]; !ok {
		return fileData
	}
	out := make(map[string]any, len(fileData))
	for k, v := range fileData {
		out[k] = v
	}
	statsCopy := make(map[string]any, len(stats))
	for k, v := range stats {
		statsCopy[k] = v
	}
	statsCopy["micros_total"] = int64(0)
	out[fingerprint.StatsKey] = statsCopy
	return out
}

func FileFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	fileData map[string]any,
	isDependency bool,
) facts.Envelope {
	fileData = collapseFingerprintWallClock(fileData)
	filePath := gitmodel.PayloadPath(fileData, "path")
	relativePath := gitmodel.RepositoryRelativePath(repoPath, filePath)
	payload := map[string]any{
		"graph_id":         repoID + ":" + relativePath,
		"graph_kind":       "file",
		"repo_id":          repoID,
		"relative_path":    relativePath,
		"parsed_file_data": fileData,
		"is_dependency":    isDependency,
	}
	if language := gitmodel.PayloadString(fileData, "language", "lang"); language != "" {
		payload["language"] = language
	}

	return gitmodel.FactEnvelope("file", scopeID, generationID, observedAt, "file:"+repoID+":"+relativePath, payload, filePath)
}
