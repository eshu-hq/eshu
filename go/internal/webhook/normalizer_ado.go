// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	adoEventPush               = "git.push"
	adoEventPullRequestUpdated = "git.pullrequest.updated"
)

// NormalizeAzureDevOps converts a verified Azure DevOps Services event payload
// into a refresh trigger decision.
func NormalizeAzureDevOps(event string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	switch strings.TrimSpace(event) {
	case adoEventPush:
		return normalizeAzureDevOpsPush(deliveryID, payload, defaultBranchFallback)
	case adoEventPullRequestUpdated:
		return normalizeAzureDevOpsPullRequestUpdated(deliveryID, payload, defaultBranchFallback)
	default:
		return Trigger{}, fmt.Errorf("unsupported azure devops webhook event %q", event)
	}
}

func normalizeAzureDevOpsPush(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event adoPushPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderAzureDevOps, EventKindPush, deliveryID, event.Resource.Repository.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	if len(event.Resource.RefUpdates) == 0 {
		return Trigger{}, fmt.Errorf("azure devops push event did not include ref updates")
	}
	refUpdate := event.Resource.RefUpdates[0]
	trigger.Ref = strings.TrimSpace(refUpdate.Name)
	trigger.BeforeSHA = strings.TrimSpace(refUpdate.OldObjectID)
	trigger.TargetSHA = strings.TrimSpace(refUpdate.NewObjectID)
	trigger.Sender = strings.TrimSpace(event.Resource.PushedBy.DisplayName)

	return decideBranchTrigger(trigger)
}

func normalizeAzureDevOpsPullRequestUpdated(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event adoPullRequestPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderAzureDevOps, EventKindPullRequestMerged, deliveryID, event.Resource.Repository.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	trigger.Action = strings.TrimSpace(event.Resource.Status)
	trigger.Ref = strings.TrimSpace(event.Resource.TargetRefName)
	if event.Resource.LastMergeCommit != nil {
		trigger.TargetSHA = strings.TrimSpace(event.Resource.LastMergeCommit.CommitID)
	}
	trigger.Sender = strings.TrimSpace(event.Resource.CreatedBy.DisplayName)
	if event.Resource.PullRequestID > 0 {
		trigger.PullRequestNumber = fmt.Sprint(event.Resource.PullRequestID)
	}

	if !strings.EqualFold(event.Resource.Status, "completed") || !strings.EqualFold(event.Resource.MergeStatus, "succeeded") {
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

type adoRepository struct {
	ID            json.RawMessage `json:"id"`
	Name          string          `json:"name"`
	DefaultBranch string          `json:"defaultBranch"`
}

func (repo adoRepository) common(defaultBranchFallback string) repositoryFields {
	defaultBranch := firstNonEmpty(repo.DefaultBranch, defaultBranchFallback)
	if branch, ok := branchFromRef(defaultBranch); ok {
		defaultBranch = branch
	}
	return repositoryFields{
		externalID:    rawScalar(repo.ID),
		fullName:      strings.TrimSpace(repo.Name),
		defaultBranch: defaultBranch,
	}
}

type adoPushPayload struct {
	Resource struct {
		RefUpdates []adoRefUpdate `json:"refUpdates"`
		Repository adoRepository  `json:"repository"`
		PushedBy   struct {
			DisplayName string `json:"displayName"`
		} `json:"pushedBy"`
	} `json:"resource"`
}

type adoRefUpdate struct {
	Name        string `json:"name"`
	OldObjectID string `json:"oldObjectId"`
	NewObjectID string `json:"newObjectId"`
}

type adoPullRequestPayload struct {
	Resource struct {
		Status          string        `json:"status"`
		MergeStatus     string        `json:"mergeStatus"`
		TargetRefName   string        `json:"targetRefName"`
		LastMergeCommit *adoCommit    `json:"lastMergeCommit"`
		Repository      adoRepository `json:"repository"`
		CreatedBy       struct {
			DisplayName string `json:"displayName"`
		} `json:"createdBy"`
		PullRequestID int `json:"pullRequestId"`
	} `json:"resource"`
}

type adoCommit struct {
	CommitID string `json:"commitId"`
}
