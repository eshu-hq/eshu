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
		// Tags are now captured, not ignored.
		"tagobj01\trefs/tags/v1.0.0\n" +
		"abc123\trefs/tags/v1.0.0^{}\n" // annotated tag — peeled commit

	refs, err := parseRemoteGitRefs(output)
	if err != nil {
		t.Fatalf("parseRemoteGitRefs() error = %v, want nil", err)
	}
	if got, want := len(refs), 3; got != want {
		t.Fatalf("len(refs) = %d, want %d (2 branches + 1 tag): %#v", got, want, refs)
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
	// Tag v1.0.0 — kind=tag, head_sha = peeled commit (abc123), never default.
	if got, want := refs[2].Name, "v1.0.0"; got != want {
		t.Fatalf("refs[2].Name = %q, want %q", got, want)
	}
	if got, want := refs[2].Kind, "tag"; got != want {
		t.Fatalf("refs[2].Kind = %q, want %q", got, want)
	}
	if got, want := refs[2].HeadSHA, "abc123"; got != want {
		t.Fatalf("refs[2].HeadSHA = %q, want %q (peeled commit)", got, want)
	}
	if got := refs[2].Default; got != false {
		t.Fatalf("refs[2].Default = %v, want false (tags are never default)", got)
	}
}

func TestParseRemoteGitRefsCapturesLightweightTag(t *testing.T) {
	t.Parallel()

	output := "" +
		"ref: refs/heads/main\tHEAD\n" +
		"abc123\tHEAD\n" +
		"abc123\trefs/heads/main\n" +
		// Lightweight tag — no ^{} line, SHA is directly the commit.
		"abc123\trefs/tags/v1.0.0\n"

	refs, err := parseRemoteGitRefs(output)
	if err != nil {
		t.Fatalf("parseRemoteGitRefs() error = %v, want nil", err)
	}
	if got, want := len(refs), 2; got != want {
		t.Fatalf("len(refs) = %d, want %d: %#v", got, want, refs)
	}
	// Tag is sorted after branch.
	if got, want := refs[1].Name, "v1.0.0"; got != want {
		t.Fatalf("refs[1].Name = %q, want %q", got, want)
	}
	if got, want := refs[1].Kind, "tag"; got != want {
		t.Fatalf("refs[1].Kind = %q, want %q", got, want)
	}
	if got, want := refs[1].HeadSHA, "abc123"; got != want {
		t.Fatalf("refs[1].HeadSHA = %q, want %q", got, want)
	}
}

func TestParseRemoteGitRefsAnnotatedTagPeeledCommit(t *testing.T) {
	t.Parallel()

	// Annotated tags produce two lines: the tag object, then the peeled commit.
	output := "" +
		"ref: refs/heads/main\tHEAD\n" +
		"abc123\tHEAD\n" +
		"abc123\trefs/heads/main\n" +
		"tagobj02\trefs/tags/v2.0.0\n" +
		"def456\trefs/tags/v2.0.0^{}\n"

	refs, err := parseRemoteGitRefs(output)
	if err != nil {
		t.Fatalf("parseRemoteGitRefs() error = %v, want nil", err)
	}
	if got, want := len(refs), 2; got != want {
		t.Fatalf("len(refs) = %d, want %d: %#v", got, want, refs)
	}
	if got, want := refs[1].Name, "v2.0.0"; got != want {
		t.Fatalf("refs[1].Name = %q, want %q", got, want)
	}
	if got, want := refs[1].HeadSHA, "def456"; got != want {
		t.Fatalf("refs[1].HeadSHA = %q, want %q (peeled commit, not tag object)", got, want)
	}
}

func TestParseRemoteGitRefsTagSameNameAsBranch(t *testing.T) {
	t.Parallel()

	output := "" +
		"ref: refs/heads/main\tHEAD\n" +
		"abc123\tHEAD\n" +
		"abc123\trefs/heads/main\n" +
		"def456\trefs/tags/main\n"

	refs, err := parseRemoteGitRefs(output)
	if err != nil {
		t.Fatalf("parseRemoteGitRefs() error = %v, want nil", err)
	}
	if got, want := len(refs), 2; got != want {
		t.Fatalf("len(refs) = %d, want %d (branch \"main\" + tag \"main\"): %#v", got, want, refs)
	}
	// Branch main is default.
	if refs[0].Kind != "branch" || refs[0].Name != "main" || !refs[0].Default {
		t.Fatalf("refs[0] = %#v, want branch main default=true", refs[0])
	}
	// Tag main is separate, not default.
	if refs[1].Kind != "tag" || refs[1].Name != "main" || refs[1].Default {
		t.Fatalf("refs[1] = %#v, want tag main default=false", refs[1])
	}
}

func TestParseRemoteGitRefsEmptyTags(t *testing.T) {
	t.Parallel()

	output := "" +
		"ref: refs/heads/main\tHEAD\n" +
		"abc123\tHEAD\n" +
		"abc123\trefs/heads/main\n"

	refs, err := parseRemoteGitRefs(output)
	if err != nil {
		t.Fatalf("parseRemoteGitRefs() error = %v, want nil", err)
	}
	if got, want := len(refs), 1; got != want {
		t.Fatalf("len(refs) = %d, want %d (only branch, no tags): %#v", got, want, refs)
	}
}

func TestParseRemoteGitRefsInvalidTagName(t *testing.T) {
	t.Parallel()

	output := "" +
		"ref: refs/heads/main\tHEAD\n" +
		"abc123\tHEAD\n" +
		"abc123\trefs/heads/main\n" +
		"def456\trefs/tags/bad..tag\n"

	_, err := parseRemoteGitRefs(output)
	if err == nil {
		t.Fatalf("parseRemoteGitRefs() error = nil, want error for invalid tag name")
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
