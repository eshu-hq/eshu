// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"strings"
)

// StoryContainerImageCandidateState carries the OCI collector target
// state behind a deployment image candidate explanation. The implementation
// moved from root's container_image_candidate_explanation.go for #6060 so a
// handler-family subpackage can explain a candidate without importing root.
type StoryContainerImageCandidateState struct {
	ScopeID          string
	ScopeStatus      string
	GenerationID     string
	GenerationStatus string
	WorkStatus       string
	FailureClass     string
	WarningCode      string
	WarningDigest    string
}

// StoryContainerImageCandidateReason explains why a deployment image
// candidate has no canonical container image identity, returning the
// machine-readable reason, the collector scope, and the operator action. The
// implementation moved from root's container_image_candidate_explanation.go
// for #6060; see StoryContainerImageCandidateState.
func StoryContainerImageCandidateReason(
	repositoryID string,
	state StoryContainerImageCandidateState,
) (string, string, string) {
	if state.ScopeID == "" && state.WorkStatus == "" && state.WarningCode == "" {
		return "oci_registry_target_outside_scope",
			"outside_configured_targets",
			"add an OCI registry collector target for " + repositoryID
	}
	if StoryContainerImageCandidateWorkFailed(state.WorkStatus) {
		return "oci_registry_target_unreadable",
			"configured_unreadable",
			StoryUnreadableOCIRegistryAction(repositoryID, state.FailureClass)
	}
	if state.GenerationStatus == "failed" || state.ScopeStatus == "failed" {
		return "oci_registry_target_unreadable",
			"configured_unreadable",
			StoryUnreadableOCIRegistryAction(repositoryID, state.FailureClass)
	}
	if StoryContainerImageCandidateWorkPending(state.WorkStatus) ||
		state.GenerationStatus == "pending" ||
		state.ScopeStatus == "pending" {
		return "oci_registry_target_collection_pending",
			"configured_pending",
			"wait for or run the configured OCI registry collector target for " + repositoryID
	}
	if state.ScopeID != "" && state.GenerationID == "" && state.WorkStatus == "" && state.WarningCode == "" {
		return "oci_registry_target_collection_pending",
			"configured_pending",
			"wait for or run the configured OCI registry collector target for " + repositoryID
	}
	if state.FailureClass != "" && state.WorkStatus != "completed" {
		return "oci_registry_target_unreadable",
			"configured_unreadable",
			StoryUnreadableOCIRegistryAction(repositoryID, state.FailureClass)
	}
	if state.ScopeID != "" || state.WorkStatus == "completed" || state.WarningCode != "" {
		return "container_image_identity_scanned_missing",
			"configured_scanned",
			"verify the configured OCI registry collector scans the candidate tag or digest for " + repositoryID
	}
	return "container_image_identity_missing",
		"unknown",
		"verify OCI registry collector coverage and reducer image identity facts for this deployment image reference"
}

// StoryContainerImageCandidateWorkFailed reports whether an OCI
// collector work status is a failure state. See
// StoryContainerImageCandidateState for the move note.
func StoryContainerImageCandidateWorkFailed(status string) bool {
	switch strings.TrimSpace(status) {
	case "failed_retryable", "failed_terminal":
		return true
	default:
		return false
	}
}

// StoryContainerImageCandidateWorkPending reports whether an OCI
// collector work status is a pending state. See
// StoryContainerImageCandidateState for the move note.
func StoryContainerImageCandidateWorkPending(status string) bool {
	switch strings.TrimSpace(status) {
	case "pending", "claimed", "expired":
		return true
	default:
		return false
	}
}

// StoryUnreadableOCIRegistryAction builds the operator action for an
// unreadable OCI registry collector target. See
// StoryContainerImageCandidateState for the move note.
func StoryUnreadableOCIRegistryAction(repositoryID string, failureClass string) string {
	action := "fix the configured OCI registry collector target for " + repositoryID
	if failureClass = strings.TrimSpace(failureClass); failureClass != "" {
		action += "; current failure class is " + failureClass
	}
	return action
}
