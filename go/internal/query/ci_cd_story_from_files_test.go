// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

// fixedRepoFilesStore serves one fixed file list from ListRepoFiles and counts
// the calls, delegating every other read to the default fake store.
type fixedRepoFilesStore struct {
	fakePortContentStore
	files []querycontract.FileContent
	calls int
}

func (s *fixedRepoFilesStore) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	s.calls++
	return s.files, nil
}

// TestStaticWorkflowArtifactEvidenceFromFilesMatchesListingVariant pins #7126:
// the repository story hands the file list it already read to the CI/CD
// evidence instead of having it list the repository a third time. The
// FromFiles variants must return exactly what the listing variants return for
// the same files, for a repository with workflows, without workflows, with no
// files, with no repository scope, and with no content store, and must not list.
func TestStaticWorkflowArtifactEvidenceFromFilesMatchesListingVariant(t *testing.T) {
	t.Parallel()

	workflow := querycontract.FileContent{RepoID: "repo-1", RelativePath: ".github/workflows/ci.yml", ArtifactType: "github_actions_workflow"}
	other := querycontract.FileContent{RepoID: "repo-1", RelativePath: "src/main.go"}
	cases := []struct {
		name  string
		repo  string
		files []querycontract.FileContent
		nilCS bool
	}{
		{name: "workflow_present", repo: "repo-1", files: []querycontract.FileContent{other, workflow}},
		{name: "workflow_absent", repo: "repo-1", files: []querycontract.FileContent{other}},
		{name: "no_files", repo: "repo-1", files: nil},
		{name: "no_repository_scope", repo: "", files: []querycontract.FileContent{workflow}},
		{name: "no_content_store", repo: "repo-1", files: []querycontract.FileContent{workflow}, nilCS: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			listing := &fixedRepoFilesStore{files: tc.files}
			var listingStore, fromFilesStore querycontract.ContentStore = listing, &fixedRepoFilesStore{files: tc.files}
			if tc.nilCS {
				listingStore, fromFilesStore = nil, nil
			}

			want := artifacts.StaticWorkflowArtifactEvidence(t.Context(), listingStore, tc.repo)
			got := artifacts.StaticWorkflowArtifactEvidenceFromFiles(t.Context(), fromFilesStore, tc.repo, tc.files)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("StaticWorkflowArtifactEvidenceFromFiles = %#v, want %#v", got, want)
			}

			wantSummary, wantErr := artifacts.LoadRepositoryScopedCICDEvidence(t.Context(), listingStore, nil, tc.repo)
			gotSummary, gotErr := artifacts.LoadRepositoryScopedCICDEvidenceFromFiles(t.Context(), fromFilesStore, nil, tc.repo, tc.files)
			if (gotErr == nil) != (wantErr == nil) || !reflect.DeepEqual(gotSummary, wantSummary) {
				t.Fatalf("LoadRepositoryScopedCICDEvidenceFromFiles = %#v, %v; want %#v, %v", gotSummary, gotErr, wantSummary, wantErr)
			}
			if store, ok := fromFilesStore.(*fixedRepoFilesStore); ok && store.calls != 0 {
				t.Fatalf("FromFiles variants listed the repository %d times, want 0", store.calls)
			}
		})
	}
}
