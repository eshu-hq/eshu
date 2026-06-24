// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsramtypes "github.com/aws/aws-sdk-go-v2/service/ram/types"

	ramservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ram"
)

// mapResourceshare converts one SDK resource share summary into the
// scanner-owned metadata record. Associated resources, principals, and
// permissions are attached separately by the adapter after dedicated reads.
func mapResourceShare(share awsramtypes.ResourceShare) ramservice.ResourceShare {
	return ramservice.ResourceShare{
		ARN:                     strings.TrimSpace(aws.ToString(share.ResourceShareArn)),
		Name:                    strings.TrimSpace(aws.ToString(share.Name)),
		Status:                  strings.TrimSpace(string(share.Status)),
		StatusMessage:           strings.TrimSpace(aws.ToString(share.StatusMessage)),
		OwningAccountID:         strings.TrimSpace(aws.ToString(share.OwningAccountId)),
		AllowExternalPrincipals: aws.ToBool(share.AllowExternalPrincipals),
		FeatureSet:              strings.TrimSpace(string(share.FeatureSet)),
		CreationTime:            timeOrZero(share.CreationTime),
		LastUpdatedTime:         timeOrZero(share.LastUpdatedTime),
		Tags:                    mapTags(share.Tags),
	}
}

func mapSharedResource(resource awsramtypes.Resource) ramservice.SharedResource {
	return ramservice.SharedResource{
		ARN:         strings.TrimSpace(aws.ToString(resource.Arn)),
		Type:        strings.TrimSpace(aws.ToString(resource.Type)),
		Status:      strings.TrimSpace(string(resource.Status)),
		RegionScope: strings.TrimSpace(string(resource.ResourceRegionScope)),
	}
}

func mapPrincipal(principal awsramtypes.Principal) ramservice.Principal {
	return ramservice.Principal{
		ID:       strings.TrimSpace(aws.ToString(principal.Id)),
		External: aws.ToBool(principal.External),
	}
}

// mapPermission converts one SDK permission summary into the scanner-owned
// record. The summary carries no policy document body, so a leak is structurally
// impossible: only name, ARN, version, type, and status are copied.
func mapPermission(permission awsramtypes.ResourceSharePermissionSummary) ramservice.Permission {
	return ramservice.Permission{
		ARN:            strings.TrimSpace(aws.ToString(permission.Arn)),
		Name:           strings.TrimSpace(aws.ToString(permission.Name)),
		Version:        strings.TrimSpace(aws.ToString(permission.Version)),
		PermissionType: strings.TrimSpace(string(permission.PermissionType)),
		ResourceType:   strings.TrimSpace(aws.ToString(permission.ResourceType)),
		Status:         strings.TrimSpace(aws.ToString(permission.Status)),
		DefaultVersion: aws.ToBool(permission.DefaultVersion),
	}
}

func mapTags(tags []awsramtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
