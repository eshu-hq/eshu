// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const circleCIEventWorkflowCompleted = "workflow-completed"

// NormalizeCircleCI converts a verified CircleCI event payload into a refresh
// trigger decision. CircleCI is CI-only so the repository full name is used as
// the external ID.
func NormalizeCircleCI(event string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	if strings.TrimSpace(event) != circleCIEventWorkflowCompleted {
		return Trigger{}, fmt.Errorf("unsupported circleci webhook event %q", event)
	}

	var parsed circleCIPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Trigger{}, err
	}

	repo := parsed.repositoryFields(defaultBranchFallback)

	trigger, err := newTrigger(ProviderCircleCI, EventKindPush, deliveryID, repo)
	if err != nil {
		return Trigger{}, err
	}
	trigger.TargetSHA = strings.TrimSpace(parsed.Pipeline.VCS.Revision)

	if tag := strings.TrimSpace(parsed.Pipeline.VCS.Tag); tag != "" {
		trigger.Ref = "refs/tags/" + tag
	} else {
		trigger.Ref = branchRef(strings.TrimSpace(parsed.Pipeline.VCS.Branch))
	}

	return decideBranchTrigger(trigger)
}

type circleCIPayload struct {
	Pipeline struct {
		VCS struct {
			Revision            string `json:"revision"`
			Branch              string `json:"branch"`
			Tag                 string `json:"tag"`
			OriginRepositoryURL string `json:"origin_repository_url"`
			ProviderName        string `json:"provider_name"`
		} `json:"vcs"`
	} `json:"pipeline"`
	Project struct {
		Name string `json:"name"`
	} `json:"project"`
}

func (p circleCIPayload) repositoryFields(defaultBranchFallback string) repositoryFields {
	fullName := extractFullNameFromURL(strings.TrimSpace(p.Pipeline.VCS.OriginRepositoryURL))
	if fullName == "" {
		fullName = strings.TrimSpace(p.Project.Name)
	}
	return repositoryFields{
		externalID:    fullName,
		fullName:      fullName,
		defaultBranch: defaultBranchFallback,
	}
}

// extractFullNameFromURL extracts "org/repo" from a git repository URL
// (HTTPS, git://, or SSH-style git@host:org/repo.git).
func extractFullNameFromURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	url = strings.TrimSuffix(url, ".git")

	if strings.Contains(url, "@") && strings.Contains(url, ":") {
		if idx := strings.LastIndex(url, ":"); idx >= 0 {
			url = url[idx+1:]
		}
	}

	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		remainder := url[:idx]
		if idx2 := strings.LastIndex(remainder, "/"); idx2 >= 0 {
			org := remainder[idx2+1:]
			repo := url[idx+1:]
			if org != "" && repo != "" {
				return org + "/" + repo
			}
		}
	}
	return ""
}
