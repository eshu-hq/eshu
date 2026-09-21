// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rolesanywhere

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// profileRoleRelationships records a Roles Anywhere profile's reported
// dependency on the IAM roles it can vend session credentials for. AWS reports
// each role as an ARN, which matches how the IAM scanner publishes its role
// resource_id, so each edge joins the IAM role node exactly. Empty or duplicate
// role ARNs are skipped so no edge dangles or duplicates.
func profileRoleRelationships(boundary aws.Boundary, profile Profile) []aws.RelationshipObservation {
	sourceID := profileResourceID(profile)
	if sourceID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	seen := make(map[string]struct{}, len(profile.RoleARNs))
	for _, roleARN := range profile.RoleARNs {
		roleARN = strings.TrimSpace(roleARN)
		if roleARN == "" {
			continue
		}
		if _, dup := seen[roleARN]; dup {
			continue
		}
		seen[roleARN] = struct{}{}
		targetARN := ""
		if isARN(roleARN) {
			targetARN = roleARN
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipRolesAnywhereProfileAssumesRole,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(profile.ARN),
			TargetResourceID: roleARN,
			TargetARN:        targetARN,
			TargetType:       aws.ResourceTypeIAMRole,
			SourceRecordID:   sourceID + "->" + aws.RelationshipRolesAnywhereProfileAssumesRole + ":" + roleARN,
		})
	}
	return observations
}

// trustAnchorACMPCARelationship records a Roles Anywhere trust anchor's reported
// dependency on an AWS Private CA (ACM PCA) certificate authority. It is emitted
// only for trust anchors whose source is AWS_ACM_PCA and that report a CA ARN.
// AWS reports the CA ARN, which matches how the acmpca scanner publishes its
// certificate-authority resource_id, so the edge joins the CA node. It returns
// nil when the trust anchor is not ACM-PCA-backed or no CA ARN is reported.
func trustAnchorACMPCARelationship(boundary aws.Boundary, anchor TrustAnchor) *aws.RelationshipObservation {
	caARN := strings.TrimSpace(anchor.ACMPCAArn)
	if caARN == "" || !isARN(caARN) {
		return nil
	}
	sourceID := trustAnchorResourceID(anchor)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipRolesAnywhereTrustAnchorUsesACMPCA,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(anchor.ARN),
		TargetResourceID: caARN,
		TargetARN:        caARN,
		TargetType:       aws.ResourceTypeACMPCACertificateAuthority,
		SourceRecordID:   sourceID + "->" + aws.RelationshipRolesAnywhereTrustAnchorUsesACMPCA + ":" + caARN,
	}
}

// crlTrustAnchorRelationship records that an imported certificate revocation
// list (CRL) provides revocation for a trust anchor. AWS reports the trust
// anchor ARN on the CRL, which is the resource_id the trust-anchor node
// publishes, so the edge joins the trust-anchor node. It returns nil when no
// trust anchor is associated.
func crlTrustAnchorRelationship(boundary aws.Boundary, crl CRL) *aws.RelationshipObservation {
	targetID := strings.TrimSpace(crl.TrustAnchorARN)
	if targetID == "" {
		return nil
	}
	sourceID := crlResourceID(crl)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipRolesAnywhereCRLValidatesTrustAnchor,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(crl.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeRolesAnywhereTrustAnchor,
		SourceRecordID:   sourceID + "->" + aws.RelationshipRolesAnywhereCRLValidatesTrustAnchor + ":" + targetID,
	}
}
