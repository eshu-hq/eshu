// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeguru

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// associationResourceID returns the resource_id a repository-association node
// publishes. It prefers the association ARN (always present from the Reviewer
// API) and falls back to the association id, so an association's own edges are
// sourced on the same id the node publishes.
func associationResourceID(association RepositoryAssociation) string {
	return firstNonEmpty(association.ARN, association.AssociationID, association.Name)
}

// profilingGroupResourceID returns the resource_id a profiling-group node
// publishes. It prefers the group ARN and falls back to the group name.
func profilingGroupResourceID(group ProfilingGroup) string {
	return firstNonEmpty(group.ARN, group.Name)
}

// codeCommitRepositoryARN synthesizes the partition-aware CodeCommit repository
// ARN (arn:<partition>:codecommit:<region>:<account>:<name>) that matches how
// the CodeCommit scanner publishes its repository resource_id. CodeGuru Reviewer
// reports only the repository name and the owning account for a CodeCommit
// association, so the ARN is synthesized from the scan boundary region and the
// reported owner account, carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the edge joins the real repository node in every partition. It
// returns "" when the repository name, owning account, or region is missing,
// which keeps the edge from dangling.
func codeCommitRepositoryARN(boundary awscloud.Boundary, owner, name string) string {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	region := strings.TrimSpace(boundary.Region)
	if owner == "" || name == "" || region == "" {
		return ""
	}
	partition := awscloud.PartitionForBoundary(boundary)
	return "arn:" + partition + ":codecommit:" + region + ":" + owner + ":" + name
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// boolOrNil returns value when set, or nil so the attribute payload omits an
// unreported profiling-enabled posture instead of asserting a false default.
func boolOrNil(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
