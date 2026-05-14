package cloudfront

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func distributionRelationships(
	boundary awscloud.Boundary,
	distribution Distribution,
) []awscloud.RelationshipObservation {
	distributionID := distributionResourceID(distribution)
	if distributionID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	if relationship, ok := acmCertificateRelationship(boundary, distribution, distributionID); ok {
		relationships = append(relationships, relationship)
	}
	if relationship, ok := wafWebACLRelationship(boundary, distribution, distributionID); ok {
		relationships = append(relationships, relationship)
	}
	return relationships
}

func acmCertificateRelationship(
	boundary awscloud.Boundary,
	distribution Distribution,
	distributionID string,
) (awscloud.RelationshipObservation, bool) {
	certificateARN := strings.TrimSpace(distribution.ViewerCertificate.ACMCertificateARN)
	if certificateARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudFrontDistributionUsesACMCertificate,
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
	boundary awscloud.Boundary,
	distribution Distribution,
	distributionID string,
) (awscloud.RelationshipObservation, bool) {
	webACLID := strings.TrimSpace(distribution.WebACLID)
	if webACLID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	webACLARN := ""
	if isARN(webACLID) {
		webACLARN = webACLID
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudFrontDistributionUsesWAFWebACL,
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
