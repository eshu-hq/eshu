package collector

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSelectGitHubRepositoryIDsSkipsArchivedUnlessAllowed(t *testing.T) {
	t.Parallel()

	selection := selectGitHubRepositoryIDs(
		[]GitHubRepositoryRecord{
			{RepoID: "eshu-hq/live", Archived: false},
			{RepoID: "eshu-hq/archived", Archived: true},
			{RepoID: "eshu-hq/forced", Archived: true},
		},
		[]RepoSyncRepositoryRule{
			{Kind: "exact", Value: "eshu-hq/forced"},
			{Kind: "regex", Value: "eshu-hq/.*"},
		},
		false,
	)

	if got, want := len(selection.RepositoryIDs), 2; got != want {
		t.Fatalf("len(RepositoryIDs) = %d, want %d", got, want)
	}
	if got, want := selection.RepositoryIDs[0], "eshu-hq/live"; got != want {
		t.Fatalf("RepositoryIDs[0] = %q, want %q", got, want)
	}
	if got, want := selection.RepositoryIDs[1], "eshu-hq/forced"; got != want {
		t.Fatalf("RepositoryIDs[1] = %q, want %q", got, want)
	}
	if got, want := len(selection.ArchivedRepositoryIDs), 1; got != want {
		t.Fatalf("len(ArchivedRepositoryIDs) = %d, want %d", got, want)
	}
	if got, want := selection.ArchivedRepositoryIDs[0], "eshu-hq/archived"; got != want {
		t.Fatalf("ArchivedRepositoryIDs[0] = %q, want %q", got, want)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemSyncsChangedRepositories(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "ignored.txt\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "ignored.txt"), "skip me\n")

	observedAt := time.Date(2026, time.April, 13, 20, 0, 0, 0, time.UTC)
	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:        reposDir,
			SourceMode:      "filesystem",
			FilesystemRoot:  filesystemRoot,
			Repositories:    nil,
			Component:       "collector-git",
			CloneDepth:      1,
			RepoLimit:       4000,
			GitAuthMethod:   "none",
			RepositoryRules: nil,
		},
		Now: func() time.Time {
			return observedAt
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}

	if got, want := batch.ObservedAt, observedAt; !got.Equal(want) {
		t.Fatalf("ObservedAt = %v, want %v", got, want)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	selectedRepo := batch.Repositories[0]
	wantRepoPath := filepath.Join(reposDir, "eshu-hq", "service-a")
	if got, want := selectedRepo.RepoPath, wantRepoPath; got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
	if selectedRepo.RemoteURL != "" {
		t.Fatalf("RemoteURL = %q, want empty for filesystem mode", selectedRepo.RemoteURL)
	}

	if _, err := os.Stat(filepath.Join(wantRepoPath, "main.go")); err != nil {
		t.Fatalf("copied repository missing main.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantRepoPath, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("ignored.txt stat error = %v, want not-exist after gitignore-filtered copy", err)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemPreservesGitHubWorkflows(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(
		t,
		filepath.Join(sourceRepo, ".github", "workflows", "deploy.yml"),
		"name: deploy\non:\n  push:\n",
	)

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	wantWorkflowPath := filepath.Join(reposDir, "eshu-hq", "service-a", ".github", "workflows", "deploy.yml")
	if _, err := os.Stat(wantWorkflowPath); err != nil {
		t.Fatalf("copied repository missing GitHub workflow %q: %v", wantWorkflowPath, err)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemReturnsEmptyBatchWhenManifestUnchanged(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
		Now: func() time.Time {
			return time.Date(2026, time.April, 13, 20, 30, 0, 0, time.UTC)
		},
	}

	firstBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("first SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(firstBatch.Repositories), 1; got != want {
		t.Fatalf("len(first.Repositories) = %d, want %d", got, want)
	}

	secondBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("second SelectRepositories() error = %v, want nil", err)
	}
	if got := len(secondBatch.Repositories); got != 0 {
		t.Fatalf("len(second.Repositories) = %d, want 0 when filesystem manifest is unchanged", got)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemRootRepository(t *testing.T) {
	t.Parallel()

	sourceRepo := t.TempDir()
	reposDir := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	observedAt := time.Date(2026, time.April, 13, 21, 15, 0, 0, time.UTC)
	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: sourceRepo,
			Component:      "bootstrap-index",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
		Now: func() time.Time {
			return observedAt
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	selectedRepo := batch.Repositories[0]
	wantRepoPath := filepath.Join(reposDir, filepath.Base(sourceRepo))
	if got, want := selectedRepo.RepoPath, wantRepoPath; got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(wantRepoPath, "main.go")); err != nil {
		t.Fatalf("copied repository missing main.go: %v", err)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemDirectRootRepository(t *testing.T) {
	t.Parallel()

	sourceRepo := t.TempDir()
	reposDir := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:         reposDir,
			SourceMode:       "filesystem",
			FilesystemRoot:   sourceRepo,
			FilesystemDirect: true,
			Component:        "bootstrap-index",
			CloneDepth:       1,
			RepoLimit:        4000,
			GitAuthMethod:    "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}
	if got, want := batch.Repositories[0].RepoPath, sourceRepo; got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemDirectWorkspaceRepositories(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:         reposDir,
			SourceMode:       "filesystem",
			FilesystemRoot:   filesystemRoot,
			FilesystemDirect: true,
			Component:        "workspace-index",
			CloneDepth:       1,
			RepoLimit:        4000,
			GitAuthMethod:    "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}
	if got, want := batch.Repositories[0].RepoPath, sourceRepo; got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesMarksDependencyTargets(t *testing.T) {
	t.Parallel()

	sourceRepo := t.TempDir()
	reposDir := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.py"), "def handler():\n    return 1\n")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:           reposDir,
			SourceMode:         "filesystem",
			FilesystemRoot:     sourceRepo,
			FilesystemDirect:   true,
			Component:          "bootstrap-index",
			CloneDepth:         1,
			RepoLimit:          4000,
			GitAuthMethod:      "none",
			DependencyMode:     true,
			DependencyName:     "@scope/service-lib",
			DependencyLanguage: "typescript",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	selected := batch.Repositories[0]
	if got, want := selected.RepoPath, sourceRepo; got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
	if got, want := selected.IsDependency, true; got != want {
		t.Fatalf("IsDependency = %t, want %t", got, want)
	}
	if got, want := selected.DisplayName, "@scope/service-lib"; got != want {
		t.Fatalf("DisplayName = %q, want %q", got, want)
	}
	if got, want := selected.Language, "typescript"; got != want {
		t.Fatalf("Language = %q, want %q", got, want)
	}
}

func TestLoadRepoSyncConfigNormalizesFilesystemFileTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	targetFile := filepath.Join(repoRoot, "src", "handler.py")
	writeSelectionTestFile(t, targetFile, "def handler():\n    return 1\n")

	config, err := LoadRepoSyncConfig("bootstrap-index", func(key string) string {
		switch key {
		case "ESHU_REPO_SOURCE_MODE":
			return "filesystem"
		case "ESHU_FILESYSTEM_ROOT":
			return targetFile
		case "ESHU_REPOS_DIR":
			return t.TempDir()
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(filepath.Dir(targetFile))
	if err != nil {
		resolvedRoot = filepath.Dir(targetFile)
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetFile)
	if err != nil {
		resolvedTarget = targetFile
	}

	if got, want := config.FilesystemRoot, resolvedRoot; got != want {
		t.Fatalf("FilesystemRoot = %q, want %q", got, want)
	}
	if got, want := config.FileTargets, []string{resolvedTarget}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FileTargets = %#v, want %#v", got, want)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemSingleFileTarget(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()
	reposDir := t.TempDir()
	targetFile := filepath.Join(sourceDir, "ignored.py")
	writeSelectionTestFile(t, filepath.Join(sourceDir, ".gitignore"), "ignored.py\n")
	writeSelectionTestFile(t, targetFile, "print('override')\n")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:         reposDir,
			SourceMode:       "filesystem",
			FilesystemRoot:   sourceDir,
			FilesystemDirect: true,
			FileTargets:      []string{targetFile},
			Component:        "bootstrap-index",
			CloneDepth:       1,
			RepoLimit:        4000,
			GitAuthMethod:    "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	selected := batch.Repositories[0]
	if got, want := selected.RepoPath, sourceDir; got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
	if got, want := selected.FileTargets, []string{targetFile}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FileTargets = %#v, want %#v", got, want)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesGitModesBuildsChangedRepoBatch(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	servicePath := filepath.Join(reposDir, "eshu-hq", "service-a")
	workerPath := filepath.Join(reposDir, "eshu-hq", "worker")

	observedAt := time.Date(2026, time.April, 13, 21, 0, 0, 0, time.UTC)
	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:      reposDir,
			SourceMode:    "explicit",
			GithubOrg:     "eshu-hq",
			Repositories:  []string{"eshu-hq/service-a", "eshu-hq/worker"},
			Component:     "ingester",
			CloneDepth:    1,
			RepoLimit:     4000,
			GitAuthMethod: "none",
		},
		Now: func() time.Time {
			return observedAt
		},
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{
				RepositoryIDs: []string{"eshu-hq/service-a", "eshu-hq/worker"},
			}, nil
		},
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{
				SelectedRepoPaths: []string{servicePath, workerPath},
			}, nil
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := batch.ObservedAt, observedAt; !got.Equal(want) {
		t.Fatalf("ObservedAt = %v, want %v", got, want)
	}
	if got, want := len(batch.Repositories), 2; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}
	if got, want := batch.Repositories[0].RepoPath, servicePath; got != want {
		t.Fatalf("Repositories[0].RepoPath = %q, want %q", got, want)
	}
	if got, want := batch.Repositories[0].RemoteURL, "https://github.com/eshu-hq/service-a.git"; got != want {
		t.Fatalf("Repositories[0].RemoteURL = %q, want %q", got, want)
	}
	if got, want := batch.Repositories[1].RepoPath, workerPath; got != want {
		t.Fatalf("Repositories[1].RepoPath = %q, want %q", got, want)
	}
	if got, want := batch.Repositories[1].RemoteURL, "https://github.com/eshu-hq/worker.git"; got != want {
		t.Fatalf("Repositories[1].RemoteURL = %q, want %q", got, want)
	}
}

func writeSelectionTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}
