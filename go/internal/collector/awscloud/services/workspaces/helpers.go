// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workspaces

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// workspaceResourceID returns the resource_id the WorkSpace node publishes. The
// DescribeWorkspaces API does not return an ARN, so the scanner synthesizes a
// partition-aware WorkSpaces ARN from the scan boundary and falls back to the
// bare WorkSpace id when identity is incomplete.
func workspaceResourceID(boundary awscloud.Boundary, workspace Workspace) string {
	id := strings.TrimSpace(workspace.ID)
	if id == "" {
		return ""
	}
	if arn := workspacesARN(boundary, "workspace", id); arn != "" {
		return arn
	}
	return id
}

// directoryResourceID returns the resource_id the WorkSpaces directory node
// publishes. DescribeWorkspaceDirectories returns no ARN, so the scanner
// synthesizes a partition-aware WorkSpaces directory ARN and falls back to the
// bare directory id. The bare directory id is also what the Directory Service
// scanner publishes, which the directory-to-DS edge keys on separately.
func directoryResourceID(boundary awscloud.Boundary, directory Directory) string {
	id := strings.TrimSpace(directory.ID)
	if id == "" {
		return ""
	}
	if arn := workspacesARN(boundary, "directory", id); arn != "" {
		return arn
	}
	return id
}

// bundleResourceID returns the resource_id the WorkSpaces bundle node
// publishes. DescribeWorkspaceBundles returns no ARN, so the scanner
// synthesizes a partition-aware WorkSpaces bundle ARN and falls back to the
// bare bundle id.
func bundleResourceID(boundary awscloud.Boundary, bundle Bundle) string {
	id := strings.TrimSpace(bundle.ID)
	if id == "" {
		return ""
	}
	if arn := workspacesARN(boundary, "workspacebundle", id); arn != "" {
		return arn
	}
	return id
}

// ipGroupResourceID returns the resource_id the WorkSpaces IP access control
// group node publishes. DescribeIpGroups returns no ARN, so the scanner
// synthesizes a partition-aware WorkSpaces IP-group ARN and falls back to the
// bare group id.
func ipGroupResourceID(boundary awscloud.Boundary, group IPGroup) string {
	id := strings.TrimSpace(group.ID)
	if id == "" {
		return ""
	}
	if arn := workspacesARN(boundary, "workspaceipgroup", id); arn != "" {
		return arn
	}
	return id
}

// workspacesARN synthesizes a partition-aware Amazon WorkSpaces ARN of the form
// arn:<partition>:workspaces:<region>:<account>:<resource>/<id>. WorkSpaces
// describe APIs return no ARNs, so the scanner derives the partition from the
// scan boundary (aws / aws-cn / aws-us-gov) rather than hardcoding arn:aws: so
// the published ids join in every partition. It returns "" when the account or
// region needed to form a valid ARN is missing, so the caller falls back to the
// bare id instead of emitting a malformed ARN.
func workspacesARN(boundary awscloud.Boundary, resource, id string) string {
	account := strings.TrimSpace(boundary.AccountID)
	region := strings.TrimSpace(boundary.Region)
	resource = strings.TrimSpace(resource)
	id = strings.TrimSpace(id)
	if account == "" || region == "" || resource == "" || id == "" {
		return ""
	}
	partition := awscloud.PartitionForBoundary(boundary)
	return strings.Join(
		[]string{"arn", partition, "workspaces", region, account, resource + "/" + id},
		":",
	)
}

// isARN reports whether value carries the canonical AWS ARN prefix. The scanner
// uses it to decide whether a reported KMS or IAM reference is ARN-keyed (so
// the edge carries a target_arn) or a bare id (so it does not get a fabricated
// ARN).
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
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

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// ruleAttributes projects the IP access control group rules into a payload-safe
// slice of maps carrying only the CIDR and optional description, or nil when no
// rule survives trimming.
func ruleAttributes(rules []IPRule) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		cidr := strings.TrimSpace(rule.CIDR)
		if cidr == "" {
			continue
		}
		entry := map[string]any{"cidr": cidr}
		if desc := strings.TrimSpace(rule.Description); desc != "" {
			entry["description"] = desc
		}
		output = append(output, entry)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
