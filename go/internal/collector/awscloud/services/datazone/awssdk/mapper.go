// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatazone "github.com/aws/aws-sdk-go-v2/service/datazone"
	awsdatazonetypes "github.com/aws/aws-sdk-go-v2/service/datazone/types"

	datazoneservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/datazone"
)

// listDomains pages every DataZone domain visible to the configured credentials.
func (c *Client) listDomains(ctx context.Context) ([]awsdatazonetypes.DomainSummary, error) {
	var summaries []awsdatazonetypes.DomainSummary
	var nextToken *string
	for {
		var page *awsdatazone.ListDomainsOutput
		err := c.recordAPICall(ctx, "ListDomains", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDomains(callCtx, &awsdatazone.ListDomainsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return summaries, nil
		}
		summaries = append(summaries, page.Items...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

// describeDomain enriches a domain summary with the GetDomain control-plane
// describe output (KMS key, execution/service IAM roles) and resource tags.
func (c *Client) describeDomain(
	ctx context.Context,
	summary awsdatazonetypes.DomainSummary,
) (datazoneservice.Domain, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.Id))
	domain := datazoneservice.Domain{
		ARN:           arn,
		ID:            id,
		Name:          strings.TrimSpace(aws.ToString(summary.Name)),
		Status:        strings.TrimSpace(string(summary.Status)),
		Description:   strings.TrimSpace(aws.ToString(summary.Description)),
		PortalURL:     strings.TrimSpace(aws.ToString(summary.PortalUrl)),
		CreatedAt:     aws.ToTime(summary.CreatedAt),
		LastUpdatedAt: aws.ToTime(summary.LastUpdatedAt),
	}
	if id == "" {
		return domain, nil
	}
	var output *awsdatazone.GetDomainOutput
	err := c.recordAPICall(ctx, "GetDomain", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetDomain(callCtx, &awsdatazone.GetDomainInput{
			Identifier: aws.String(id),
		})
		return err
	})
	if err != nil {
		return datazoneservice.Domain{}, err
	}
	if output != nil {
		domain.KMSKeyIdentifier = strings.TrimSpace(aws.ToString(output.KmsKeyIdentifier))
		domain.DomainExecutionRole = strings.TrimSpace(aws.ToString(output.DomainExecutionRole))
		domain.ServiceRole = strings.TrimSpace(aws.ToString(output.ServiceRole))
		if portal := strings.TrimSpace(aws.ToString(output.PortalUrl)); portal != "" {
			domain.PortalURL = portal
		}
		domain.Tags = trimTags(output.Tags)
	}
	if len(domain.Tags) == 0 && arn != "" {
		tags, err := c.listTags(ctx, arn)
		if err != nil {
			return datazoneservice.Domain{}, err
		}
		domain.Tags = tags
	}
	return domain, nil
}

// listProjects pages every project under a domain.
func (c *Client) listProjects(ctx context.Context, domainID string) ([]datazoneservice.Project, error) {
	domainID = strings.TrimSpace(domainID)
	if domainID == "" {
		return nil, nil
	}
	var projects []datazoneservice.Project
	var nextToken *string
	for {
		var page *awsdatazone.ListProjectsOutput
		err := c.recordAPICall(ctx, "ListProjects", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListProjects(callCtx, &awsdatazone.ListProjectsInput{
				DomainIdentifier: aws.String(domainID),
				NextToken:        nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return projects, nil
		}
		for _, summary := range page.Items {
			projects = append(projects, mapProject(domainID, summary))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return projects, nil
		}
	}
}

func mapProject(domainID string, summary awsdatazonetypes.ProjectSummary) datazoneservice.Project {
	return datazoneservice.Project{
		ID:              strings.TrimSpace(aws.ToString(summary.Id)),
		DomainID:        firstNonEmpty(strings.TrimSpace(aws.ToString(summary.DomainId)), domainID),
		Name:            strings.TrimSpace(aws.ToString(summary.Name)),
		Description:     strings.TrimSpace(aws.ToString(summary.Description)),
		Status:          strings.TrimSpace(string(summary.ProjectStatus)),
		ProjectCategory: strings.TrimSpace(aws.ToString(summary.ProjectCategory)),
		DomainUnitID:    strings.TrimSpace(aws.ToString(summary.DomainUnitId)),
		CreatedAt:       aws.ToTime(summary.CreatedAt),
		UpdatedAt:       aws.ToTime(summary.UpdatedAt),
	}
}

// listEnvironments pages every environment under each project of a domain.
func (c *Client) listEnvironments(
	ctx context.Context,
	domainID string,
	projects []datazoneservice.Project,
) ([]datazoneservice.Environment, error) {
	domainID = strings.TrimSpace(domainID)
	if domainID == "" {
		return nil, nil
	}
	var environments []datazoneservice.Environment
	for _, project := range projects {
		projectID := strings.TrimSpace(project.ID)
		if projectID == "" {
			continue
		}
		var nextToken *string
		for {
			var page *awsdatazone.ListEnvironmentsOutput
			err := c.recordAPICall(ctx, "ListEnvironments", func(callCtx context.Context) error {
				var err error
				page, err = c.client.ListEnvironments(callCtx, &awsdatazone.ListEnvironmentsInput{
					DomainIdentifier:  aws.String(domainID),
					ProjectIdentifier: aws.String(projectID),
					NextToken:         nextToken,
				})
				return err
			})
			if err != nil {
				return nil, err
			}
			if page == nil {
				break
			}
			for _, summary := range page.Items {
				environments = append(environments, mapEnvironment(domainID, summary))
			}
			nextToken = page.NextToken
			if aws.ToString(nextToken) == "" {
				break
			}
		}
	}
	return environments, nil
}

func mapEnvironment(domainID string, summary awsdatazonetypes.EnvironmentSummary) datazoneservice.Environment {
	return datazoneservice.Environment{
		ID:               strings.TrimSpace(aws.ToString(summary.Id)),
		DomainID:         firstNonEmpty(strings.TrimSpace(aws.ToString(summary.DomainId)), domainID),
		ProjectID:        strings.TrimSpace(aws.ToString(summary.ProjectId)),
		Name:             strings.TrimSpace(aws.ToString(summary.Name)),
		Description:      strings.TrimSpace(aws.ToString(summary.Description)),
		Provider:         strings.TrimSpace(aws.ToString(summary.Provider)),
		Status:           strings.TrimSpace(string(summary.Status)),
		ProfileID:        strings.TrimSpace(aws.ToString(summary.EnvironmentProfileId)),
		BlueprintID:      strings.TrimSpace(aws.ToString(summary.EnvironmentConfigurationId)),
		AWSAccountID:     strings.TrimSpace(aws.ToString(summary.AwsAccountId)),
		AWSAccountRegion: strings.TrimSpace(aws.ToString(summary.AwsAccountRegion)),
		CreatedAt:        aws.ToTime(summary.CreatedAt),
		UpdatedAt:        aws.ToTime(summary.UpdatedAt),
	}
}

// trimTags returns a trimmed-key copy of input, or nil when nothing survives.
func trimTags(input map[string]string) map[string]string {
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

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
