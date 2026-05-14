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
	observation := awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          distributionARN,
		ResourceID:   distributionID,
		ResourceType: awscloud.ResourceTypeCloudFrontDistribution,
		Name:         distributionName,
		State:        strings.TrimSpace(distribution.Status),
		Tags:         cloneStringMap(distribution.Tags),
		Attributes: map[string]any{
			"id":                 strings.TrimSpace(distribution.ID),
			"domain_name":        strings.TrimSpace(distribution.DomainName),
			"status":             strings.TrimSpace(distribution.Status),
			"enabled":            distribution.Enabled,
			"comment":            strings.TrimSpace(distribution.Comment),
			"http_version":       strings.TrimSpace(distribution.HTTPVersion),
			"ipv6_enabled":       distribution.IPV6Enabled,
			"last_modified_time": timeOrNil(distribution.LastModifiedTime),
			"price_class":        strings.TrimSpace(distribution.PriceClass),
			"staging":            distribution.Staging,
			"aliases":            cloneStrings(distribution.Aliases),
			"web_acl_id":         strings.TrimSpace(distribution.WebACLID),
			"origins":            originAttributes(distribution.Origins),
		},
		CorrelationAnchors: []string{distributionARN, distribution.ID, distribution.DomainName},
		SourceRecordID:     distributionID,
	}
	if attributes := cacheBehaviorAttributes(distribution.DefaultCacheBehavior, false); attributes != nil {
		observation.Attributes["default_cache_behavior"] = attributes
	}
	if attributes := cacheBehaviorsAttributes(distribution.CacheBehaviors); attributes != nil {
		observation.Attributes["cache_behaviors"] = attributes
	}
	if attributes := viewerCertificateAttributes(distribution.ViewerCertificate); attributes != nil {
		observation.Attributes["viewer_certificate"] = attributes
	}
	return observation
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
		if attributes := cacheBehaviorAttributes(behavior, true); attributes != nil {
			output = append(output, attributes)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cacheBehaviorAttributes(behavior CacheBehavior, includePathPattern bool) map[string]any {
	if !hasCacheBehaviorData(behavior, includePathPattern) {
		return nil
	}
	attributes := map[string]any{}
	if value := strings.TrimSpace(behavior.TargetOriginID); value != "" {
		attributes["target_origin_id"] = value
	}
	if value := strings.TrimSpace(behavior.ViewerProtocolPolicy); value != "" {
		attributes["viewer_protocol_policy"] = value
	}
	if values := cloneStrings(behavior.AllowedMethods); values != nil {
		attributes["allowed_methods"] = values
	}
	if values := cloneStrings(behavior.CachedMethods); values != nil {
		attributes["cached_methods"] = values
	}
	if value := strings.TrimSpace(behavior.CachePolicyID); value != "" {
		attributes["cache_policy_id"] = value
	}
	if value := strings.TrimSpace(behavior.OriginRequestPolicyID); value != "" {
		attributes["origin_request_policy_id"] = value
	}
	if value := strings.TrimSpace(behavior.ResponseHeadersPolicyID); value != "" {
		attributes["response_headers_policy_id"] = value
	}
	if behavior.Compress != nil {
		attributes["compress"] = *behavior.Compress
	}
	if includePathPattern {
		if value := strings.TrimSpace(behavior.PathPattern); value != "" {
			attributes["path_pattern"] = value
		}
	}
	return attributes
}

func hasCacheBehaviorData(behavior CacheBehavior, includePathPattern bool) bool {
	if includePathPattern && strings.TrimSpace(behavior.PathPattern) != "" {
		return true
	}
	return strings.TrimSpace(behavior.TargetOriginID) != "" ||
		strings.TrimSpace(behavior.ViewerProtocolPolicy) != "" ||
		len(behavior.AllowedMethods) > 0 ||
		len(behavior.CachedMethods) > 0 ||
		strings.TrimSpace(behavior.CachePolicyID) != "" ||
		strings.TrimSpace(behavior.OriginRequestPolicyID) != "" ||
		strings.TrimSpace(behavior.ResponseHeadersPolicyID) != "" ||
		behavior.Compress != nil
}

func viewerCertificateAttributes(certificate ViewerCertificate) map[string]any {
	if !hasViewerCertificateData(certificate) {
		return nil
	}
	attributes := map[string]any{}
	if value := strings.TrimSpace(certificate.ACMCertificateARN); value != "" {
		attributes["acm_certificate_arn"] = value
	}
	if certificate.CloudFrontDefaultCertificate != nil {
		attributes["cloudfront_default_certificate"] = *certificate.CloudFrontDefaultCertificate
	}
	if value := strings.TrimSpace(certificate.IAMCertificateID); value != "" {
		attributes["iam_certificate_id"] = value
	}
	if value := strings.TrimSpace(certificate.MinimumProtocolVersion); value != "" {
		attributes["minimum_protocol_version"] = value
	}
	if value := strings.TrimSpace(certificate.SSLSupportMethod); value != "" {
		attributes["ssl_support_method"] = value
	}
	return attributes
}

func hasViewerCertificateData(certificate ViewerCertificate) bool {
	return strings.TrimSpace(certificate.ACMCertificateARN) != "" ||
		certificate.CloudFrontDefaultCertificate != nil ||
		strings.TrimSpace(certificate.IAMCertificateID) != "" ||
		strings.TrimSpace(certificate.MinimumProtocolVersion) != "" ||
		strings.TrimSpace(certificate.SSLSupportMethod) != ""
}
