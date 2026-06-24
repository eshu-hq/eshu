// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsworkspacestypes "github.com/aws/aws-sdk-go-v2/service/workspaces/types"

	workspacesservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/workspaces"
)

// mapWorkspace maps an SDK WorkSpace into scanner-owned metadata. It copies only
// identity, the directory/bundle references, the operational state, the
// (identity-metadata) computer/user names, and the volume encryption
// configuration. It never copies the WorkSpace IP addresses, error message text,
// modification states, or any session detail.
func (c *Client) mapWorkspace(
	ctx context.Context,
	workspace awsworkspacestypes.Workspace,
) (workspacesservice.Workspace, error) {
	id := strings.TrimSpace(aws.ToString(workspace.WorkspaceId))
	tags, err := c.listTags(ctx, id)
	if err != nil {
		return workspacesservice.Workspace{}, err
	}
	return workspacesservice.Workspace{
		ID:                          id,
		Name:                        strings.TrimSpace(aws.ToString(workspace.WorkspaceName)),
		DirectoryID:                 strings.TrimSpace(aws.ToString(workspace.DirectoryId)),
		BundleID:                    strings.TrimSpace(aws.ToString(workspace.BundleId)),
		State:                       strings.TrimSpace(string(workspace.State)),
		ComputerName:                strings.TrimSpace(aws.ToString(workspace.ComputerName)),
		UserName:                    strings.TrimSpace(aws.ToString(workspace.UserName)),
		VolumeEncryptionKey:         strings.TrimSpace(aws.ToString(workspace.VolumeEncryptionKey)),
		RootVolumeEncryptionEnabled: aws.ToBool(workspace.RootVolumeEncryptionEnabled),
		UserVolumeEncryptionEnabled: aws.ToBool(workspace.UserVolumeEncryptionEnabled),
		Tags:                        tags,
	}, nil
}

// mapDirectory maps an SDK WorkSpaceDirectory into scanner-owned metadata. It
// copies the registration metadata and the network placement references and
// intentionally omits the registration code, the service-account user name, the
// DNS server addresses, and the SAML/Entra/IDC federation configuration.
func (c *Client) mapDirectory(
	ctx context.Context,
	directory awsworkspacestypes.WorkspaceDirectory,
) (workspacesservice.Directory, error) {
	id := strings.TrimSpace(aws.ToString(directory.DirectoryId))
	tags, err := c.listTags(ctx, id)
	if err != nil {
		return workspacesservice.Directory{}, err
	}
	return workspacesservice.Directory{
		ID:                       id,
		Name:                     strings.TrimSpace(aws.ToString(directory.DirectoryName)),
		Alias:                    strings.TrimSpace(aws.ToString(directory.Alias)),
		State:                    strings.TrimSpace(string(directory.State)),
		DirectoryType:            strings.TrimSpace(string(directory.DirectoryType)),
		Tenancy:                  strings.TrimSpace(string(directory.Tenancy)),
		IamRoleID:                strings.TrimSpace(aws.ToString(directory.IamRoleId)),
		WorkspaceSecurityGroupID: strings.TrimSpace(aws.ToString(directory.WorkspaceSecurityGroupId)),
		SubnetIDs:                trimmedStrings(directory.SubnetIds),
		IPGroupIDs:               trimmedStrings(directory.IpGroupIds),
		Tags:                     tags,
	}, nil
}

// mapBundle maps an SDK WorkSpaceBundle into scanner-owned metadata, copying the
// owner, type, compute type, volume sizes, backing image, and lifecycle
// timestamps only.
func (c *Client) mapBundle(
	ctx context.Context,
	bundle awsworkspacestypes.WorkspaceBundle,
) (workspacesservice.Bundle, error) {
	id := strings.TrimSpace(aws.ToString(bundle.BundleId))
	tags, err := c.listTags(ctx, id)
	if err != nil {
		return workspacesservice.Bundle{}, err
	}
	mapped := workspacesservice.Bundle{
		ID:              id,
		Name:            strings.TrimSpace(aws.ToString(bundle.Name)),
		Description:     strings.TrimSpace(aws.ToString(bundle.Description)),
		Owner:           strings.TrimSpace(aws.ToString(bundle.Owner)),
		BundleType:      strings.TrimSpace(string(bundle.BundleType)),
		ImageID:         strings.TrimSpace(aws.ToString(bundle.ImageId)),
		State:           strings.TrimSpace(string(bundle.State)),
		CreationTime:    aws.ToTime(bundle.CreationTime),
		LastUpdatedTime: aws.ToTime(bundle.LastUpdatedTime),
		Tags:            tags,
	}
	if bundle.ComputeType != nil {
		mapped.ComputeType = strings.TrimSpace(string(bundle.ComputeType.Name))
	}
	if bundle.RootStorage != nil {
		mapped.RootVolumeSizeGib = strings.TrimSpace(aws.ToString(bundle.RootStorage.Capacity))
	}
	if bundle.UserStorage != nil {
		mapped.UserVolumeSizeGib = strings.TrimSpace(aws.ToString(bundle.UserStorage.Capacity))
	}
	return mapped, nil
}

// mapIPGroup maps an SDK WorkspacesIpGroup into scanner-owned metadata, copying
// the identity, description, and CIDR access rules (network configuration, not
// secret material).
func (c *Client) mapIPGroup(
	ctx context.Context,
	group awsworkspacestypes.WorkspacesIpGroup,
) (workspacesservice.IPGroup, error) {
	id := strings.TrimSpace(aws.ToString(group.GroupId))
	tags, err := c.listTags(ctx, id)
	if err != nil {
		return workspacesservice.IPGroup{}, err
	}
	return workspacesservice.IPGroup{
		ID:          id,
		Name:        strings.TrimSpace(aws.ToString(group.GroupName)),
		Description: strings.TrimSpace(aws.ToString(group.GroupDesc)),
		Rules:       mapIPRules(group.UserRules),
		Tags:        tags,
	}, nil
}

// mapIPRules projects the SDK IP rules into scanner-owned CIDR entries, dropping
// any rule with no CIDR.
func mapIPRules(rules []awsworkspacestypes.IpRuleItem) []workspacesservice.IPRule {
	if len(rules) == 0 {
		return nil
	}
	output := make([]workspacesservice.IPRule, 0, len(rules))
	for _, rule := range rules {
		cidr := strings.TrimSpace(aws.ToString(rule.IpRule))
		if cidr == "" {
			continue
		}
		output = append(output, workspacesservice.IPRule{
			CIDR:        cidr,
			Description: strings.TrimSpace(aws.ToString(rule.RuleDesc)),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// trimmedStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func trimmedStrings(input []string) []string {
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
