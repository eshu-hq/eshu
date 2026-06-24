// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rules

// ContainerRulePacks returns the first-party rule packs for the initial
// container correlation slice.
func ContainerRulePacks() []RulePack {
	return []RulePack{
		DockerfileRulePack(),
		DockerComposeRulePack(),
		GitHubActionsRulePack(),
		JenkinsRulePack(),
		HelmRulePack(),
		ArgoCDRulePack(),
		KustomizeRulePack(),
		TerraformConfigRulePack(),
		CloudFormationRulePack(),
	}
}

// FirstPartyRulePacks returns the shipped rule packs for every currently
// supported evidence family in the correlation layer.
func FirstPartyRulePacks() []RulePack {
	return []RulePack{
		DockerfileRulePack(),
		DockerComposeRulePack(),
		GitHubActionsRulePack(),
		JenkinsRulePack(),
		HelmRulePack(),
		ArgoCDRulePack(),
		KustomizeRulePack(),
		TerraformConfigRulePack(),
		TerragruntRulePack(),
		AnsibleRulePack(),
		CloudFormationRulePack(),
		TerraformConfigStateDriftRulePack(),
		AWSCloudRuntimeDriftRulePack(),
		MultiCloudRuntimeDriftRulePack(),
	}
}
