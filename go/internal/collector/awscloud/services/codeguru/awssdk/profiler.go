// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsprofiler "github.com/aws/aws-sdk-go-v2/service/codeguruprofiler"
	profilertypes "github.com/aws/aws-sdk-go-v2/service/codeguruprofiler/types"

	codeguruservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codeguru"
)

// listProfilingGroups pages every CodeGuru Profiler profiling group to
// exhaustion. It requests the full description inline (IncludeDescription) so no
// per-group DescribeProfilingGroup round trip is needed, and it never requests
// profiling samples, findings, or frame metrics.
func (c *Client) listProfilingGroups(ctx context.Context) ([]codeguruservice.ProfilingGroup, error) {
	var groups []codeguruservice.ProfilingGroup
	var nextToken *string
	for {
		var page *awsprofiler.ListProfilingGroupsOutput
		err := c.recordAPICall(ctx, "ListProfilingGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.profiler.ListProfilingGroups(callCtx, &awsprofiler.ListProfilingGroupsInput{
				IncludeDescription: aws.Bool(true),
				NextToken:          nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, description := range page.ProfilingGroups {
			groups = append(groups, mapProfilingGroup(description))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return groups, nil
		}
	}
}

// mapProfilingGroup maps a profiling group description into the scanner-owned
// model. It records identity, compute platform, the profiling-enabled posture,
// lifecycle timestamps, and tags only - never profiling samples, aggregated
// profiles, flame graphs, or agent telemetry.
func mapProfilingGroup(description profilertypes.ProfilingGroupDescription) codeguruservice.ProfilingGroup {
	group := codeguruservice.ProfilingGroup{
		ARN:             strings.TrimSpace(aws.ToString(description.Arn)),
		Name:            strings.TrimSpace(aws.ToString(description.Name)),
		ComputePlatform: strings.TrimSpace(string(description.ComputePlatform)),
		CreatedAt:       aws.ToTime(description.CreatedAt),
		LastUpdatedAt:   aws.ToTime(description.UpdatedAt),
		Tags:            trimTags(description.Tags),
	}
	if config := description.AgentOrchestrationConfig; config != nil {
		group.ProfilingEnabled = config.ProfilingEnabled
	}
	return group
}
