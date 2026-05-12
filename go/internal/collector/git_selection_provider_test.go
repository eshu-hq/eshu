package collector

import "testing"

func TestRepoRemoteURLUsesProviderScopedHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		auth   string
		repoID string
		want   string
	}{
		{
			name:   "github default",
			repoID: "eshu-hq/eshu",
			want:   "https://github.com/eshu-hq/eshu.git",
		},
		{
			name:   "gitlab https",
			repoID: "gitlab/eshu-hq/eshu",
			want:   "https://gitlab.com/eshu-hq/eshu.git",
		},
		{
			name:   "bitbucket https",
			repoID: "bitbucket/eshu-hq/eshu",
			want:   "https://bitbucket.org/eshu-hq/eshu.git",
		},
		{
			name:   "bitbucket ssh",
			auth:   "ssh",
			repoID: "bitbucket/eshu-hq/eshu",
			want:   "git@bitbucket.org:eshu-hq/eshu.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := repoRemoteURL(RepoSyncConfig{GitAuthMethod: tt.auth}, tt.repoID)
			if got != tt.want {
				t.Fatalf("repoRemoteURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
