// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// repositoryTreeFileLimit bounds the number of indexed files scanned when
// building a repository file tree. The tree is derived from the Postgres
// content store (content_files), so this keeps the read bounded for very large
// repositories; callers see `truncated: true` when the cap is reached.
const repositoryTreeFileLimit = 50000

// getRepositoryTree returns one directory level (or the full subtree with
// ?recursive=true) of a repository's indexed files, derived from the content
// store. The directory layout is reconstructed from content_files relative
// paths; no source bytes are returned here (see the content endpoint).
//
// GET /api/v0/repositories/{repo_id}/tree?ref={ref}&path={subpath}&recursive=true
func (h *RepositoryHandler) getRepositoryTree(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	repoRef, _, err := h.repositoryStatsRepositoryRef(ctx, repoID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository failed: %v", err))
		return
	}
	if repoRef == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	requestPath := normalizeTreePath(r.URL.Query().Get("path"))
	requestedRef := strings.TrimSpace(r.URL.Query().Get("ref"))
	recursive, _ := strconv.ParseBool(r.URL.Query().Get("recursive"))
	languageFilter := repositoryTreeLanguageFilter(r.URL.Query().Get("language"))

	var files []FileContent
	if h.Content != nil {
		files, err = h.Content.ListRepoFiles(ctx, repoID, repositoryTreeFileLimit+1)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list repository files failed: %v", err))
			return
		}
	}
	indexedRef := repositoryTreeRef(files)
	if status, message, err := validateSelectedRepositoryRef(ctx, h.Content, repoID, requestedRef, indexedRef); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository refs failed: %v", err))
		return
	} else if status != 0 {
		WriteError(w, status, message)
		return
	}
	truncated := len(files) > repositoryTreeFileLimit
	if truncated {
		files = files[:repositoryTreeFileLimit]
	}

	entries, matched := buildRepositoryTree(files, requestPath, recursive, languageFilter)
	if requestPath != "" && !matched {
		WriteError(w, http.StatusNotFound, "path not found")
		return
	}

	response := map[string]any{
		"ref":       indexedRef,
		"path":      requestPath,
		"entries":   entries,
		"truncated": truncated,
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		response,
		BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			TruthBasisContentIndex,
			"reconstructed from bounded content-index file listing; directory layout reflects indexed paths only",
		),
	)
}

// normalizeTreePath trims surrounding whitespace and slashes so an empty,
// "/", or "cmd/app/" path all resolve to a consistent internal form.
func normalizeTreePath(raw string) string {
	return strings.Trim(strings.TrimSpace(raw), "/")
}

// repositoryTreeLanguageFilter builds the language-match set for a ?language=
// query param, or nil when the param is empty (no filtering). It reuses
// repositoryLanguageFamily so the alias expansion matches the by-language
// inventory endpoint (e.g. ?language=typescript also matches tsx, ?language=
// terraform also matches hcl/tfvars). Values are lowercased for a
// case-insensitive match against the stored file language.
func repositoryTreeLanguageFilter(raw string) map[string]bool {
	family := repositoryLanguageFamily(raw)
	if len(family) == 0 {
		return nil
	}
	set := make(map[string]bool, len(family))
	for _, lang := range family {
		set[strings.ToLower(strings.TrimSpace(lang))] = true
	}
	return set
}

// repositoryTreeRef returns the indexed commit SHA shared by the listed files.
// Only one commit SHA is recorded per indexed file today, so this is the single
// ref the tree was built from; it is empty when no files carry a SHA.
func repositoryTreeRef(files []FileContent) string {
	for _, file := range files {
		if file.CommitSHA != "" {
			return file.CommitSHA
		}
	}
	return ""
}

// buildRepositoryTree reconstructs directory and file entries under requestPath
// from the flat content-store file list. It returns the entries plus whether
// requestPath actually matched any indexed file, so the caller can distinguish
// an unknown path (404) from the repository root.
//
// child_count on a directory entry is the number of descendant files under that
// directory subtree, in both single-level and recursive modes.
//
// languageFilter, when non-nil, restricts the returned files (and the directory
// child_counts) to files whose language is in the set. It is applied AFTER the
// path-prefix match so that `matched` still reflects path existence: filtering a
// real path down to zero language matches yields an empty listing, not a 404.
func buildRepositoryTree(files []FileContent, requestPath string, recursive bool, languageFilter map[string]bool) ([]map[string]any, bool) {
	prefix := ""
	if requestPath != "" {
		prefix = requestPath + "/"
	}

	dirCounts := map[string]int{}
	fileEntries := make([]map[string]any, 0)
	matched := requestPath == ""

	for _, file := range files {
		relativePath := file.RelativePath
		if prefix != "" && !strings.HasPrefix(relativePath, prefix) {
			continue
		}
		matched = true
		if languageFilter != nil && !languageFilter[strings.ToLower(strings.TrimSpace(file.Language))] {
			continue
		}
		remainder := strings.TrimPrefix(relativePath, prefix)
		if remainder == "" {
			continue
		}

		segments := strings.Split(remainder, "/")
		if len(segments) == 1 {
			fileEntries = append(fileEntries, repositoryTreeFileEntry(file, segments[0], relativePath))
			continue
		}

		if recursive {
			fileEntries = append(fileEntries, repositoryTreeFileEntry(file, segments[len(segments)-1], relativePath))
			// Count this file against every ancestor directory in the subtree.
			for depth := 1; depth < len(segments); depth++ {
				dirCounts[joinTreePath(requestPath, strings.Join(segments[:depth], "/"))]++
			}
			continue
		}
		// Single level: collapse everything below the first segment into a dir.
		dirCounts[joinTreePath(requestPath, segments[0])]++
	}

	dirEntries := make([]map[string]any, 0, len(dirCounts))
	for dirPath, count := range dirCounts {
		dirEntries = append(dirEntries, map[string]any{
			"name":        treeBaseName(dirPath),
			"type":        "dir",
			"path":        dirPath,
			"child_count": count,
		})
	}

	entries := append(dirEntries, fileEntries...)
	sortRepositoryTreeEntries(entries)
	return entries, matched
}

// repositoryTreeFileEntry builds one file entry, attaching size (line count)
// and language when the content store recorded them.
func repositoryTreeFileEntry(file FileContent, name, relativePath string) map[string]any {
	entry := map[string]any{
		"name": name,
		"type": "file",
		"path": relativePath,
	}
	if file.LineCount > 0 {
		entry["size"] = file.LineCount
	}
	if file.Language != "" {
		entry["language"] = file.Language
	}
	return entry
}

// joinTreePath joins a base directory and a relative remainder, treating an
// empty base as the repository root.
func joinTreePath(base, remainder string) string {
	if base == "" {
		return remainder
	}
	return base + "/" + remainder
}

// treeBaseName returns the final path segment used as an entry's display name.
func treeBaseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// sortRepositoryTreeEntries orders directories before files, then by path, so
// the listing is deterministic for the UI and tests.
func sortRepositoryTreeEntries(entries []map[string]any) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		leftDir := left["type"] == "dir"
		rightDir := right["type"] == "dir"
		if leftDir != rightDir {
			return leftDir
		}
		return StringVal(left, "path") < StringVal(right, "path")
	})
}
