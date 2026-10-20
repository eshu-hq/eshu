// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rolesanywhere

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// profileRoleRelationships records a Roles Anywhere profile's reported
// dependency on the IAM roles it can vend session credentials for. AWS reports
// each role as an ARN, which matches how the IAM scanner publishes its role
// resource_id, so each edge joins the IAM role node exactly. Empty or duplicate
// role ARNs are skipped so no edge dangles or duplicates.
func profileRoleRelationships(boundary awscloud.Boundary, profile Profile) []awscloud.RelationshipObservation {
	sourceID := profileResourceID(profile)
	if sourceID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
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
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipRolesAnywhereProfileAssumesRole,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(profile.ARN),
			TargetResourceID: roleARN,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipRolesAnywhereProfileAssumesRole + ":" + roleARN,
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
func trustAnchorACMPCARelationship(boundary awscloud.Boundary, anchor TrustAnchor) *awscloud.RelationshipObservation {
	caARN := strings.TrimSpace(anchor.ACMPCAArn)
	if caARN == "" || !isARN(caARN) {
		return nil
	}
	sourceID := trustAnchorResourceID(anchor)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRolesAnywhereTrustAnchorUsesACMPCA,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(anchor.ARN),
		TargetResourceID: caARN,
		TargetARN:        caARN,
		TargetType:       awscloud.ResourceTypeACMPCACertificateAuthority,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipRolesAnywhereTrustAnchorUsesACMPCA + ":" + caARN,
	}
}

// crlTrustAnchorRelationship records that an imported certificate revocation
// list (CRL) provides revocation for a trust anchor. AWS reports the trust
// anchor ARN on the CRL, which is the resource_id the trust-anchor node
// publishes, so the edge joins the trust-anchor node. It returns nil when no
// trust anchor is associated.
func crlTrustAnchorRelationship(boundary awscloud.Boundary, crl CRL) *awscloud.RelationshipObservation {
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
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRolesAnywhereCRLValidatesTrustAnchor,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(crl.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeRolesAnywhereTrustAnchor,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipRolesAnywhereCRLValidatesTrustAnchor + ":" + targetID,
	}
}
