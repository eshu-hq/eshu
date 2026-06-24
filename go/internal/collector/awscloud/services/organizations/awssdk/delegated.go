// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsorg "github.com/aws/aws-sdk-go-v2/service/organizations"
	awsorgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"

	organizationsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations"
)

func (c *Client) listDelegatedAdministrators(ctx context.Context) ([]organizationsservice.DelegatedAdministrator, error) {
	var admins []organizationsservice.DelegatedAdministrator
	var nextToken *string
	for {
		var output *awsorg.ListDelegatedAdministratorsOutput
		err := c.recordAPICall(ctx, "ListDelegatedAdministrators", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListDelegatedAdministrators(callCtx, &awsorg.ListDelegatedAdministratorsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return admins, nil
		}
		for _, admin := range output.DelegatedAdministrators {
			mapped, err := c.mapDelegatedAdministrator(ctx, admin)
			if err != nil {
				return nil, err
			}
			admins = append(admins, mapped...)
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return admins, nil
		}
	}
}

func (c *Client) mapDelegatedAdministrator(
	ctx context.Context,
	admin awsorgtypes.DelegatedAdministrator,
) ([]organizationsservice.DelegatedAdministrator, error) {
	accountID := aws.ToString(admin.Id)
	services, err := c.listDelegatedServicesForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return []organizationsservice.DelegatedAdministrator{delegatedAdministrator(admin, awsorgtypes.DelegatedService{})}, nil
	}
	mapped := make([]organizationsservice.DelegatedAdministrator, 0, len(services))
	for _, service := range services {
		mapped = append(mapped, delegatedAdministrator(admin, service))
	}
	return mapped, nil
}

func (c *Client) listDelegatedServicesForAccount(
	ctx context.Context,
	accountID string,
) ([]awsorgtypes.DelegatedService, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, nil
	}
	var services []awsorgtypes.DelegatedService
	var nextToken *string
	for {
		var output *awsorg.ListDelegatedServicesForAccountOutput
		err := c.recordAPICall(ctx, "ListDelegatedServicesForAccount", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListDelegatedServicesForAccount(callCtx, &awsorg.ListDelegatedServicesForAccountInput{
				AccountId: aws.String(accountID),
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return services, nil
		}
		services = append(services, output.DelegatedServices...)
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return services, nil
		}
	}
}

func delegatedAdministrator(
	admin awsorgtypes.DelegatedAdministrator,
	service awsorgtypes.DelegatedService,
) organizationsservice.DelegatedAdministrator {
	delegationEnabledAt := aws.ToTime(service.DelegationEnabledDate)
	if delegationEnabledAt.IsZero() {
		delegationEnabledAt = aws.ToTime(admin.DelegationEnabledDate)
	}
	return organizationsservice.DelegatedAdministrator{
		AccountARN:          strings.TrimSpace(aws.ToString(admin.Arn)),
		AccountEmail:        strings.TrimSpace(aws.ToString(admin.Email)),
		AccountID:           strings.TrimSpace(aws.ToString(admin.Id)),
		AccountName:         strings.TrimSpace(aws.ToString(admin.Name)),
		DelegationEnabledAt: delegationEnabledAt,
		JoinedAt:            aws.ToTime(admin.JoinedTimestamp),
		JoinedVia:           strings.TrimSpace(string(admin.JoinedMethod)),
		ServicePrincipal:    strings.TrimSpace(aws.ToString(service.ServicePrincipal)),
		State:               strings.TrimSpace(string(admin.State)),
		Status:              strings.TrimSpace(string(admin.Status)),
	}
}
