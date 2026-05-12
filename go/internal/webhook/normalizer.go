package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	githubEventPush        = "push"
	githubEventPullRequest = "pull_request"
	gitlabPushHook         = "push hook"
	gitlabTagPushHook      = "tag push hook"
	gitlabMergeRequestHook = "merge request hook"
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

// NormalizeGitLab converts a verified GitLab event payload into a refresh
// trigger decision.
func NormalizeGitLab(eventHeader string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	event := strings.ToLower(strings.TrimSpace(eventHeader))
	switch event {
	case gitlabPushHook, gitlabTagPushHook:
		return normalizeGitLabPush(deliveryID, payload, defaultBranchFallback)
	case gitlabMergeRequestHook:
		return normalizeGitLabMergeRequest(deliveryID, payload, defaultBranchFallback)
	default:
		return normalizeGitLabByObjectKind(eventHeader, deliveryID, payload, defaultBranchFallback)
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
	if trigger.TargetSHA == "" {
		trigger.TargetSHA = strings.TrimSpace(event.PullRequest.Head.SHA)
	}

	if trigger.Action != "closed" || !event.PullRequest.Merged {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonPullRequestNotMerged
		return trigger, nil
	}
	return decideBranchTrigger(trigger)
}

func normalizeGitLabPush(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event gitlabPushPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderGitLab, EventKindPush, deliveryID, event.Project.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	trigger.Ref = strings.TrimSpace(event.Ref)
	trigger.BeforeSHA = strings.TrimSpace(event.Before)
	trigger.TargetSHA = strings.TrimSpace(event.After)
	trigger.Sender = strings.TrimSpace(event.UserUsername)

	return decideBranchTrigger(trigger)
}

func normalizeGitLabMergeRequest(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event gitlabMergeRequestPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderGitLab, EventKindMergeRequestMerged, deliveryID, event.Project.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	trigger.Action = strings.TrimSpace(event.ObjectAttributes.Action)
	trigger.Ref = branchRef(strings.TrimSpace(event.ObjectAttributes.TargetBranch))
	trigger.TargetSHA = strings.TrimSpace(event.ObjectAttributes.MergeCommitSHA)
	if trigger.TargetSHA == "" {
		trigger.TargetSHA = strings.TrimSpace(event.ObjectAttributes.LastCommit.ID)
	}
	trigger.Sender = strings.TrimSpace(event.User.Username)

	if trigger.Action != "merge" && trigger.Action != "merged" {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonMergeRequestNotMerged
		return trigger, nil
	}
	return decideBranchTrigger(trigger)
}

func normalizeGitLabByObjectKind(eventHeader string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var envelope struct {
		ObjectKind string `json:"object_kind"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Trigger{}, err
	}
	switch strings.TrimSpace(envelope.ObjectKind) {
	case "push", "tag_push":
		return normalizeGitLabPush(deliveryID, payload, defaultBranchFallback)
	case "merge_request":
		return normalizeGitLabMergeRequest(deliveryID, payload, defaultBranchFallback)
	default:
		return Trigger{}, fmt.Errorf("unsupported gitlab webhook event %q", eventHeader)
	}
}

func newTrigger(provider Provider, kind EventKind, deliveryID string, repo repositoryFields) (Trigger, error) {
	if repo.externalID == "" {
		return Trigger{}, errors.New("repository external id is required")
	}
	if repo.fullName == "" {
		return Trigger{}, errors.New("repository full name is required")
	}
	if repo.defaultBranch == "" {
		return Trigger{}, errors.New("repository default branch is required")
	}
	return Trigger{
		Provider:             provider,
		EventKind:            kind,
		DeliveryID:           strings.TrimSpace(deliveryID),
		RepositoryExternalID: repo.externalID,
		RepositoryFullName:   repo.fullName,
		DefaultBranch:        repo.defaultBranch,
	}, nil
}

func decideBranchTrigger(trigger Trigger) (Trigger, error) {
	if isTagRef(trigger.Ref) {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonTagRef
		return trigger, nil
	}

	branch, ok := branchFromRef(trigger.Ref)
	if !ok || branch != trigger.DefaultBranch {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonNonDefaultBranch
		return trigger, nil
	}
	if trigger.TargetSHA == "" {
		return Trigger{}, errors.New("target sha is required")
	}
	trigger.Decision = DecisionAccepted
	return trigger, nil
}

func branchFromRef(ref string) (string, bool) {
	return strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/")
}

func isTagRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "refs/tags/")
}

func branchRef(branch string) string {
	if branch == "" {
		return ""
	}
	return "refs/heads/" + branch
}

type repositoryFields struct {
	externalID    string
	fullName      string
	defaultBranch string
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

type gitlabProject struct {
	ID            json.RawMessage `json:"id"`
	PathWithNS    string          `json:"path_with_namespace"`
	DefaultBranch string          `json:"default_branch"`
}

func (project gitlabProject) common(defaultBranchFallback string) repositoryFields {
	return repositoryFields{
		externalID:    rawScalar(project.ID),
		fullName:      strings.TrimSpace(project.PathWithNS),
		defaultBranch: firstNonEmpty(project.DefaultBranch, defaultBranchFallback),
	}
}

func rawScalar(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.String()
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

type gitlabPushPayload struct {
	Ref          string        `json:"ref"`
	Before       string        `json:"before"`
	After        string        `json:"after"`
	Project      gitlabProject `json:"project"`
	UserUsername string        `json:"user_username"`
}

type gitlabMergeRequestPayload struct {
	Project          gitlabProject `json:"project"`
	ObjectAttributes struct {
		Action         string `json:"action"`
		TargetBranch   string `json:"target_branch"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		LastCommit     struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
}
