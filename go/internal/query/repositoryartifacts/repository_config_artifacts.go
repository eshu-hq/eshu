// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The config-artifact extractors, the per-repository function and the shared
// content_files statements live in go/internal/content/artifacts, so a derive
// in storage/postgres can run the same code without importing a query package.
// This file is the query package's adapter to it: the exported names the
// repository handlers and the service handlers already call, the file type
// conversion, and the content-store port.

// BuildRepositoryConfigArtifacts extracts the config-path rows of one
// repository from its hydrated files. It adapts the file type and calls
// contentartifacts.BuildConfigPaths.
func BuildRepositoryConfigArtifacts(repoName string, files []querycontract.FileContent) map[string]any {
	return contentartifacts.BuildConfigPaths(repoName, filesFromContract(files))
}

// CleanRepositoryRelativePath normalizes a repository-relative path. It is
// contentartifacts.CleanRelativePath under the name the handlers already use.
func CleanRepositoryRelativePath(relativePath string) string {
	return contentartifacts.CleanRelativePath(relativePath)
}

// NormalizeConfigArtifactExpression resolves a Terraform or Terragrunt
// expression to a local config path. It is
// contentartifacts.NormalizeConfigArtifactExpression under the name the impact
// handlers already use.
func NormalizeConfigArtifactExpression(expression string, localAssignments map[string]string) string {
	return contentartifacts.NormalizeConfigArtifactExpression(expression, localAssignments)
}

// decodeYAMLMaps keeps the in-package spelling for the workflow and GitHub
// Actions readers; the decoder is shared with the compose and kustomize
// extractors.
func decodeYAMLMaps(content string) ([]map[string]any, error) {
	return contentartifacts.DecodeYAMLMaps(content)
}

// fileFromContract adapts one querycontract.FileContent to the neutral file
// type. SearchBackend is a search-page marker, not a content_files column, and
// is not carried.
func fileFromContract(file querycontract.FileContent) contentartifacts.File {
	return contentartifacts.File{
		RepoID:       file.RepoID,
		RelativePath: file.RelativePath,
		CommitSHA:    file.CommitSHA,
		Content:      file.Content,
		ContentHash:  file.ContentHash,
		LineCount:    file.LineCount,
		Language:     file.Language,
		ArtifactType: file.ArtifactType,
	}
}

// filesFromContract adapts a file list. A nil list stays nil.
func filesFromContract(files []querycontract.FileContent) []contentartifacts.File {
	if files == nil {
		return nil
	}
	adapted := make([]contentartifacts.File, len(files))
	for i, file := range files {
		adapted[i] = fileFromContract(file)
	}
	return adapted
}

// contentStoreReader adapts a querycontract.ContentStore to the neutral
// contentartifacts.Reader. The store's ListRepoFiles, GetFileContents and
// NonBlankRepoFilePaths run the shared statements of the neutral package, so
// the statement counting wrapper around the store still sees every read.
type contentStoreReader struct {
	store querycontract.ContentStore
}

// ListFiles lists a repository's files through ContentStore.ListRepoFiles.
func (r contentStoreReader) ListFiles(ctx context.Context, repoID string, limit int) ([]contentartifacts.File, error) {
	rows, err := r.store.ListRepoFiles(ctx, repoID, limit)
	if err != nil {
		return nil, err
	}
	return filesFromContract(rows), nil
}

// ReadFiles reads stored rows through ContentStore.GetFileContents.
func (r contentStoreReader) ReadFiles(ctx context.Context, repoID string, relativePaths []string) ([]contentartifacts.File, error) {
	rows, err := r.store.GetFileContents(ctx, repoID, relativePaths)
	if err != nil {
		return nil, err
	}
	return filesFromContract(rows), nil
}

// NonBlankPaths asks the store which paths hold non-blank content.
func (r contentStoreReader) NonBlankPaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error) {
	return r.store.NonBlankRepoFilePaths(ctx, repoID, relativePaths)
}
