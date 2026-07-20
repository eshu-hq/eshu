// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestParseRemoteGitRefsRejectsColonInTagName(t *testing.T) {
	t.Parallel()

	output := "" +
		"ref: refs/heads/main\tHEAD\n" +
		"abc123\tHEAD\n" +
		"abc123\trefs/heads/main\n" +
		"def456\trefs/tags/v1:2.0\n"

	_, err := parseRemoteGitRefs(output)
	if err == nil {
		t.Fatalf("parseRemoteGitRefs() error = nil, want error for tag name containing colon")
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

// TestRemoteGitRefsIncludesTags runs real git ls-remote against a bare
// remote carrying both annotated and lightweight tags, verifying that the
// argv change (adding refs/tags/*) actually produces tag entries.
func TestRemoteGitRefsIncludesTags(t *testing.T) {
	// Real-git test — skip when git is not available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found in PATH: %v", err)
	}

	// 1. Create a bare repository to serve as the remote.
	barePath := t.TempDir()
	runGit(t, barePath, "init", "--bare")

	// 2. Create a working repository with origin pointing to the bare repo.
	workPath := t.TempDir()
	runGit(t, workPath, "init", "-b", "main")
	runGit(t, workPath, "config", "user.email", "test@example.com")
	runGit(t, workPath, "config", "user.name", "Test")
	runGit(t, workPath, "remote", "add", "origin", barePath)

	// 3. Create a commit so we have a branch to push.
	writeFile(t, workPath, "README.md", "# Test repo")
	runGit(t, workPath, "add", "README.md")
	runGit(t, workPath, "commit", "-m", "initial commit")
	commitSHA := strings.TrimSpace(runGit(t, workPath, "rev-parse", "HEAD"))

	// 4. Create an annotated tag (two objects: tag object + commit).
	runGit(t, workPath, "tag", "-a", "v1.0.0", "-m", "annotated tag", commitSHA)
	// Create a lightweight tag (one object: commit).
	runGit(t, workPath, "tag", "lightweight", commitSHA)

	// 5. Push branches and tags to the bare remote.
	runGit(t, workPath, "push", "origin", "main", "--tags")

	// 6. Call remoteGitRefs — the function under test.
	ctx := context.Background()
	config := RepoSyncConfig{ReposDir: workPath}
	refs, err := remoteGitRefs(ctx, config, workPath, "" /* token */)
	if err != nil {
		t.Fatalf("remoteGitRefs() error = %v, want nil", err)
	}

	// 7. Assert: at least the branch + 2 tags.
	if got := len(refs); got < 3 {
		t.Fatalf("len(refs) = %d, want >= 3 (main + v1.0.0 + lightweight): %#v", got, refs)
	}

	// Find the branch and tag entries.
	var branchMain *GitRef
	var tagV1 *GitRef
	var tagLightweight *GitRef
	for i := range refs {
		switch {
		case refs[i].Kind == "branch" && refs[i].Name == "main":
			branchMain = &refs[i]
		case refs[i].Kind == "tag" && refs[i].Name == "v1.0.0":
			tagV1 = &refs[i]
		case refs[i].Kind == "tag" && refs[i].Name == "lightweight":
			tagLightweight = &refs[i]
		}
	}

	if branchMain == nil {
		t.Fatalf("branch 'main' not found in refs: %#v", refs)
	}
	if branchMain.HeadSHA != commitSHA {
		t.Fatalf("branch main HeadSHA = %s, want %s", branchMain.HeadSHA, commitSHA)
	}

	if tagV1 == nil {
		t.Fatalf("tag 'v1.0.0' not found in refs: %#v", refs)
	}
	// Annotated tag: HeadSHA must be the peeled commit, not the tag object.
	if tagV1.HeadSHA != commitSHA {
		t.Fatalf("annotated tag v1.0.0 HeadSHA = %s, want %s (peeled commit)", tagV1.HeadSHA, commitSHA)
	}
	if tagV1.Default {
		t.Fatal("tag v1.0.0 Default = true, want false")
	}

	if tagLightweight == nil {
		t.Fatalf("tag 'lightweight' not found in refs: %#v", refs)
	}
	if tagLightweight.HeadSHA != commitSHA {
		t.Fatalf("lightweight tag HeadSHA = %s, want %s", tagLightweight.HeadSHA, commitSHA)
	}
	if tagLightweight.Default {
		t.Fatal("tag lightweight Default = true, want false")
	}
}

// TestLocalGitRefsIncludesTags proves that localGitRefs discovers branches
// and tags from a local repo with no origin remote — the path filesystem-mode
// collectors use.
func TestLocalGitRefsIncludesTags(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found in PATH: %v", err)
	}

	repoPath := t.TempDir()
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")

	writeFile(t, repoPath, "README.md", "# Local repo")
	runGit(t, repoPath, "add", "README.md")
	runGit(t, repoPath, "commit", "-m", "initial commit")
	commitSHA := strings.TrimSpace(runGit(t, repoPath, "rev-parse", "HEAD"))

	// Annotated tag.
	runGit(t, repoPath, "tag", "-a", "v1.0.0", "-m", "annotated", commitSHA)
	// Lightweight tag.
	runGit(t, repoPath, "tag", "lightweight", commitSHA)

	ctx := context.Background()
	refs, err := localGitRefs(ctx, repoPath)
	if err != nil {
		t.Fatalf("localGitRefs() error = %v, want nil", err)
	}

	if got := len(refs); got < 3 {
		t.Fatalf("len(refs) = %d, want >= 3 (main + v1.0.0 + lightweight): %#v", got, refs)
	}

	// Branch main is default.
	var branch *GitRef
	var tagV1 *GitRef
	var tagLW *GitRef
	for i := range refs {
		switch {
		case refs[i].Kind == "branch" && refs[i].Name == "main":
			branch = &refs[i]
		case refs[i].Kind == "tag" && refs[i].Name == "v1.0.0":
			tagV1 = &refs[i]
		case refs[i].Kind == "tag" && refs[i].Name == "lightweight":
			tagLW = &refs[i]
		}
	}
	if branch == nil || !branch.Default {
		t.Fatalf("branch main missing or not default: %#v", branch)
	}
	if tagV1 == nil || tagV1.HeadSHA != commitSHA {
		t.Fatalf("annotated tag v1.0.0 missing or wrong SHA: %#v", tagV1)
	}
	if tagLW == nil || tagLW.HeadSHA != commitSHA {
		t.Fatalf("lightweight tag missing or wrong SHA: %#v", tagLW)
	}
}

// TestSelectRepositoriesFilesystemCarriesGitRefs proves that managed
// filesystem mode (the default) discovers git refs from the source path
// (with .git) even though copyRepositoryTree strips dotfiles from the
// target copy. Before the fix, collectLocalRefs ran on the target path
// (no .git -> silent skip -> zero refs -> legacy fallback -> tags: []).
func TestSelectRepositoriesFilesystemCarriesGitRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found in PATH: %v", err)
	}

	// 1. Create a real git repo with an annotated tag.
	filesystemRoot := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "tagged-project")
	if err := os.MkdirAll(sourceRepo, 0o750); err != nil {
		t.Fatalf("create source repo dir: %v", err)
	}
	runGit(t, sourceRepo, "init", "-b", "main")
	runGit(t, sourceRepo, "config", "user.email", "test@example.com")
	runGit(t, sourceRepo, "config", "user.name", "Test")
	writeFile(t, sourceRepo, "README.md", "# tagged project")
	runGit(t, sourceRepo, "add", "README.md")
	runGit(t, sourceRepo, "commit", "-m", "initial")
	commitSHA := strings.TrimSpace(runGit(t, sourceRepo, "rev-parse", "HEAD"))
	runGit(t, sourceRepo, "tag", "-a", "v1.0.0", "-m", "annotated", commitSHA)

	// 2. Set up managed filesystem mode (NOT filesystemDirect).
	reposDir := t.TempDir()
	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
			GithubOrg:      "acme",
		},
		Now: func() time.Time {
			return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	// 3. Select repositories.
	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	// 4. Assert: the selected repository carries GitRefs with the tag.
	refs := batch.Repositories[0].GitRefs
	var tagV1 *GitRef
	for i := range refs {
		if refs[i].Kind == "tag" && refs[i].Name == "v1.0.0" {
			tagV1 = &refs[i]
		}
	}
	if tagV1 == nil {
		t.Fatalf("tag 'v1.0.0' not found in GitRefs: %#v", refs)
	}
	if tagV1.HeadSHA != commitSHA {
		t.Fatalf("tag v1.0.0 HeadSHA = %s, want %s (peeled commit)", tagV1.HeadSHA, commitSHA)
	}
	if tagV1.Default {
		t.Fatal("tag Default = true, want false")
	}
}
