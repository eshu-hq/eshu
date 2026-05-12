package webhook

import "testing"

func TestNormalizeBitbucketPushAcceptsDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"actor": {"nickname": "linuxdynasty"},
		"repository": {
			"uuid": "{repo-uuid}",
			"full_name": "eshu-hq/eshu",
			"mainbranch": {"name": "main"}
		},
		"push": {
			"changes": [{
				"old": {"target": {"hash": "1111111111111111111111111111111111111111"}},
				"new": {
					"type": "branch",
					"name": "main",
					"target": {"hash": "2222222222222222222222222222222222222222"}
				}
			}]
		}
	}`)

	trigger, err := NormalizeBitbucket("repo:push", "request-1", payload, "")
	if err != nil {
		t.Fatalf("NormalizeBitbucket() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderBitbucket,
		EventKind:            EventKindPush,
		Decision:             DecisionAccepted,
		DeliveryID:           "request-1",
		RepositoryExternalID: "{repo-uuid}",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		BeforeSHA:            "1111111111111111111111111111111111111111",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeBitbucketPullRequestAcceptsFulfilledDefaultBranch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"actor": {"nickname": "linuxdynasty"},
		"repository": {
			"uuid": "{repo-uuid}",
			"full_name": "eshu-hq/eshu",
			"mainbranch": {"name": "main"}
		},
		"pullrequest": {
			"state": "MERGED",
			"destination": {"branch": {"name": "main"}},
			"merge_commit": {"hash": "3333333333333333333333333333333333333333"}
		}
	}`)

	trigger, err := NormalizeBitbucket("pullrequest:fulfilled", "request-2", payload, "")
	if err != nil {
		t.Fatalf("NormalizeBitbucket() error = %v, want nil", err)
	}

	assertTrigger(t, trigger, Trigger{
		Provider:             ProviderBitbucket,
		EventKind:            EventKindPullRequestMerged,
		Decision:             DecisionAccepted,
		DeliveryID:           "request-2",
		RepositoryExternalID: "{repo-uuid}",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "3333333333333333333333333333333333333333",
		Action:               "fulfilled",
		Sender:               "linuxdynasty",
	})
}

func TestNormalizeBitbucketIgnoresNonRefreshingEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		reason  DecisionReason
	}{
		{
			name: "non default branch push",
			payload: `{
				"repository": {"uuid": "{repo-uuid}", "full_name": "eshu-hq/eshu", "mainbranch": {"name": "main"}},
				"push": {"changes": [{"new": {"type": "branch", "name": "feature", "target": {"hash": "2222222222222222222222222222222222222222"}}}]}
			}`,
			reason: ReasonNonDefaultBranch,
		},
		{
			name: "tag push",
			payload: `{
				"repository": {"uuid": "{repo-uuid}", "full_name": "eshu-hq/eshu", "mainbranch": {"name": "main"}},
				"push": {"changes": [{"new": {"type": "tag", "name": "v1.0.0", "target": {"hash": "2222222222222222222222222222222222222222"}}}]}
			}`,
			reason: ReasonTagRef,
		},
		{
			name: "branch delete",
			payload: `{
				"repository": {"uuid": "{repo-uuid}", "full_name": "eshu-hq/eshu", "mainbranch": {"name": "main"}},
				"push": {"changes": [{"old": {"name": "main", "target": {"hash": "1111111111111111111111111111111111111111"}}, "new": null}]}
			}`,
			reason: ReasonDeletedBranch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			trigger, err := NormalizeBitbucket("repo:push", "request-3", []byte(tt.payload), "")
			if err != nil {
				t.Fatalf("NormalizeBitbucket() error = %v, want nil", err)
			}
			if trigger.Decision != DecisionIgnored {
				t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
			}
			if trigger.Reason != tt.reason {
				t.Fatalf("Reason = %q, want %q", trigger.Reason, tt.reason)
			}
		})
	}
}

func TestNormalizeBitbucketPullRequestRequiresMergeCommit(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"repository": {
			"uuid": "{repo-uuid}",
			"full_name": "eshu-hq/eshu",
			"mainbranch": {"name": "main"}
		},
		"pullrequest": {
			"state": "MERGED",
			"destination": {"branch": {"name": "main"}},
			"source": {"commit": {"hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
			"merge_commit": null
		}
	}`)

	trigger, err := NormalizeBitbucket("pullrequest:fulfilled", "request-4", payload, "")
	if err != nil {
		t.Fatalf("NormalizeBitbucket() error = %v, want nil", err)
	}
	if trigger.Decision != DecisionIgnored {
		t.Fatalf("Decision = %q, want %q", trigger.Decision, DecisionIgnored)
	}
	if trigger.Reason != ReasonMissingMergeCommit {
		t.Fatalf("Reason = %q, want %q", trigger.Reason, ReasonMissingMergeCommit)
	}
}
