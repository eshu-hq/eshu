// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdstypes "github.com/aws/aws-sdk-go-v2/service/directoryservice/types"

	dsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ds"
)

// mapDirectory converts an AWS DirectoryDescription into the scanner-owned
// Directory. It reads the VPC placement from VpcSettings (Simple AD and Managed
// Microsoft AD) or ConnectSettings (AD Connector). It never maps the directory
// admin password (not returned by DescribeDirectories), the RADIUS shared secret
// (RadiusSettings is not read), or the AD Connector service-account user name.
func mapDirectory(raw awsdstypes.DirectoryDescription, ldapsStatuses []string, tags map[string]string) dsservice.Directory {
	directory := dsservice.Directory{
		ID:            aws.ToString(raw.DirectoryId),
		Name:          aws.ToString(raw.Name),
		ShortName:     aws.ToString(raw.ShortName),
		Type:          string(raw.Type),
		Edition:       string(raw.Edition),
		Size:          string(raw.Size),
		Stage:         string(raw.Stage),
		Description:   aws.ToString(raw.Description),
		AccessURL:     aws.ToString(raw.AccessUrl),
		Alias:         aws.ToString(raw.Alias),
		LDAPSStatuses: ldapsStatuses,
		ShareMethod:   string(raw.ShareMethod),
		ShareStatus:   string(raw.ShareStatus),
		SsoEnabled:    raw.SsoEnabled,
		Tags:          tags,
	}
	if settings := raw.VpcSettings; settings != nil {
		directory.VPCID = aws.ToString(settings.VpcId)
		directory.SubnetIDs = cloneStrings(settings.SubnetIds)
		directory.SecurityGroupID = aws.ToString(settings.SecurityGroupId)
		directory.AvailabilityZones = cloneStrings(settings.AvailabilityZones)
	}
	// AD Connector reports VPC placement under ConnectSettings instead of
	// VpcSettings. The CustomerUserName service-account field on ConnectSettings is
	// intentionally not read.
	if settings := raw.ConnectSettings; settings != nil {
		if directory.VPCID == "" {
			directory.VPCID = aws.ToString(settings.VpcId)
		}
		if len(directory.SubnetIDs) == 0 {
			directory.SubnetIDs = cloneStrings(settings.SubnetIds)
		}
		if directory.SecurityGroupID == "" {
			directory.SecurityGroupID = aws.ToString(settings.SecurityGroupId)
		}
		if len(directory.AvailabilityZones) == 0 {
			directory.AvailabilityZones = cloneStrings(settings.AvailabilityZones)
		}
	}
	return directory
}

func mapTrust(raw awsdstypes.Trust) dsservice.Trust {
	return dsservice.Trust{
		ID:               aws.ToString(raw.TrustId),
		DirectoryID:      aws.ToString(raw.DirectoryId),
		RemoteDomainName: aws.ToString(raw.RemoteDomainName),
		Direction:        string(raw.TrustDirection),
		Type:             string(raw.TrustType),
		State:            string(raw.TrustState),
		SelectiveAuth:    string(raw.SelectiveAuth),
	}
}

func mapSharedDirectory(raw awsdstypes.SharedDirectory) dsservice.SharedDirectory {
	return dsservice.SharedDirectory{
		OwnerAccountID:    aws.ToString(raw.OwnerAccountId),
		OwnerDirectoryID:  aws.ToString(raw.OwnerDirectoryId),
		SharedAccountID:   aws.ToString(raw.SharedAccountId),
		SharedDirectoryID: aws.ToString(raw.SharedDirectoryId),
		ShareMethod:       string(raw.ShareMethod),
		ShareStatus:       string(raw.ShareStatus),
	}
}

func mapLDAPSSetting(raw awsdstypes.LDAPSSettingInfo) dsservice.LDAPSSetting {
	return dsservice.LDAPSSetting{
		Status: string(raw.LDAPSStatus),
	}
}

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
