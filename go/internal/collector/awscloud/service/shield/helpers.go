// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shield

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// protectedTarget is the classified join target for a protection's protected
// resource ARN. TargetType names a declared awscloud.ResourceType* constant,
// TargetResourceID is the join key that matches how the target scanner
// publishes its resource_id, and ARNKeyed reports whether that key is the ARN
// itself (so the relationship may also carry target_arn) or a bare id (so it
// must not, to keep the relguard join-mode check satisfied).
type protectedTarget struct {
	TargetType       string
	TargetResourceID string
	ARNKeyed         bool
}

// classifyProtectedARN maps a Shield Advanced protected resource ARN to the
// Eshu resource family it references and the join key the target scanner
// publishes for that family. AWS reports the protected ARN already
// partition-correct, so it is used directly (or has its bare id extracted)
// rather than synthesized.
//
// The five families Shield Advanced can protect map as follows:
//
//   - ELBv2 load balancer (:elasticloadbalancing:) -> aws_elbv2_load_balancer,
//     keyed by the full ARN (the elbv2 scanner publishes resource_id = LB ARN);
//   - CloudFront distribution (:cloudfront:) -> aws_cloudfront_distribution,
//     keyed by the full ARN (the cloudfront scanner publishes resource_id =
//     distribution ARN);
//   - Global Accelerator accelerator (:globalaccelerator:) ->
//     aws_globalaccelerator_accelerator, keyed by the full ARN (the
//     globalaccelerator scanner publishes resource_id = accelerator ARN);
//   - Elastic IP (:eip-allocation/eipalloc-...) -> aws_vpc_elastic_ip, keyed by
//     the bare eipalloc- allocation id (the vpc scanner publishes resource_id =
//     allocation id), so the edge is not ARN-keyed;
//   - Route 53 hosted zone (:route53:::hostedzone/Z...) ->
//     aws_route53_hosted_zone, keyed by the bare hosted-zone id (the route53
//     scanner strips the /hostedzone/ prefix from its resource_id), so the edge
//     is not ARN-keyed.
//
// It returns ok=false for an empty ARN or any service segment without a
// canonical Eshu resource family. The caller skips emission rather than
// emitting an empty or dangling target_type.
func classifyProtectedARN(arn string) (protectedTarget, bool) {
	trimmed := strings.TrimSpace(arn)
	if trimmed == "" {
		return protectedTarget{}, false
	}
	switch {
	case strings.Contains(trimmed, ":elasticloadbalancing:"):
		return protectedTarget{
			TargetType:       awscloud.ResourceTypeELBv2LoadBalancer,
			TargetResourceID: trimmed,
			ARNKeyed:         true,
		}, true
	case strings.Contains(trimmed, ":cloudfront:"):
		return protectedTarget{
			TargetType:       awscloud.ResourceTypeCloudFrontDistribution,
			TargetResourceID: trimmed,
			ARNKeyed:         true,
		}, true
	case strings.Contains(trimmed, ":globalaccelerator:"):
		return protectedTarget{
			TargetType:       awscloud.ResourceTypeGlobalAcceleratorAccelerator,
			TargetResourceID: trimmed,
			ARNKeyed:         true,
		}, true
	case strings.Contains(trimmed, ":eip-allocation/"):
		allocationID := arnResourceSuffix(trimmed, "eip-allocation/")
		if allocationID == "" {
			return protectedTarget{}, false
		}
		return protectedTarget{
			TargetType:       awscloud.ResourceTypeVPCElasticIP,
			TargetResourceID: allocationID,
			ARNKeyed:         false,
		}, true
	case strings.Contains(trimmed, ":hostedzone/"):
		zoneID := arnResourceSuffix(trimmed, "hostedzone/")
		if zoneID == "" {
			return protectedTarget{}, false
		}
		return protectedTarget{
			TargetType: awscloud.ResourceTypeRoute53HostedZone,
			// The route53 scanner publishes the hosted zone resource_id with the
			// "/hostedzone/" prefix (it does not strip the API-reported ID), so
			// the edge must carry the same prefixed form to join the node.
			TargetResourceID: "/hostedzone/" + zoneID,
			ARNKeyed:         false,
		}, true
	default:
		return protectedTarget{}, false
	}
}

// arnResourceSuffix returns the resource id that follows marker in an ARN's
// resource segment, trimming any leading path so the bare id (for example
// eipalloc-0a1b2c3d or Z1234567890ABC) matches the target scanner's published
// resource_id. It returns the empty string when the marker is absent or has no
// suffix.
func arnResourceSuffix(arn, marker string) string {
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(arn[idx+len(marker):])
}

// firstNonEmpty returns the first non-empty, trimmed value, or the empty string
// when every value is blank.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
