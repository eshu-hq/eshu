// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsvpclattice "github.com/aws/aws-sdk-go-v2/service/vpclattice"
	awsvpclatticetypes "github.com/aws/aws-sdk-go-v2/service/vpclattice/types"

	vpclatticeservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/vpclattice"
)

func (c *Client) listServiceNetworks(ctx context.Context) ([]vpclatticeservice.ServiceNetwork, error) {
	var networks []vpclatticeservice.ServiceNetwork
	var nextToken *string
	for {
		var page *awsvpclattice.ListServiceNetworksOutput
		err := c.recordAPICall(ctx, "ListServiceNetworks", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServiceNetworks(callCtx, &awsvpclattice.ListServiceNetworksInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return networks, nil
		}
		for _, summary := range page.Items {
			mapped, err := c.mapServiceNetwork(ctx, summary)
			if err != nil {
				return nil, err
			}
			networks = append(networks, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return networks, nil
		}
	}
}

func (c *Client) mapServiceNetwork(
	ctx context.Context,
	summary awsvpclatticetypes.ServiceNetworkSummary,
) (vpclatticeservice.ServiceNetwork, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.Id))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return vpclatticeservice.ServiceNetwork{}, err
	}
	vpcAssociations, err := c.listVPCAssociations(ctx, id)
	if err != nil {
		return vpclatticeservice.ServiceNetwork{}, err
	}
	serviceAssociations, err := c.listServiceAssociations(ctx, id)
	if err != nil {
		return vpclatticeservice.ServiceNetwork{}, err
	}
	return vpclatticeservice.ServiceNetwork{
		ARN:                                      arn,
		ID:                                       id,
		Name:                                     strings.TrimSpace(aws.ToString(summary.Name)),
		NumberOfAssociatedServices:               aws.ToInt64(summary.NumberOfAssociatedServices),
		NumberOfAssociatedVPCs:                   aws.ToInt64(summary.NumberOfAssociatedVPCs),
		NumberOfAssociatedResourceConfigurations: aws.ToInt64(summary.NumberOfAssociatedResourceConfigurations),
		CreatedAt:                                aws.ToTime(summary.CreatedAt),
		LastUpdatedAt:                            aws.ToTime(summary.LastUpdatedAt),
		Tags:                                     tags,
		VPCAssociations:                          vpcAssociations,
		ServiceAssociations:                      serviceAssociations,
	}, nil
}

func (c *Client) listVPCAssociations(ctx context.Context, networkID string) ([]vpclatticeservice.VPCAssociation, error) {
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return nil, nil
	}
	var associations []vpclatticeservice.VPCAssociation
	var nextToken *string
	for {
		var page *awsvpclattice.ListServiceNetworkVpcAssociationsOutput
		err := c.recordAPICall(ctx, "ListServiceNetworkVpcAssociations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServiceNetworkVpcAssociations(callCtx, &awsvpclattice.ListServiceNetworkVpcAssociationsInput{
				ServiceNetworkIdentifier: aws.String(networkID),
				NextToken:                nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return associations, nil
		}
		for _, summary := range page.Items {
			associations = append(associations, vpclatticeservice.VPCAssociation{
				ID:     strings.TrimSpace(aws.ToString(summary.Id)),
				VPCID:  strings.TrimSpace(aws.ToString(summary.VpcId)),
				Status: strings.TrimSpace(string(summary.Status)),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return associations, nil
		}
	}
}

func (c *Client) listServiceAssociations(ctx context.Context, networkID string) ([]vpclatticeservice.ServiceAssociation, error) {
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return nil, nil
	}
	var associations []vpclatticeservice.ServiceAssociation
	var nextToken *string
	for {
		var page *awsvpclattice.ListServiceNetworkServiceAssociationsOutput
		err := c.recordAPICall(ctx, "ListServiceNetworkServiceAssociations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServiceNetworkServiceAssociations(callCtx, &awsvpclattice.ListServiceNetworkServiceAssociationsInput{
				ServiceNetworkIdentifier: aws.String(networkID),
				NextToken:                nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return associations, nil
		}
		for _, summary := range page.Items {
			associations = append(associations, vpclatticeservice.ServiceAssociation{
				ID:         strings.TrimSpace(aws.ToString(summary.Id)),
				ServiceARN: strings.TrimSpace(aws.ToString(summary.ServiceArn)),
				ServiceID:  strings.TrimSpace(aws.ToString(summary.ServiceId)),
				Status:     strings.TrimSpace(string(summary.Status)),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return associations, nil
		}
	}
}

func (c *Client) listServices(ctx context.Context) ([]vpclatticeservice.Service, error) {
	var services []vpclatticeservice.Service
	var nextToken *string
	for {
		var page *awsvpclattice.ListServicesOutput
		err := c.recordAPICall(ctx, "ListServices", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServices(callCtx, &awsvpclattice.ListServicesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return services, nil
		}
		for _, summary := range page.Items {
			mapped, err := c.mapService(ctx, summary)
			if err != nil {
				return nil, err
			}
			services = append(services, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return services, nil
		}
	}
}

func (c *Client) mapService(
	ctx context.Context,
	summary awsvpclatticetypes.ServiceSummary,
) (vpclatticeservice.Service, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.Id))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return vpclatticeservice.Service{}, err
	}
	service := vpclatticeservice.Service{
		ARN:                arn,
		ID:                 id,
		Name:               strings.TrimSpace(aws.ToString(summary.Name)),
		Status:             strings.TrimSpace(string(summary.Status)),
		CustomDomainName:   strings.TrimSpace(aws.ToString(summary.CustomDomainName)),
		DNSEntryDomainName: dnsEntryDomainName(summary.DnsEntry),
		CreatedAt:          aws.ToTime(summary.CreatedAt),
		LastUpdatedAt:      aws.ToTime(summary.LastUpdatedAt),
		Tags:               tags,
	}
	if err := c.enrichService(ctx, &service, id); err != nil {
		return vpclatticeservice.Service{}, err
	}
	listeners, err := c.listListeners(ctx, id)
	if err != nil {
		return vpclatticeservice.Service{}, err
	}
	service.Listeners = listeners
	return service, nil
}

// enrichService reads GetService for the ACM certificate ARN and auth type that
// the ListServices summary does not carry. It never reads the auth-policy body.
func (c *Client) enrichService(ctx context.Context, service *vpclatticeservice.Service, serviceID string) error {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil
	}
	var output *awsvpclattice.GetServiceOutput
	err := c.recordAPICall(ctx, "GetService", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetService(callCtx, &awsvpclattice.GetServiceInput{
			ServiceIdentifier: aws.String(serviceID),
		})
		return callErr
	})
	if err != nil || output == nil {
		return err
	}
	service.AuthType = strings.TrimSpace(string(output.AuthType))
	service.CertificateARN = strings.TrimSpace(aws.ToString(output.CertificateArn))
	if service.DNSEntryDomainName == "" {
		service.DNSEntryDomainName = dnsEntryDomainName(output.DnsEntry)
	}
	return nil
}

func (c *Client) listListeners(ctx context.Context, serviceID string) ([]vpclatticeservice.Listener, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, nil
	}
	var listeners []vpclatticeservice.Listener
	var nextToken *string
	for {
		var page *awsvpclattice.ListListenersOutput
		err := c.recordAPICall(ctx, "ListListeners", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListListeners(callCtx, &awsvpclattice.ListListenersInput{
				ServiceIdentifier: aws.String(serviceID),
				NextToken:         nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return listeners, nil
		}
		for _, summary := range page.Items {
			listeners = append(listeners, vpclatticeservice.Listener{
				ARN:      strings.TrimSpace(aws.ToString(summary.Arn)),
				ID:       strings.TrimSpace(aws.ToString(summary.Id)),
				Name:     strings.TrimSpace(aws.ToString(summary.Name)),
				Protocol: strings.TrimSpace(string(summary.Protocol)),
				Port:     aws.ToInt32(summary.Port),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return listeners, nil
		}
	}
}

func dnsEntryDomainName(entry *awsvpclatticetypes.DnsEntry) string {
	if entry == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(entry.DomainName))
}
