package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
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
		for _, base := range docsVerifyLocalPathBases(root, doc) {
			candidate, ok := safeJoinLocalPath(root, base, normalizedPath)
			if !ok {
				continue
			}
			if _, err := os.Stat(candidate); err == nil {
				return doctruth.LocalPathResolution{Supported: true, Exists: true}
			}
		}
		return doctruth.LocalPathResolution{Supported: true, Exists: false}
	}
}

func docsVerifyTruthRoot(verifyPath string) (string, bool) {
	start := verifyPath
	if start == "" {
		start = "."
	}
	absolute, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
		absolute = filepath.Dir(absolute)
	}
	for current := absolute; ; current = filepath.Dir(current) {
		if info, err := os.Stat(filepath.Join(current, ".git")); err == nil && info.IsDir() {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd, true
	}
	return absolute, true
}

func docsVerifyLocalPathBases(root string, doc doctruth.DocumentInput) []string {
	bases := []string{root}
	if docPath := filePathFromURI(doc.SourceURI); docPath != "" {
		bases = append(bases, filepath.Dir(docPath))
	} else if strings.TrimSpace(doc.Path) != "" {
		bases = append(bases, filepath.Dir(doc.Path))
	}
	return bases
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
