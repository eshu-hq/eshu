package collector

import (
	"context"
	"reflect"
	"testing"
)

func TestLoadRepoSyncConfigParsesRepositoryShard(t *testing.T) {
	t.Parallel()

	config, err := LoadRepoSyncConfig("ingester", repoSyncTestGetenv(map[string]string{
		"ESHU_REPO_SHARD_COUNT": "3",
		"ESHU_REPO_SHARD_INDEX": "1",
	}))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil", err)
	}
	if got, want := config.RepoShardCount, 3; got != want {
		t.Fatalf("RepoShardCount = %d, want %d", got, want)
	}
	if got, want := config.RepoShardIndex, 1; got != want {
		t.Fatalf("RepoShardIndex = %d, want %d", got, want)
	}
}

func TestLoadRepoSyncConfigAllowsDefaultRepositoryShardCount(t *testing.T) {
	t.Parallel()

	config, err := LoadRepoSyncConfig("ingester", repoSyncTestGetenv(map[string]string{
		"ESHU_REPO_SHARD_INDEX": "0",
	}))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil", err)
	}
	if got, want := config.RepoShardCount, 1; got != want {
		t.Fatalf("RepoShardCount = %d, want %d", got, want)
	}
	if got, want := config.RepoShardIndex, 0; got != want {
		t.Fatalf("RepoShardIndex = %d, want %d", got, want)
	}
}

func TestLoadRepoSyncConfigRejectsInvalidRepositoryShard(t *testing.T) {
	t.Parallel()

	_, err := LoadRepoSyncConfig("ingester", repoSyncTestGetenv(map[string]string{
		"ESHU_REPO_SHARD_COUNT": "3",
		"ESHU_REPO_SHARD_INDEX": "3",
	}))
	if err == nil {
		t.Fatal("LoadRepoSyncConfig() error = nil, want invalid shard error")
	}
}

func TestNativeRepositorySelectorAppliesRepositoryShardBeforeGitSync(t *testing.T) {
	t.Parallel()

	allRepositories := []string{
		"eshu-hq/api",
		"eshu-hq/console",
		"eshu-hq/ingester",
		"eshu-hq/reducer",
		"eshu-hq/worker",
	}
	config := RepoSyncConfig{
		ReposDir:       t.TempDir(),
		SourceMode:     "explicit",
		GitAuthMethod:  "none",
		CloneDepth:     1,
		RepoLimit:      4000,
		RepoShardCount: 2,
		RepoShardIndex: 1,
	}
	wantSynced := filterRepositoryIDsByShard(allRepositories, config)
	var gotSynced []string

	selector := NativeRepositorySelector{
		Config: config,
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: append([]string(nil), allRepositories...)}, nil
		},
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			gotSynced = append([]string(nil), repositoryIDs...)
			return GitSyncSelection{}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(gotSynced, wantSynced) {
		t.Fatalf("SyncGit repositoryIDs = %#v, want %#v", gotSynced, wantSynced)
	}
	if len(gotSynced) == 0 || len(gotSynced) == len(allRepositories) {
		t.Fatalf("shard selected %d of %d repos, want a strict subset", len(gotSynced), len(allRepositories))
	}
}

func TestNativeRepositorySelectorAppliesRepositoryShardBeforeFilesystemSync(t *testing.T) {
	t.Parallel()

	allRepositories := []string{
		"/repos/api",
		"/repos/console",
		"/repos/ingester",
		"/repos/reducer",
		"/repos/worker",
	}
	config := RepoSyncConfig{
		ReposDir:       t.TempDir(),
		SourceMode:     "filesystem",
		FilesystemRoot: t.TempDir(),
		RepoLimit:      4000,
		RepoShardCount: 2,
		RepoShardIndex: 0,
	}
	wantSynced := filterRepositoryIDsByShard(allRepositories, config)
	var gotSynced []string

	selector := NativeRepositorySelector{
		Config: config,
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: append([]string(nil), allRepositories...)}, nil
		},
		SyncFilesystem: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) ([]string, bool, error) {
			gotSynced = append([]string(nil), repositoryIDs...)
			return append([]string(nil), repositoryIDs...), true, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(gotSynced, wantSynced) {
		t.Fatalf("SyncFilesystem repositoryIDs = %#v, want %#v", gotSynced, wantSynced)
	}
	if len(gotSynced) == 0 || len(gotSynced) == len(allRepositories) {
		t.Fatalf("shard selected %d of %d repos, want a strict subset", len(gotSynced), len(allRepositories))
	}
}

func repoSyncTestGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
