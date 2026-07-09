// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestParseRemoteGitRefsCapturesDefaultAndHeads(t *testing.T) {
	t.Parallel()

	output := "" +
		"ref: refs/heads/main\tHEAD\n" +
		"abc123\tHEAD\n" +
		"abc123\trefs/heads/main\n" +
		"def456\trefs/heads/release\n" +
		"ignored\trefs/tags/v1.0.0\n"

	refs, err := parseRemoteGitRefs(output)
	if err != nil {
		t.Fatalf("parseRemoteGitRefs() error = %v, want nil", err)
	}
	if got, want := len(refs), 2; got != want {
		t.Fatalf("len(refs) = %d, want %d: %#v", got, want, refs)
	}
	if got, want := refs[0].Name, "main"; got != want {
		t.Fatalf("refs[0].Name = %q, want %q", got, want)
	}
	if got, want := refs[0].Kind, "branch"; got != want {
		t.Fatalf("refs[0].Kind = %q, want %q", got, want)
	}
	if got, want := refs[0].HeadSHA, "abc123"; got != want {
		t.Fatalf("refs[0].HeadSHA = %q, want %q", got, want)
	}
	if got, want := refs[0].Default, true; got != want {
		t.Fatalf("refs[0].Default = %v, want %v", got, want)
	}
	if got, want := refs[1].Name, "release"; got != want {
		t.Fatalf("refs[1].Name = %q, want %q", got, want)
	}
	if got, want := refs[1].HeadSHA, "def456"; got != want {
		t.Fatalf("refs[1].HeadSHA = %q, want %q", got, want)
	}
}

func TestBuildSelectedRepositoriesCarriesGitRefs(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "service")
	refs := []GitRef{{
		Name:    "main",
		Kind:    "branch",
		HeadSHA: "abc123",
		Default: true,
	}}

	got := buildSelectedRepositories(
		RepoSyncConfig{ReposDir: reposDir, SourceMode: "githubOrg", GithubOrg: "example"},
		[]string{repoPath},
		nil,
		nil,
		nil,
		map[string][]GitRef{repoPath: refs},
	)
	if gotLen, wantLen := len(got), 1; gotLen != wantLen {
		t.Fatalf("len(got) = %d, want %d", gotLen, wantLen)
	}
	if gotRef, wantRef := got[0].GitRefs[0], refs[0]; gotRef != wantRef {
		t.Fatalf("GitRefs[0] = %#v, want %#v", gotRef, wantRef)
	}
}

func TestRepositoryFactEnvelopeCarriesGitRefs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 12, 15, 30, 0, 0, time.UTC)
	envelope := repositoryFactEnvelope(
		t.TempDir(),
		repositoryidentity.Metadata{ID: "r_service", Name: "service"},
		"source-run-1",
		"scope-1",
		"generation-1",
		observedAt,
		1,
		nil,
		false,
		[]GitRef{{
			Name:    "main",
			Kind:    "branch",
			HeadSHA: "abc123",
			Default: true,
		}},
		false,
		nil,
		nil,
		false,
	)

	if got, want := envelope.Payload["default_branch"], "main"; got != want {
		t.Fatalf("default_branch = %#v, want %#v", got, want)
	}
	refs, ok := envelope.Payload["git_refs"].([]map[string]any)
	if !ok {
		t.Fatalf("git_refs = %T, want []map[string]any", envelope.Payload["git_refs"])
	}
	if got, want := refs[0]["name"], "main"; got != want {
		t.Fatalf("git_refs[0].name = %#v, want %#v", got, want)
	}
	if got, want := refs[0]["head_sha"], "abc123"; got != want {
		t.Fatalf("git_refs[0].head_sha = %#v, want %#v", got, want)
	}
	if got, want := refs[0]["is_default"], true; got != want {
		t.Fatalf("git_refs[0].is_default = %#v, want %#v", got, want)
	}
}
