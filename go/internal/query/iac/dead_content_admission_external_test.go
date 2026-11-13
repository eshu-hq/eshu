// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/iac"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// externalDeadContentAdmissionStub is the satellite-side shape: an
// external package that names only exported identifiers. It proves
// the supplemental pin is servable from outside package iac — a pin
// the satellite cannot serve is void on arrival.
type externalDeadContentAdmissionStub struct {
	lastRepoID string
	lastPath   string
	lastLimit  int
	files      []querycontract.FileContent
	repos      []querycontract.RepositoryCatalogEntry
}

func (s *externalDeadContentAdmissionStub) ListRepositories(
	_ context.Context,
) ([]querycontract.RepositoryCatalogEntry, error) {
	return append([]querycontract.RepositoryCatalogEntry(nil), s.repos...), nil
}

func (s *externalDeadContentAdmissionStub) MatchRepositories(
	_ context.Context,
	selector string,
) ([]querycontract.RepositoryCatalogEntry, error) {
	var matched []querycontract.RepositoryCatalogEntry
	for _, repo := range s.repos {
		if repo.Name == selector || repo.ID == selector {
			matched = append(matched, repo)
		}
	}
	return matched, nil
}

func (s *externalDeadContentAdmissionStub) ListRepoFiles(
	_ context.Context,
	repoID string,
	limit int,
) ([]querycontract.FileContent, error) {
	s.lastRepoID = repoID
	s.lastLimit = limit
	return append([]querycontract.FileContent(nil), s.files...), nil
}

func (s *externalDeadContentAdmissionStub) GetFileContent(
	_ context.Context,
	repoID string,
	relativePath string,
) (*querycontract.FileContent, error) {
	s.lastRepoID = repoID
	s.lastPath = relativePath
	for i := range s.files {
		if s.files[i].RelativePath == relativePath {
			row := s.files[i]
			return &row, nil
		}
	}
	return nil, nil
}

var _ iac.IaCDeadContentAdmission = (*externalDeadContentAdmissionStub)(nil)

// TestExternalPackageCanServeDeadContentAdmission proves the pin needs
// no unexported seam: an external package serves all four reads from
// exported shapes alone. All shapes here are already exported, so
// unlike the search supplement there is no kind to map.
func TestExternalPackageCanServeDeadContentAdmission(t *testing.T) {
	t.Parallel()

	stub := &externalDeadContentAdmissionStub{
		repos: []querycontract.RepositoryCatalogEntry{{ID: "repo-infra", Name: "infra"}},
		files: []querycontract.FileContent{{RepoID: "repo-infra", RelativePath: "main.tf", Content: `resource "aws_s3_bucket" "logs" {}`}},
	}
	var admission iac.IaCDeadContentAdmission = stub

	repos, err := admission.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("external ListRepositories: %v", err)
	}
	if len(repos) != 1 || repos[0].ID != "repo-infra" {
		t.Fatalf("external ListRepositories = %#v, want the stub catalog row", repos)
	}

	matched, err := admission.MatchRepositories(t.Context(), "infra")
	if err != nil {
		t.Fatalf("external MatchRepositories: %v", err)
	}
	if len(matched) != 1 || matched[0].ID != "repo-infra" {
		t.Fatalf("external MatchRepositories = %#v, want the stub catalog row", matched)
	}

	files, err := admission.ListRepoFiles(t.Context(), "repo-infra", 10000)
	if err != nil {
		t.Fatalf("external ListRepoFiles: %v", err)
	}
	if len(files) != 1 || files[0].RelativePath != "main.tf" {
		t.Fatalf("external ListRepoFiles = %#v, want the stub file row", files)
	}
	if stub.lastRepoID != "repo-infra" || stub.lastLimit != 10000 {
		t.Fatalf("external ListRepoFiles saw repo %q limit %d, want %q limit %d", stub.lastRepoID, stub.lastLimit, "repo-infra", 10000)
	}

	got, err := admission.GetFileContent(t.Context(), "repo-infra", "main.tf")
	if err != nil {
		t.Fatalf("external GetFileContent: %v", err)
	}
	if got == nil || got.Content != `resource "aws_s3_bucket" "logs" {}` {
		t.Fatalf("external GetFileContent = %#v, want the stub file content", got)
	}
	if stub.lastPath != "main.tf" {
		t.Fatalf("external GetFileContent saw path %q, want %q", stub.lastPath, "main.tf")
	}
}
