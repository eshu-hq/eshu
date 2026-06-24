// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func fileTombstoneEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	isDependency bool,
) facts.Envelope {
	relativePath = filepath.ToSlash(filepath.Clean(relativePath))
	payload := map[string]any{
		"graph_id":      repoID + ":" + relativePath,
		"graph_kind":    "file",
		"repo_id":       repoID,
		"relative_path": relativePath,
		"is_dependency": isDependency,
	}
	envelope := factEnvelope(
		"file",
		scopeID,
		generationID,
		observedAt,
		"file:"+repoID+":"+relativePath,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(relativePath)),
	)
	envelope.IsTombstone = true
	return envelope
}

func contentTombstoneEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
) facts.Envelope {
	relativePath = filepath.ToSlash(filepath.Clean(relativePath))
	payload := map[string]any{
		"content_path":   relativePath,
		"content_digest": "",
		"repo_id":        repoID,
	}
	envelope := factEnvelope(
		"content",
		scopeID,
		generationID,
		observedAt,
		"content:"+repoID+":"+relativePath,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(relativePath)),
	)
	envelope.IsTombstone = true
	return envelope
}
