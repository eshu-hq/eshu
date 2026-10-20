// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package controltower

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// organizationsTarget is the resolved Organizations join key for a Control Tower
// target ARN: the bare id the organizations scanner publishes as a resource_id
// plus the declared awscloud.ResourceType* constant that node carries.
type organizationsTarget struct {
	// ResourceID is the bare Organizations id (ou-…, a 12-digit account id, or
	// r-…) parsed from the target ARN. It matches the organizations scanner's
	// published resource_id so the edge joins the node instead of dangling.
	ResourceID string
	// ResourceType is the declared awscloud.ResourceType* constant for the
	// resolved id family.
	ResourceType string
	// ARN is the original Control Tower target ARN, preserved for edge provenance.
	ARN string
}

// resolveOrganizationsTarget parses a Control Tower target ARN into the bare
// Organizations id and resource type the organizations scanner publishes. AWS
// Control Tower reports the target as an Organizations ARN, for example
// arn:<partition>:organizations::<mgmt>:ou/o-<org>/ou-<id>, whose last
// slash-separated segment is the bare id the organizations scanner keys its
// node on (ou-… / account id / r-…). The resource segment (ou, account, root)
// selects the target type. It returns ok=false when the ARN is empty, is not an
// Organizations ARN, or names a family the organizations scanner does not
// publish, so the caller skips the edge rather than dangling it.
func resolveOrganizationsTarget(targetARN string) (organizationsTarget, bool) {
	trimmed := strings.TrimSpace(targetARN)
	if trimmed == "" || !strings.HasPrefix(trimmed, "arn:") {
		return organizationsTarget{}, false
	}
	// ARN layout: arn:<partition>:organizations::<account>:<resource-segment>.
	parts := strings.SplitN(trimmed, ":", 6)
	if len(parts) < 6 || strings.TrimSpace(parts[2]) != "organizations" {
		return organizationsTarget{}, false
	}
	resourceSegment := strings.TrimSpace(parts[5])
	slash := strings.Index(resourceSegment, "/")
	if slash <= 0 {
		return organizationsTarget{}, false
	}
	family := resourceSegment[:slash]
	bareID := resourceSegment[strings.LastIndex(resourceSegment, "/")+1:]
	bareID = strings.TrimSpace(bareID)
	if bareID == "" {
		return organizationsTarget{}, false
	}
	resourceType, ok := organizationsResourceType(family)
	if !ok {
		return organizationsTarget{}, false
	}
	return organizationsTarget{
		ResourceID:   bareID,
		ResourceType: resourceType,
		ARN:          trimmed,
	}, true
}

// organizationsResourceType maps an Organizations ARN resource family (ou,
// account, root) to the declared awscloud.ResourceType* constant the
// organizations scanner publishes. It returns ok=false for any other family so
// the caller skips the edge instead of keying an unknown target type.
func organizationsResourceType(family string) (string, bool) {
	switch strings.TrimSpace(family) {
	case "ou":
		return awscloud.ResourceTypeOrganizationsOrganizationalUnit, true
	case "account":
		return awscloud.ResourceTypeOrganizationsAccount, true
	case "root":
		return awscloud.ResourceTypeOrganizationsRoot, true
	default:
		return "", false
	}
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
