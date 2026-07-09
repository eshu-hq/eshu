// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"crypto/sha1" // #nosec G505 -- non-cryptographic content digest for documentation file deduplication, not a security primitive
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func partitionNativeSnapshotFiles(files []discovery.FileWithSize, registry parser.Registry) ([]discovery.FileWithSize, []discovery.FileWithSize) {
	parserFiles := make([]discovery.FileWithSize, 0, len(files))
	documentationFiles := []discovery.FileWithSize{}
	for _, file := range files {
		if isGitDocumentationPath(file.Path) {
			if isNotebookDocumentationPath(file.Path) {
				if isParserPreferredDocumentationPath(file.Path, registry) {
					parserFiles = append(parserFiles, file)
				}
				documentationFiles = append(documentationFiles, file)
				continue
			}
			if isParserPreferredDocumentationPath(file.Path, registry) {
				parserFiles = append(parserFiles, file)
				continue
			}
			documentationFiles = append(documentationFiles, file)
			continue
		}
		parserFiles = append(parserFiles, file)
	}
	return parserFiles, documentationFiles
}

func parserPreScanFiles(files []discovery.FileWithSize) []discovery.FileWithSize {
	out := make([]discovery.FileWithSize, 0, len(files))
	for _, file := range files {
		if isNotebookDocumentationPath(file.Path) {
			continue
		}
		out = append(out, file)
	}
	return out
}

// sortUniqueFileWithSizeSlice deduplicates and sorts a slice of FileWithSize
// by Path. When two entries share the same Path the first is kept.
func sortUniqueFileWithSizeSlice(files []discovery.FileWithSize) []discovery.FileWithSize {
	if len(files) <= 1 {
		return files
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	unique := files[:1]
	for i := 1; i < len(files); i++ {
		if files[i].Path != unique[len(unique)-1].Path {
			unique = append(unique, files[i])
		}
	}
	return unique
}

func isParserPreferredDocumentationPath(filePath string, registry parser.Registry) bool {
	if strings.ToLower(filepath.Ext(filePath)) == ".ipynb" {
		_, ok := registry.LookupByPath(filePath)
		return ok
	}
	if strings.ToLower(filepath.Ext(filePath)) != ".txt" {
		return false
	}
	_, ok := registry.LookupByPath(filePath)
	return ok
}

func isNotebookDocumentationPath(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".ipynb"
}

func documentationFileMetasForPaths(repoPath string, paths []string, commitSHA string) []ContentFileMeta {
	metas := make([]ContentFileMeta, 0, len(paths))
	for _, filePath := range paths {
		relativePath, err := filepath.Rel(repoPath, filePath)
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		format, ok := gitDocumentationFormatForPath(relativePath)
		if !ok {
			continue
		}
		digest, ok := documentationDigestForFile(filePath)
		if !ok {
			continue
		}
		metas = append(metas, ContentFileMeta{
			RelativePath: relativePath,
			Digest:       digest,
			Language:     format.language,
			ArtifactType: "documentation",
			CommitSHA:    commitSHA,
		})
	}
	return metas
}

func documentationDigestForFile(filePath string) (string, bool) {
	file, err := os.Open(filePath) // #nosec G304 -- reads indexed repo documentation file at a path derived from the scan target, not user-supplied input
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	hash := sha1.New() // #nosec G401 -- non-cryptographic content digest for documentation file deduplication, not a security primitive
	if _, err := io.Copy(hash, file); err != nil {
		return "", false
	}
	return hex.EncodeToString(hash.Sum(nil)), true
}
