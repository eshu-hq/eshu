// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	buildkiteEventBuildFinished = "build.finished"
	buildkiteEventBuildRunning  = "build.running"
)

// NormalizeBuildkite converts a verified Buildkite event payload into a
// refresh trigger decision. Buildkite is CI-only so the pipeline slug is used
// as both the repository full name and external ID.
func NormalizeBuildkite(event string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	switch strings.TrimSpace(event) {
	case buildkiteEventBuildFinished, buildkiteEventBuildRunning:
		return normalizeBuildkiteBuild(deliveryID, payload, defaultBranchFallback)
	default:
		return Trigger{}, fmt.Errorf("unsupported buildkite webhook event %q", event)
	}
}

func normalizeBuildkiteBuild(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var parsed buildkitePayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Trigger{}, err
	}

	slug := strings.TrimSpace(parsed.Pipeline.Slug)
	repo := repositoryFields{
		externalID:    slug,
		fullName:      slug,
		defaultBranch: defaultBranchFallback,
	}

	trigger, err := newTrigger(ProviderBuildkite, EventKindPush, deliveryID, repo)
	if err != nil {
		return Trigger{}, err
	}
	trigger.Ref = branchRef(strings.TrimSpace(parsed.Build.Branch))
	trigger.TargetSHA = strings.TrimSpace(parsed.Build.Commit)
	trigger.Sender = strings.TrimSpace(parsed.Sender.Name)

	return decideBranchTrigger(trigger)
}

type buildkitePayload struct {
	Build struct {
		Commit string `json:"commit"`
		Branch string `json:"branch"`
	} `json:"build"`
	Pipeline struct {
		Slug string `json:"slug"`
	} `json:"pipeline"`
	Sender struct {
		Name string `json:"name"`
	} `json:"sender"`
}
