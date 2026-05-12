package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	githubEventPush        = "push"
	githubEventPullRequest = "pull_request"
)

// NormalizeGitHub converts a verified GitHub event payload into a refresh
// trigger decision.
func NormalizeGitHub(event string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	switch strings.TrimSpace(event) {
	case githubEventPush:
		return normalizeGitHubPush(deliveryID, payload, defaultBranchFallback)
	case githubEventPullRequest:
		return normalizeGitHubPullRequest(deliveryID, payload, defaultBranchFallback)
	default:
		return Trigger{}, fmt.Errorf("unsupported github webhook event %q", event)
	}
}

func normalizeGitHubPush(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event githubPushPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderGitHub, EventKindPush, deliveryID, event.Repository.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	trigger.Ref = strings.TrimSpace(event.Ref)
	trigger.BeforeSHA = strings.TrimSpace(event.Before)
	trigger.TargetSHA = strings.TrimSpace(event.After)
	trigger.Sender = strings.TrimSpace(event.Sender.Login)

	return decideBranchTrigger(trigger)
}

func normalizeGitHubPullRequest(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event githubPullRequestPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderGitHub, EventKindPullRequestMerged, deliveryID, event.Repository.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	trigger.Action = strings.TrimSpace(event.Action)
	trigger.Ref = branchRef(strings.TrimSpace(event.PullRequest.Base.Ref))
	trigger.TargetSHA = strings.TrimSpace(event.PullRequest.MergeCommitSHA)

	if trigger.Action != "closed" || !event.PullRequest.Merged {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonPullRequestNotMerged
		return trigger, nil
	}
	if trigger.TargetSHA == "" {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonMissingMergeCommit
		return trigger, nil
	}
	return decideBranchTrigger(trigger)
}

type githubRepository struct {
	ID            json.RawMessage `json:"id"`
	FullName      string          `json:"full_name"`
	DefaultBranch string          `json:"default_branch"`
}

func (repo githubRepository) common(defaultBranchFallback string) repositoryFields {
	return repositoryFields{
		externalID:    rawScalar(repo.ID),
		fullName:      strings.TrimSpace(repo.FullName),
		defaultBranch: firstNonEmpty(repo.DefaultBranch, defaultBranchFallback),
	}
}

type githubPushPayload struct {
	Ref        string           `json:"ref"`
	Before     string           `json:"before"`
	After      string           `json:"after"`
	Repository githubRepository `json:"repository"`
	Sender     struct {
		Login string `json:"login"`
	} `json:"sender"`
}

type githubPullRequestPayload struct {
	Action      string           `json:"action"`
	Repository  githubRepository `json:"repository"`
	PullRequest struct {
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		Base           struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}
