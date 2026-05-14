package cloudfront

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS CloudFront distribution metadata facts for one claimed
// account. It never reads objects, origin payloads, policy documents,
// certificate bodies, private keys, or mutates distribution configuration.
type Scanner struct {
	Client Client
}

// Scan observes CloudFront distributions and direct certificate/WAF dependency
// metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cloudfront scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceCloudFront
	case awscloud.ServiceCloudFront:
	default:
		return nil, fmt.Errorf("cloudfront scanner received service_kind %q", boundary.ServiceKind)
	}

	distributions, err := s.Client.ListDistributions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudFront distributions: %w", err)
	}
	var envelopes []facts.Envelope
	for _, distribution := range distributions {
		resource, err := awscloud.NewResourceEnvelope(distributionObservation(boundary, distribution))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range distributionRelationships(boundary, distribution) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func distributionObservation(
	boundary awscloud.Boundary,
	distribution Distribution,
) awscloud.ResourceObservation {
	distributionARN := strings.TrimSpace(distribution.ARN)
	distributionID := distributionResourceID(distribution)
	distributionName := firstNonEmpty(distribution.ID, distribution.DomainName, distributionARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          distributionARN,
		ResourceID:   distributionID,
		ResourceType: awscloud.ResourceTypeCloudFrontDistribution,
		Name:         distributionName,
		State:        strings.TrimSpace(distribution.Status),
		Tags:         cloneStringMap(distribution.Tags),
		Attributes: map[string]any{
			"id":                     strings.TrimSpace(distribution.ID),
			"domain_name":            strings.TrimSpace(distribution.DomainName),
			"status":                 strings.TrimSpace(distribution.Status),
			"enabled":                distribution.Enabled,
			"comment":                strings.TrimSpace(distribution.Comment),
			"http_version":           strings.TrimSpace(distribution.HTTPVersion),
			"ipv6_enabled":           distribution.IPV6Enabled,
			"last_modified_time":     timeOrNil(distribution.LastModifiedTime),
			"price_class":            strings.TrimSpace(distribution.PriceClass),
			"staging":                distribution.Staging,
			"aliases":                cloneStrings(distribution.Aliases),
			"web_acl_id":             strings.TrimSpace(distribution.WebACLID),
			"origins":                originAttributes(distribution.Origins),
			"default_cache_behavior": cacheBehaviorAttributes(distribution.DefaultCacheBehavior, false),
			"cache_behaviors":        cacheBehaviorsAttributes(distribution.CacheBehaviors),
			"viewer_certificate":     viewerCertificateAttributes(distribution.ViewerCertificate),
		},
		CorrelationAnchors: []string{distributionARN, distribution.ID, distribution.DomainName},
		SourceRecordID:     distributionID,
	}
}

func distributionResourceID(distribution Distribution) string {
	return firstNonEmpty(distribution.ARN, distribution.ID, distribution.DomainName)
}

func originAttributes(origins []Origin) []map[string]any {
	if len(origins) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(origins))
	for _, origin := range origins {
		output = append(output, map[string]any{
			"id":                       strings.TrimSpace(origin.ID),
			"domain_name":              strings.TrimSpace(origin.DomainName),
			"origin_path":              strings.TrimSpace(origin.OriginPath),
			"origin_access_control_id": strings.TrimSpace(origin.OriginAccessControlID),
			"custom_header_names":      cloneStrings(origin.CustomHeaderNames),
		})
	}
	return output
}

func cacheBehaviorsAttributes(behaviors []CacheBehavior) []map[string]any {
	if len(behaviors) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(behaviors))
	for _, behavior := range behaviors {
		output = append(output, cacheBehaviorAttributes(behavior, true))
	}
	return output
}

func cacheBehaviorAttributes(behavior CacheBehavior, includePathPattern bool) map[string]any {
	attributes := map[string]any{
		"target_origin_id":           strings.TrimSpace(behavior.TargetOriginID),
		"viewer_protocol_policy":     strings.TrimSpace(behavior.ViewerProtocolPolicy),
		"allowed_methods":            cloneStrings(behavior.AllowedMethods),
		"cached_methods":             cloneStrings(behavior.CachedMethods),
		"cache_policy_id":            strings.TrimSpace(behavior.CachePolicyID),
		"origin_request_policy_id":   strings.TrimSpace(behavior.OriginRequestPolicyID),
		"response_headers_policy_id": strings.TrimSpace(behavior.ResponseHeadersPolicyID),
		"compress":                   behavior.Compress,
	}
	if includePathPattern {
		attributes["path_pattern"] = strings.TrimSpace(behavior.PathPattern)
	}
	return attributes
}

func viewerCertificateAttributes(certificate ViewerCertificate) map[string]any {
	return map[string]any{
		"acm_certificate_arn":            strings.TrimSpace(certificate.ACMCertificateARN),
		"cloudfront_default_certificate": certificate.CloudFrontDefaultCertificate,
		"iam_certificate_id":             strings.TrimSpace(certificate.IAMCertificateID),
		"minimum_protocol_version":       strings.TrimSpace(certificate.MinimumProtocolVersion),
		"ssl_support_method":             strings.TrimSpace(certificate.SSLSupportMethod),
	}
}
