package terraformstate

import (
	"context"
	"testing"
)

func TestDiscoverySkipsGitLocalCandidatesUntilApproved(t *testing.T) {
	t.Parallel()

	facts := &stubBackendFactReader{
		candidates: []DiscoveryCandidate{{
			State: StateKey{
				BackendKind: BackendLocal,
				Locator:     "/repos/platform-infra/env/prod/terraform.tfstate",
			},
			Source:       DiscoveryCandidateSourceGitLocalFile,
			RepoID:       "platform-infra",
			RelativePath: "env/prod/terraform.tfstate",
		}},
	}
	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph:      true,
			LocalRepos: []string{"platform-infra"},
			LocalStateCandidates: LocalStateCandidatePolicy{
				Mode: LocalStateCandidateModeDiscoverOnly,
			},
		},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": true}},
		BackendFacts: facts,
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("len(candidates) = %d, want 0 for discover-only local state candidate", len(candidates))
	}
}

func TestDiscoveryAllowsApprovedGitLocalCandidate(t *testing.T) {
	t.Parallel()

	facts := &stubBackendFactReader{
		candidates: []DiscoveryCandidate{{
			State: StateKey{
				BackendKind: BackendLocal,
				Locator:     "/repos/platform-infra/env/prod/terraform.tfstate",
			},
			Source:       DiscoveryCandidateSourceGitLocalFile,
			RepoID:       "platform-infra",
			RelativePath: "env/prod/terraform.tfstate",
		}},
	}
	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph:      true,
			LocalRepos: []string{"platform-infra"},
			LocalStateCandidates: LocalStateCandidatePolicy{
				Mode: LocalStateCandidateModeApproved,
				Approved: []LocalStateCandidateRef{{
					RepoID:       "platform-infra",
					RelativePath: "env/prod/terraform.tfstate",
				}},
			},
		},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": true}},
		BackendFacts: facts,
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].Source, DiscoveryCandidateSourceGitLocalFile; got != want {
		t.Fatalf("Source = %q, want %q", got, want)
	}
	if !candidates[0].StateInVCS {
		t.Fatal("StateInVCS = false, want true for git-local approved candidate")
	}
	if got, want := facts.lastQuery.IncludeLocalStateCandidates, true; got != want {
		t.Fatalf("IncludeLocalStateCandidates = %v, want %v", got, want)
	}
	if got, want := facts.lastQuery.ApprovedLocalCandidates[0].RelativePath, "env/prod/terraform.tfstate"; got != want {
		t.Fatalf("ApprovedLocalCandidates[0].RelativePath = %q, want %q", got, want)
	}
}

func TestDiscoverySkipsIgnoredGitLocalCandidate(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph:      true,
			LocalRepos: []string{"platform-infra"},
			LocalStateCandidates: LocalStateCandidatePolicy{
				Mode: LocalStateCandidateModeApproved,
				Approved: []LocalStateCandidateRef{{
					RepoID:       "platform-infra",
					RelativePath: "env/prod/terraform.tfstate",
				}},
				Ignored: []LocalStateCandidateRef{{
					RepoID:       "platform-infra",
					RelativePath: "env/prod/terraform.tfstate",
				}},
			},
		},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": true}},
		BackendFacts: &stubBackendFactReader{
			candidates: []DiscoveryCandidate{{
				State: StateKey{
					BackendKind: BackendLocal,
					Locator:     "/repos/platform-infra/env/prod/terraform.tfstate",
				},
				Source:       DiscoveryCandidateSourceGitLocalFile,
				RepoID:       "platform-infra",
				RelativePath: "env/prod/terraform.tfstate",
			}},
		},
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("len(candidates) = %d, want ignored git-local candidate skipped", len(candidates))
	}
}
