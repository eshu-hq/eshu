package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	cloudfrontservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudfront"
)

func mapDistribution(
	distribution awscloudfronttypes.DistributionSummary,
	tags map[string]string,
) cloudfrontservice.Distribution {
	return cloudfrontservice.Distribution{
		ARN:                  strings.TrimSpace(aws.ToString(distribution.ARN)),
		ID:                   strings.TrimSpace(aws.ToString(distribution.Id)),
		DomainName:           strings.TrimSpace(aws.ToString(distribution.DomainName)),
		Status:               strings.TrimSpace(aws.ToString(distribution.Status)),
		Enabled:              aws.ToBool(distribution.Enabled),
		Comment:              strings.TrimSpace(aws.ToString(distribution.Comment)),
		HTTPVersion:          string(distribution.HttpVersion),
		IPV6Enabled:          aws.ToBool(distribution.IsIPV6Enabled),
		LastModifiedTime:     aws.ToTime(distribution.LastModifiedTime),
		PriceClass:           string(distribution.PriceClass),
		Staging:              aws.ToBool(distribution.Staging),
		WebACLID:             strings.TrimSpace(aws.ToString(distribution.WebACLId)),
		Aliases:              mapAliases(distribution.Aliases),
		Origins:              mapOrigins(distribution.Origins),
		DefaultCacheBehavior: mapDefaultCacheBehavior(distribution.DefaultCacheBehavior),
		CacheBehaviors:       mapCacheBehaviors(distribution.CacheBehaviors),
		ViewerCertificate:    mapViewerCertificate(distribution.ViewerCertificate),
		Tags:                 cloneStringMap(tags),
	}
}

func mapAliases(aliases *awscloudfronttypes.Aliases) []string {
	if aliases == nil {
		return nil
	}
	return cloneStrings(aliases.Items)
}

func mapOrigins(origins *awscloudfronttypes.Origins) []cloudfrontservice.Origin {
	if origins == nil || len(origins.Items) == 0 {
		return nil
	}
	output := make([]cloudfrontservice.Origin, 0, len(origins.Items))
	for _, origin := range origins.Items {
		output = append(output, cloudfrontservice.Origin{
			ID:                    strings.TrimSpace(aws.ToString(origin.Id)),
			DomainName:            strings.TrimSpace(aws.ToString(origin.DomainName)),
			OriginPath:            strings.TrimSpace(aws.ToString(origin.OriginPath)),
			OriginAccessControlID: strings.TrimSpace(aws.ToString(origin.OriginAccessControlId)),
			CustomHeaderNames:     mapCustomHeaderNames(origin.CustomHeaders),
		})
	}
	return output
}

func mapCustomHeaderNames(headers *awscloudfronttypes.CustomHeaders) []string {
	if headers == nil || len(headers.Items) == 0 {
		return nil
	}
	output := make([]string, 0, len(headers.Items))
	for _, header := range headers.Items {
		if name := strings.TrimSpace(aws.ToString(header.HeaderName)); name != "" {
			output = append(output, name)
		}
	}
	return output
}

func mapDefaultCacheBehavior(
	behavior *awscloudfronttypes.DefaultCacheBehavior,
) cloudfrontservice.CacheBehavior {
	if behavior == nil {
		return cloudfrontservice.CacheBehavior{}
	}
	return cloudfrontservice.CacheBehavior{
		TargetOriginID:          strings.TrimSpace(aws.ToString(behavior.TargetOriginId)),
		ViewerProtocolPolicy:    string(behavior.ViewerProtocolPolicy),
		AllowedMethods:          mapAllowedMethods(behavior.AllowedMethods),
		CachedMethods:           mapCachedMethods(behavior.AllowedMethods),
		CachePolicyID:           strings.TrimSpace(aws.ToString(behavior.CachePolicyId)),
		OriginRequestPolicyID:   strings.TrimSpace(aws.ToString(behavior.OriginRequestPolicyId)),
		ResponseHeadersPolicyID: strings.TrimSpace(aws.ToString(behavior.ResponseHeadersPolicyId)),
		Compress:                cloneBool(behavior.Compress),
	}
}

func mapCacheBehaviors(
	behaviors *awscloudfronttypes.CacheBehaviors,
) []cloudfrontservice.CacheBehavior {
	if behaviors == nil || len(behaviors.Items) == 0 {
		return nil
	}
	output := make([]cloudfrontservice.CacheBehavior, 0, len(behaviors.Items))
	for _, behavior := range behaviors.Items {
		output = append(output, cloudfrontservice.CacheBehavior{
			PathPattern:             strings.TrimSpace(aws.ToString(behavior.PathPattern)),
			TargetOriginID:          strings.TrimSpace(aws.ToString(behavior.TargetOriginId)),
			ViewerProtocolPolicy:    string(behavior.ViewerProtocolPolicy),
			AllowedMethods:          mapAllowedMethods(behavior.AllowedMethods),
			CachedMethods:           mapCachedMethods(behavior.AllowedMethods),
			CachePolicyID:           strings.TrimSpace(aws.ToString(behavior.CachePolicyId)),
			OriginRequestPolicyID:   strings.TrimSpace(aws.ToString(behavior.OriginRequestPolicyId)),
			ResponseHeadersPolicyID: strings.TrimSpace(aws.ToString(behavior.ResponseHeadersPolicyId)),
			Compress:                cloneBool(behavior.Compress),
		})
	}
	return output
}

func mapAllowedMethods(methods *awscloudfronttypes.AllowedMethods) []string {
	if methods == nil || len(methods.Items) == 0 {
		return nil
	}
	output := make([]string, 0, len(methods.Items))
	for _, method := range methods.Items {
		if value := strings.TrimSpace(string(method)); value != "" {
			output = append(output, value)
		}
	}
	return output
}

func mapCachedMethods(methods *awscloudfronttypes.AllowedMethods) []string {
	if methods == nil || methods.CachedMethods == nil || len(methods.CachedMethods.Items) == 0 {
		return nil
	}
	output := make([]string, 0, len(methods.CachedMethods.Items))
	for _, method := range methods.CachedMethods.Items {
		if value := strings.TrimSpace(string(method)); value != "" {
			output = append(output, value)
		}
	}
	return output
}

func mapViewerCertificate(
	certificate *awscloudfronttypes.ViewerCertificate,
) cloudfrontservice.ViewerCertificate {
	if certificate == nil {
		return cloudfrontservice.ViewerCertificate{}
	}
	return cloudfrontservice.ViewerCertificate{
		ACMCertificateARN:            strings.TrimSpace(aws.ToString(certificate.ACMCertificateArn)),
		CloudFrontDefaultCertificate: cloneBool(certificate.CloudFrontDefaultCertificate),
		IAMCertificateID:             strings.TrimSpace(aws.ToString(certificate.IAMCertificateId)),
		MinimumProtocolVersion:       string(certificate.MinimumProtocolVersion),
		SSLSupportMethod:             string(certificate.SSLSupportMethod),
	}
}

func mapTags(tags []awscloudfronttypes.Tag) map[string]string {
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

func cloneStringMap(input map[string]string) map[string]string {
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

func cloneBool(input *bool) *bool {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}
