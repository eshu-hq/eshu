// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (r *preChangeImpactRequest) UnmarshalJSON(data []byte) error {
	type preChangeImpactRequestAlias preChangeImpactRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var decoded preChangeImpactRequestAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = preChangeImpactRequest(decoded)
	r.changedPathsProvided = jsonFieldProvided(raw, "changed_paths")
	r.changesProvided = jsonFieldProvided(raw, "changes")
	return nil
}

func jsonFieldProvided(raw map[string]json.RawMessage, key string) bool {
	value, ok := raw[key]
	return ok && strings.TrimSpace(string(value)) != "null"
}

type preChangeCodeSurfaceError struct {
	err error
}

func (e preChangeCodeSurfaceError) Error() string {
	return e.err.Error()
}

func (e preChangeCodeSurfaceError) Unwrap() error {
	return e.err
}

func preChangeImpactErrorStatus(err error) int {
	// #5167 W3: a repo_id outside the caller's grant renders as not-found, the
	// same as every other cross-tenant selector in this family, rather than the
	// operational-failure statuses below.
	if errors.Is(err, ErrChangeSurfaceRepoNotGranted) {
		return http.StatusNotFound
	}
	var codeSurfaceErr preChangeCodeSurfaceError
	if errors.As(err, &codeSurfaceErr) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

func preChangeMode(req preChangeImpactRequest) string {
	switch {
	case req.BaseRef != "" || req.HeadRef != "":
		return "ref_diff"
	case len(req.Changes) > 0:
		return "file_list"
	default:
		return "target_or_topic"
	}
}

func preChangeFileMaps(repoID string, changes []preChangeFileChange) []map[string]any {
	files := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		row := map[string]any{
			"repo_id": repoID,
			"path":    change.Path,
			"status":  change.Status,
			"source_handle": map[string]any{
				"repo_id":       repoID,
				"relative_path": change.Path,
			},
		}
		if change.OldPath != "" {
			row["old_path"] = change.OldPath
		}
		files = append(files, row)
	}
	return files
}
