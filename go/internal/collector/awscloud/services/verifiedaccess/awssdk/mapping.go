// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	verifiedaccessservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/verifiedaccess"
)

// mapInstance maps an EC2 Verified Access instance into scanner-owned metadata.
// It copies only safe identity, FIPS, encryption-flag, attached-trust-provider,
// and lifecycle fields.
func mapInstance(instance awsec2types.VerifiedAccessInstance) verifiedaccessservice.Instance {
	return verifiedaccessservice.Instance{
		ID:                        strings.TrimSpace(aws.ToString(instance.VerifiedAccessInstanceId)),
		Description:               strings.TrimSpace(aws.ToString(instance.Description)),
		FIPSEnabled:               aws.ToBool(instance.FipsEnabled),
		CustomerManagedKeyEnabled: customerManagedKeyEnabled(nil),
		TrustProviderIDs:          trustProviderIDs(instance.VerifiedAccessTrustProviders),
		CreationTime:              parseTime(instance.CreationTime),
		LastUpdatedTime:           parseTime(instance.LastUpdatedTime),
		Tags:                      mapTags(instance.Tags),
	}
}

// mapGroup maps an EC2 Verified Access group into scanner-owned metadata. The
// group policy document is never read; only safe identity, ownership,
// encryption-flag, and lifecycle fields are copied.
func mapGroup(group awsec2types.VerifiedAccessGroup) verifiedaccessservice.Group {
	return verifiedaccessservice.Group{
		ARN:                       strings.TrimSpace(aws.ToString(group.VerifiedAccessGroupArn)),
		ID:                        strings.TrimSpace(aws.ToString(group.VerifiedAccessGroupId)),
		InstanceID:                strings.TrimSpace(aws.ToString(group.VerifiedAccessInstanceId)),
		Owner:                     strings.TrimSpace(aws.ToString(group.Owner)),
		Description:               strings.TrimSpace(aws.ToString(group.Description)),
		CustomerManagedKeyEnabled: customerManagedKeyEnabled(group.SseSpecification),
		CreationTime:              parseTime(group.CreationTime),
		LastUpdatedTime:           parseTime(group.LastUpdatedTime),
		Tags:                      mapTags(group.Tags),
	}
}

// mapEndpoint maps an EC2 Verified Access endpoint into scanner-owned metadata.
// The endpoint policy document is never read. Subnet and security-group ids are
// gathered from the attachment-type-specific options so the cross-service graph
// edges resolve to the EC2 scanner's published node ids.
func mapEndpoint(endpoint awsec2types.VerifiedAccessEndpoint) verifiedaccessservice.Endpoint {
	mapped := verifiedaccessservice.Endpoint{
		ID:                   strings.TrimSpace(aws.ToString(endpoint.VerifiedAccessEndpointId)),
		GroupID:              strings.TrimSpace(aws.ToString(endpoint.VerifiedAccessGroupId)),
		InstanceID:           strings.TrimSpace(aws.ToString(endpoint.VerifiedAccessInstanceId)),
		Description:          strings.TrimSpace(aws.ToString(endpoint.Description)),
		EndpointType:         strings.TrimSpace(string(endpoint.EndpointType)),
		AttachmentType:       strings.TrimSpace(string(endpoint.AttachmentType)),
		ApplicationDomain:    strings.TrimSpace(aws.ToString(endpoint.ApplicationDomain)),
		EndpointDomain:       strings.TrimSpace(aws.ToString(endpoint.EndpointDomain)),
		DomainCertificateARN: strings.TrimSpace(aws.ToString(endpoint.DomainCertificateArn)),
		SecurityGroupIDs:     trimAll(endpoint.SecurityGroupIds),
		CreationTime:         parseTime(endpoint.CreationTime),
		LastUpdatedTime:      parseTime(endpoint.LastUpdatedTime),
		Tags:                 mapTags(endpoint.Tags),
	}
	if status := endpoint.Status; status != nil {
		mapped.Status = strings.TrimSpace(string(status.Code))
	}
	mapped.SubnetIDs = endpointSubnetIDs(endpoint)
	if lb := endpoint.LoadBalancerOptions; lb != nil {
		mapped.LoadBalancerARN = strings.TrimSpace(aws.ToString(lb.LoadBalancerArn))
	}
	if eni := endpoint.NetworkInterfaceOptions; eni != nil {
		mapped.NetworkInterfaceID = strings.TrimSpace(aws.ToString(eni.NetworkInterfaceId))
	}
	return mapped
}

// mapTrustProvider maps an EC2 Verified Access trust provider into scanner-owned
// metadata. OIDC client identifiers, client secrets, and token/userinfo
// endpoints are never copied; only the OIDC issuer reference and the provider
// type taxonomy are kept.
func mapTrustProvider(trustProvider awsec2types.VerifiedAccessTrustProvider) verifiedaccessservice.TrustProvider {
	mapped := verifiedaccessservice.TrustProvider{
		ID:                        strings.TrimSpace(aws.ToString(trustProvider.VerifiedAccessTrustProviderId)),
		Description:               strings.TrimSpace(aws.ToString(trustProvider.Description)),
		TrustProviderType:         strings.TrimSpace(string(trustProvider.TrustProviderType)),
		UserTrustProviderType:     strings.TrimSpace(string(trustProvider.UserTrustProviderType)),
		DeviceTrustProviderType:   strings.TrimSpace(string(trustProvider.DeviceTrustProviderType)),
		PolicyReferenceName:       strings.TrimSpace(aws.ToString(trustProvider.PolicyReferenceName)),
		CustomerManagedKeyEnabled: customerManagedKeyEnabled(trustProvider.SseSpecification),
		CreationTime:              parseTime(trustProvider.CreationTime),
		LastUpdatedTime:           parseTime(trustProvider.LastUpdatedTime),
		Tags:                      mapTags(trustProvider.Tags),
	}
	if oidc := trustProvider.OidcOptions; oidc != nil {
		// Copy only the issuer reference. ClientId, ClientSecret, and the token/
		// userinfo endpoints are never persisted.
		mapped.OIDCIssuer = strings.TrimSpace(aws.ToString(oidc.Issuer))
	}
	return mapped
}

// endpointSubnetIDs gathers the bare subnet ids from whichever attachment-type
// options the endpoint carries (load-balancer, CIDR, or RDS). Network-interface
// endpoints attach to a single ENI and report no subnet list.
func endpointSubnetIDs(endpoint awsec2types.VerifiedAccessEndpoint) []string {
	switch {
	case endpoint.LoadBalancerOptions != nil:
		return trimAll(endpoint.LoadBalancerOptions.SubnetIds)
	case endpoint.CidrOptions != nil:
		return trimAll(endpoint.CidrOptions.SubnetIds)
	case endpoint.RdsOptions != nil:
		return trimAll(endpoint.RdsOptions.SubnetIds)
	default:
		return nil
	}
}

// customerManagedKeyEnabled reports whether the SSE specification uses a
// customer-managed KMS key. It never copies the KMS key ARN into the metadata
// payload, keeping the flag the only encryption signal.
func customerManagedKeyEnabled(spec *awsec2types.VerifiedAccessSseSpecificationResponse) bool {
	if spec == nil {
		return false
	}
	return aws.ToBool(spec.CustomerManagedKeyEnabled)
}

func trustProviderIDs(condensed []awsec2types.VerifiedAccessTrustProviderCondensed) []string {
	if len(condensed) == 0 {
		return nil
	}
	ids := make([]string, 0, len(condensed))
	for _, provider := range condensed {
		if id := strings.TrimSpace(aws.ToString(provider.VerifiedAccessTrustProviderId)); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func trimAll(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapTags(tags []awsec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseTime parses the RFC3339 timestamp strings the EC2 Verified Access API
// returns. It returns the zero time for an empty or unparseable value so the
// scanner omits an unknown timestamp instead of emitting an epoch.
func parseTime(value *string) time.Time {
	raw := strings.TrimSpace(aws.ToString(value))
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
