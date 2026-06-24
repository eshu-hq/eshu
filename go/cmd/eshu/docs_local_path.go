// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

func docsVerifyLocalPathResolver(verifyPath string) doctruth.LocalPathResolver {
	root, ok := docsVerifyTruthRoot(verifyPath)
	if !ok {
		return nil
	}
	return func(doc doctruth.DocumentInput, normalizedPath string) doctruth.LocalPathResolution {
		if strings.TrimSpace(normalizedPath) == "" {
			return doctruth.LocalPathResolution{}
		}
		checked := false
		for _, base := range docsVerifyLocalPathBases(root, doc) {
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

func docsVerifyTruthRoot(verifyPath string) (string, bool) {
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

func docsVerifyLocalPathBases(root string, doc doctruth.DocumentInput) []string {
	bases := []string{root}
	if docPath := filePathFromURI(doc.SourceURI); docPath != "" {
		bases = append(bases, resolvedDir(filepath.Dir(docPath)))
	} else if strings.TrimSpace(doc.Path) != "" {
		bases = append(bases, resolvedDir(filepath.Dir(doc.Path)))
	}
	return bases
}

func resolvedDir(dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return resolved
}

func filePathFromURI(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	return parsed.Path
}

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
