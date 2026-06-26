// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	jenkinsEventPush  = "push"
	jenkinsEventMerge = "merge"
)

// NormalizeJenkins converts a verified Jenkins Generic Webhook Trigger event
// payload into a refresh trigger decision.
func NormalizeJenkins(event string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	switch strings.TrimSpace(event) {
	case jenkinsEventPush, jenkinsEventMerge:
		return normalizeJenkinsPush(deliveryID, payload, defaultBranchFallback)
	default:
		return Trigger{}, fmt.Errorf("unsupported jenkins webhook event %q", event)
	}
}

func normalizeJenkinsPush(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event jenkinsPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderJenkins, EventKindPush, deliveryID, event.Repository.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	trigger.Ref = jenkinsRef(event)
	trigger.BeforeSHA = strings.TrimSpace(event.Before)
	trigger.TargetSHA = jenkinsTargetSHA(event)
	trigger.Sender = jenkinsSender(event)

	return decideBranchTrigger(trigger)
}

func jenkinsRef(event jenkinsPayload) string {
	if ref := strings.TrimSpace(event.Ref); ref != "" {
		return ref
	}
	if branch := strings.TrimSpace(event.GitBranch); branch != "" {
		if !strings.HasPrefix(branch, "refs/") {
			return branchRef(branch)
		}
		return branch
	}
	return ""
}

func jenkinsTargetSHA(event jenkinsPayload) string {
	return firstNonEmpty(event.After, event.GitCommit, event.Commit)
}

func jenkinsSender(event jenkinsPayload) string {
	if sender := strings.TrimSpace(event.Pusher.Name); sender != "" {
		return sender
	}
	return strings.TrimSpace(event.GitAuthor)
}

type jenkinsRepository struct {
	ID       json.RawMessage `json:"id"`
	FullName string          `json:"full_name"`
	URL      string          `json:"url"`
}

func (repo jenkinsRepository) common(defaultBranchFallback string) repositoryFields {
	externalID := rawScalar(repo.ID)
	if externalID == "" {
		externalID = strings.TrimSpace(repo.FullName)
	}
	fullName := strings.TrimSpace(repo.FullName)
	if fullName == "" {
		fullName = repoFullNameFromURL(repo.URL)
	}
	return repositoryFields{
		externalID:    externalID,
		fullName:      fullName,
		defaultBranch: defaultBranchFallback,
	}
}

func repoFullNameFromURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")
	if idx := strings.LastIndex(url, ":"); idx != -1 {
		url = url[idx+1:]
	}
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return ""
}

type jenkinsPayload struct {
	Ref        string            `json:"ref"`
	GitBranch  string            `json:"GIT_BRANCH"`
	Before     string            `json:"before"`
	After      string            `json:"after"`
	GitCommit  string            `json:"GIT_COMMIT"`
	Commit     string            `json:"commit"`
	Repository jenkinsRepository `json:"repository"`
	Pusher     struct {
		Name string `json:"name"`
	} `json:"pusher"`
	GitAuthor string `json:"GIT_AUTHOR"`
}
