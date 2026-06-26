// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	droneEventBuildSuccess = "build.success"
	droneEventBuildFailure = "build.failure"
)

// NormalizeDrone converts a verified Drone CI event payload into a refresh
// trigger decision. Drone is CI-only so the repo slug is used as both the
// repository full name and external ID.
//
// For push events the branch and after-SHA are used directly. For pull request
// events the target branch is treated as the default branch and the after-SHA
// is the merged commit.
func NormalizeDrone(event string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	switch strings.TrimSpace(event) {
	case droneEventBuildSuccess, droneEventBuildFailure:
		return normalizeDroneBuild(deliveryID, payload, defaultBranchFallback)
	default:
		return Trigger{}, fmt.Errorf("unsupported drone webhook event %q", event)
	}
}

func normalizeDroneBuild(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var parsed dronePayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Trigger{}, err
	}

	slug := strings.TrimSpace(parsed.Repo.Slug)
	repo := repositoryFields{
		externalID:    slug,
		fullName:      slug,
		defaultBranch: defaultBranchFallback,
	}

	trigger, err := newTrigger(ProviderDrone, EventKindPush, deliveryID, repo)
	if err != nil {
		return Trigger{}, err
	}

	buildEvent := strings.TrimSpace(parsed.Build.Event)
	switch buildEvent {
	case "push":
		trigger.Ref = branchRef(strings.TrimSpace(parsed.Build.Branch))
		trigger.TargetSHA = strings.TrimSpace(parsed.Build.After)
	case "pull_request":
		targetBranch := strings.TrimSpace(parsed.Build.Target)
		trigger.Ref = branchRef(targetBranch)
		trigger.TargetSHA = strings.TrimSpace(parsed.Build.After)
	default:
		return Trigger{}, fmt.Errorf("unsupported drone build event type %q", buildEvent)
	}
	trigger.BeforeSHA = strings.TrimSpace(parsed.Build.Before)
	trigger.Sender = strings.TrimSpace(parsed.Sender.Login)

	return decideBranchTrigger(trigger)
}

type dronePayload struct {
	Build struct {
		After  string `json:"after"`
		Before string `json:"before"`
		Branch string `json:"branch"`
		Source string `json:"source"`
		Target string `json:"target"`
		Link   string `json:"link"`
		Number int64  `json:"number"`
		Event  string `json:"event"`
	} `json:"build"`
	Repo struct {
		Slug string `json:"slug"`
	} `json:"repo"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}
