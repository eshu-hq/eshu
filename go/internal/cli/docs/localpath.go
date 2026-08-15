// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

// LocalPathResolver builds the resolver that checks a documented repo-relative
// path against the workspace on disk. It returns nil when no workspace root can
// be resolved from verifyPath, which leaves local-path claims unchecked instead
// of judged against the wrong tree.
//
// Each claim is tried against the workspace root and against the directory of
// the document that made it. A candidate escaping the workspace root is not
// stat-ed at all; if no candidate could be checked the claim reports
// unsupported (missing evidence) rather than contradicted.
func LocalPathResolver(verifyPath string) doctruth.LocalPathResolver {
	root, ok := TruthRoot(verifyPath)
	if !ok {
		return nil
	}
	return func(doc doctruth.DocumentInput, normalizedPath string) doctruth.LocalPathResolution {
		if strings.TrimSpace(normalizedPath) == "" {
			return doctruth.LocalPathResolution{}
		}
		checked := false
		for _, base := range localPathBases(root, doc) {
			candidate, ok := safeJoinLocalPath(root, base, normalizedPath)
			if !ok {
				continue
			}
			if _, err := os.Stat(candidate); err == nil {
				return doctruth.LocalPathResolution{Supported: true, Exists: true}
			} else if os.IsNotExist(err) {
				checked = true
			} else {
				return doctruth.LocalPathResolution{}
			}
		}
		if !checked {
			return doctruth.LocalPathResolution{}
		}
		return doctruth.LocalPathResolution{Supported: true, Exists: false}
	}
}

// TruthRoot resolves the workspace root that bounds every filesystem-backed
// truth lookup (local paths, container image manifests, Terraform files). The
// second return reports whether a root was found; a false means the caller
// should skip that truth source rather than fall back to the process working
// directory.
func TruthRoot(verifyPath string) (string, bool) {
	start := verifyPath
	if start == "" {
		start = "."
	}
	root, err := eshulocal.ResolveWorkspaceRoot(start, "")
	if err != nil {
		return "", false
	}
	return root, true
}

// localPathBases lists the directories a documented relative path is resolved
// against: the workspace root first, then the directory holding the document.
func localPathBases(root string, doc doctruth.DocumentInput) []string {
	bases := []string{root}
	if docPath := filePathFromURI(doc.SourceURI); docPath != "" {
		bases = append(bases, resolvedDir(filepath.Dir(docPath)))
	} else if strings.TrimSpace(doc.Path) != "" {
		bases = append(bases, resolvedDir(filepath.Dir(doc.Path)))
	}
	return bases
}

// resolvedDir follows symlinks so a candidate under a symlinked directory
// compares against the same real root safeJoinLocalPath checks. A directory
// that cannot be resolved falls back to its cleaned form.
func resolvedDir(dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return resolved
}

// filePathFromURI extracts the filesystem path from a file:// URI, returning
// empty for any other scheme or an unparsable value.
func filePathFromURI(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	return parsed.Path
}

// safeJoinLocalPath joins a documented relative path onto base and reports
// whether the result stays inside root. An absolute claim or one that climbs
// out of the workspace is rejected, so a documented `../../outside.yaml` is
// never stat-ed against the host filesystem.
func safeJoinLocalPath(root string, base string, normalizedPath string) (string, bool) {
	if filepath.IsAbs(normalizedPath) {
		return "", false
	}
	candidate := filepath.Clean(filepath.Join(base, filepath.FromSlash(normalizedPath)))
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return candidate, true
}
