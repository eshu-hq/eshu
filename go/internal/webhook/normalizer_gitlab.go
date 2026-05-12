package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	gitlabPushHook         = "push hook"
	gitlabTagPushHook      = "tag push hook"
	gitlabMergeRequestHook = "merge request hook"
)

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
	trigger.Sender = strings.TrimSpace(event.User.Username)

	if trigger.Action != "merge" && trigger.Action != "merged" {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonMergeRequestNotMerged
		return trigger, nil
	}
	if trigger.TargetSHA == "" {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonMissingMergeCommit
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
