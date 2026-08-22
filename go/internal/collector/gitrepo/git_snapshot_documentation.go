// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitdocs"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func partitionNativeSnapshotFiles(files []discovery.FileWithSize, registry parser.Registry) ([]discovery.FileWithSize, []discovery.FileWithSize) {
	parserFiles := make([]discovery.FileWithSize, 0, len(files))
	documentationFiles := []discovery.FileWithSize{}
	for _, file := range files {
		if gitdocs.IsGitDocumentationPath(file.Path) {
			if gitdocs.IsNotebookDocumentationPath(file.Path) {
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
		if gitdocs.IsNotebookDocumentationPath(file.Path) {
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
