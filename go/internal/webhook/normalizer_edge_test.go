package webhook

import "testing"

func TestNormalizeGitHubPullRequestIgnoresMergedEventWithoutMergeCommit(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"action": "closed",
		"pull_request": {
			"merged": true,
			"merge_commit_sha": "",
			"base": {"ref": "main"},
			"head": {"sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
		},
		"repository": {
			"id": 42,
			"full_name": "eshu-hq/eshu",
			"default_branch": "main"
		}
	}`)

	trigger, err := NormalizeGitHub("pull_request", "delivery-missing-merge", payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonMissingMergeCommit {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonMissingMergeCommit)
	}
	if trigger.TargetSHA != "" {
		t.Fatalf("TargetSHA = %q, want empty SHA when merge commit is missing", trigger.TargetSHA)
	}
}

func TestNormalizeGitLabMergeRequestIgnoresMergedEventWithoutMergeCommit(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"object_kind": "merge_request",
		"project": {
			"id": 77,
			"path_with_namespace": "eshu-hq/eshu",
			"default_branch": "main"
		},
		"object_attributes": {
			"action": "merge",
			"target_branch": "main",
			"merge_commit_sha": "",
			"last_commit": {"id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
		},
		"user": {"username": "linuxdynasty"}
	}`)

	trigger, err := NormalizeGitLab("Merge Request Hook", "delivery-missing-merge", payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitLab() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonMissingMergeCommit {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonMissingMergeCommit)
	}
	if trigger.TargetSHA != "" {
		t.Fatalf("TargetSHA = %q, want empty SHA when merge commit is missing", trigger.TargetSHA)
	}
}

func TestNormalizePushIgnoresDefaultBranchDeletes(t *testing.T) {
	t.Parallel()

	zeroSHA := "0000000000000000000000000000000000000000"
	tests := []struct {
		name        string
		provider    Provider
		eventHeader string
		payload     string
	}{
		{
			name:        "github",
			provider:    ProviderGitHub,
			eventHeader: "push",
			payload: `{
				"ref": "refs/heads/main",
				"after": "` + zeroSHA + `",
				"repository": {"id": 42, "full_name": "eshu-hq/eshu", "default_branch": "main"}
			}`,
		},
		{
			name:        "gitlab",
			provider:    ProviderGitLab,
			eventHeader: "Push Hook",
			payload: `{
				"object_kind": "push",
				"ref": "refs/heads/main",
				"after": "` + zeroSHA + `",
				"project": {"id": 77, "path_with_namespace": "eshu-hq/eshu", "default_branch": "main"}
			}`,
		},
		{
			name:        "bitbucket",
			provider:    ProviderBitbucket,
			eventHeader: "repo:push",
			payload: `{
				"repository": {"uuid": "{repo-uuid}", "full_name": "eshu-hq/eshu", "mainbranch": {"name": "main"}},
				"push": {"changes": [{"new": {"type": "branch", "name": "main", "target": {"hash": "` + zeroSHA + `"}}}]}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				trigger Trigger
				err     error
			)
			switch tt.provider {
			case ProviderGitHub:
				trigger, err = NormalizeGitHub(tt.eventHeader, "delivery-delete", []byte(tt.payload), "")
			case ProviderGitLab:
				trigger, err = NormalizeGitLab(tt.eventHeader, "delivery-delete", []byte(tt.payload), "")
			case ProviderBitbucket:
				trigger, err = NormalizeBitbucket(tt.eventHeader, "delivery-delete", []byte(tt.payload), "")
			default:
				t.Fatalf("unsupported provider %q", tt.provider)
			}
			if err != nil {
				t.Fatalf("Normalize() error = %v, want nil", err)
			}
			if trigger.Decision != DecisionIgnored {
				t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
			}
			if trigger.Reason != ReasonDeletedBranch {
				t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonDeletedBranch)
			}
		})
	}
}
