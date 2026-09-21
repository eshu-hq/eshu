// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudfront

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func distributionRelationships(
	boundary aws.Boundary,
	distribution Distribution,
) []aws.RelationshipObservation {
	distributionID := distributionResourceID(distribution)
	if distributionID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	if relationship, ok := acmCertificateRelationship(boundary, distribution, distributionID); ok {
		relationships = append(relationships, relationship)
	}
	if relationship, ok := wafWebACLRelationship(boundary, distribution, distributionID); ok {
		relationships = append(relationships, relationship)
	}
	return relationships
}

func acmCertificateRelationship(
	boundary aws.Boundary,
	distribution Distribution,
	distributionID string,
) (aws.RelationshipObservation, bool) {
	certificateARN := strings.TrimSpace(distribution.ViewerCertificate.ACMCertificateARN)
	if certificateARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCloudFrontDistributionUsesACMCertificate,
		SourceResourceID: distributionID,
		SourceARN:        strings.TrimSpace(distribution.ARN),
		TargetResourceID: certificateARN,
		TargetARN:        certificateARN,
		TargetType:       "aws_acm_certificate",
		Attributes: map[string]any{
			"distribution_id": strings.TrimSpace(distribution.ID),
		},
		SourceRecordID: distributionID + "->" + certificateARN,
	}, true
}

func wafWebACLRelationship(
	boundary aws.Boundary,
	distribution Distribution,
	distributionID string,
) (aws.RelationshipObservation, bool) {
	webACLID := strings.TrimSpace(distribution.WebACLID)
	if webACLID == "" {
		return aws.RelationshipObservation{}, false
	}
	webACLARN := ""
	if isARN(webACLID) {
		webACLARN = webACLID
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCloudFrontDistributionUsesWAFWebACL,
		SourceResourceID: distributionID,
		SourceARN:        strings.TrimSpace(distribution.ARN),
		TargetResourceID: webACLID,
		TargetARN:        webACLARN,
		TargetType:       "aws_waf_web_acl",
		Attributes: map[string]any{
			"distribution_id": strings.TrimSpace(distribution.ID),
		},
		SourceRecordID: distributionID + "->" + webACLID,
	}, true
}
