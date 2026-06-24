// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import "testing"

func TestLoadConfigAcceptsDisabledHostedCollectorsWithBlankTargetFields(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[
				{
					"instance_id":"collector-git-primary",
					"collector_kind":"git",
					"mode":"continuous",
					"enabled":true,
					"claims_enabled":true,
					"configuration":{"provider":"github"}
				},
				{
					"instance_id":"pagerduty-optional",
					"collector_kind":"pagerduty",
					"mode":"continuous",
					"enabled":false,
					"claims_enabled":true,
					"configuration":{"targets":[{}]}
				},
				{
					"instance_id":"jira-optional",
					"collector_kind":"jira",
					"mode":"continuous",
					"enabled":false,
					"claims_enabled":true,
					"configuration":{"targets":[{}]}
				}
			]`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
}

func TestLoadConfigAcceptsDisabledHostedClaimsWhenCoordinatorClaimsDisabled(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "dark"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "false"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[
				{
					"instance_id":"pagerduty-optional",
					"collector_kind":"pagerduty",
					"mode":"continuous",
					"enabled":false,
					"claims_enabled":true,
					"configuration":{"targets":[{}]}
				}
			]`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
}
