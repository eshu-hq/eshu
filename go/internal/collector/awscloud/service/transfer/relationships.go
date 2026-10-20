// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transfer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// serverRelationships returns every reported relationship for one Transfer
// server: VPC endpoint placement, Elastic IP attachments, FTPS ACM certificate,
// CloudWatch Logs structured-log destinations, and the CloudWatch logging IAM
// role. Each edge is emitted only when AWS reports the join key in the shape the
// target scanner publishes.
func serverRelationships(boundary awscloud.Boundary, server Server) []awscloud.RelationshipObservation {
	// Source the edges on the identity the server resource node publishes as its
	// resource_id (ARN-preferred, matching serverObservation). Using the bare
	// ServerID here would dangle every server->* edge whenever the server ARN is
	// present (the common case), because it would not join the emitted
	// aws_transfer_server node.
	serverID := firstNonEmpty(strings.TrimSpace(server.ARN), strings.TrimSpace(server.ServerID))
	if serverID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation

	if endpoint := serverVPCEndpointRelationship(boundary, serverID, server); endpoint != nil {
		observations = append(observations, *endpoint)
	}
	observations = append(observations, serverElasticIPRelationships(boundary, serverID, server)...)
	if certificate := serverACMCertificateRelationship(boundary, serverID, server); certificate != nil {
		observations = append(observations, *certificate)
	}
	if role := serverLoggingRoleRelationship(boundary, serverID, server); role != nil {
		observations = append(observations, *role)
	}
	observations = append(observations, serverLogGroupRelationships(boundary, serverID, server)...)
	return observations
}

// serverVPCEndpointRelationship records the interface VPC endpoint a VPC-hosted
// server publishes. The VPC scanner keys a VPC endpoint by its bare ID
// (vpce-...), so the edge is keyed by the bare ID and carries no target ARN.
func serverVPCEndpointRelationship(boundary awscloud.Boundary, serverID string, server Server) *awscloud.RelationshipObservation {
	endpointID := strings.TrimSpace(server.VPCEndpointID)
	if endpointID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransferServerUsesVPCEndpoint,
		SourceResourceID: serverID,
		SourceARN:        strings.TrimSpace(server.ARN),
		TargetResourceID: endpointID,
		TargetType:       awscloud.ResourceTypeVPCEndpoint,
		SourceRecordID:   serverID + "->" + awscloud.RelationshipTransferServerUsesVPCEndpoint + ":" + endpointID,
	}
}

// serverElasticIPRelationships records each Elastic IP allocation attached to a
// VPC endpoint server. The VPC scanner keys an Elastic IP by its bare
// allocation ID (eipalloc-...), so each edge is keyed by the bare ID and
// carries no target ARN.
func serverElasticIPRelationships(boundary awscloud.Boundary, serverID string, server Server) []awscloud.RelationshipObservation {
	if len(server.AddressAllocationIDs) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(server.AddressAllocationIDs))
	seen := make(map[string]struct{}, len(server.AddressAllocationIDs))
	for _, allocation := range server.AddressAllocationIDs {
		allocationID := strings.TrimSpace(allocation)
		if allocationID == "" {
			continue
		}
		if _, ok := seen[allocationID]; ok {
			continue
		}
		seen[allocationID] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransferServerUsesElasticIP,
			SourceResourceID: serverID,
			SourceARN:        strings.TrimSpace(server.ARN),
			TargetResourceID: allocationID,
			TargetType:       awscloud.ResourceTypeVPCElasticIP,
			SourceRecordID:   serverID + "->" + awscloud.RelationshipTransferServerUsesElasticIP + ":" + allocationID,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// serverACMCertificateRelationship records the ACM certificate backing FTPS.
// ACM publishes a certificate's resource_id as its ARN, and Transfer reports
// the certificate as an ARN, so the edge is ARN-keyed on both sides.
func serverACMCertificateRelationship(boundary awscloud.Boundary, serverID string, server Server) *awscloud.RelationshipObservation {
	certificateARN := strings.TrimSpace(server.CertificateARN)
	if !isARN(certificateARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransferServerUsesACMCertificate,
		SourceResourceID: serverID,
		SourceARN:        strings.TrimSpace(server.ARN),
		TargetResourceID: certificateARN,
		TargetARN:        certificateARN,
		TargetType:       awscloud.ResourceTypeACMCertificate,
		SourceRecordID:   serverID + "->" + awscloud.RelationshipTransferServerUsesACMCertificate + ":" + certificateARN,
	}
}

// serverLoggingRoleRelationship records the IAM role Transfer assumes to deliver
// CloudWatch logs. IAM publishes a role's resource_id as its ARN, so the edge is
// ARN-keyed when AWS reports an ARN-shaped role.
func serverLoggingRoleRelationship(boundary awscloud.Boundary, serverID string, server Server) *awscloud.RelationshipObservation {
	roleARN := strings.TrimSpace(server.LoggingRoleARN)
	if !isARN(roleARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransferServerUsesLoggingRole,
		SourceResourceID: serverID,
		SourceARN:        strings.TrimSpace(server.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   serverID + "->" + awscloud.RelationshipTransferServerUsesLoggingRole + ":" + roleARN,
	}
}

// serverLogGroupRelationships records each CloudWatch Logs log group receiving
// the server's structured logs. The CloudWatch Logs scanner keys a log group by
// its ARN, and Transfer reports structured-log destinations as ARNs, so each
// edge is ARN-keyed.
func serverLogGroupRelationships(boundary awscloud.Boundary, serverID string, server Server) []awscloud.RelationshipObservation {
	if len(server.StructuredLogDestinations) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(server.StructuredLogDestinations))
	seen := make(map[string]struct{}, len(server.StructuredLogDestinations))
	for _, destination := range server.StructuredLogDestinations {
		logGroupARN := strings.TrimSpace(destination)
		if !isARN(logGroupARN) {
			continue
		}
		if _, ok := seen[logGroupARN]; ok {
			continue
		}
		seen[logGroupARN] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipTransferServerLogsToLogGroup,
			SourceResourceID: serverID,
			SourceARN:        strings.TrimSpace(server.ARN),
			TargetResourceID: logGroupARN,
			TargetARN:        logGroupARN,
			TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
			SourceRecordID:   serverID + "->" + awscloud.RelationshipTransferServerLogsToLogGroup + ":" + logGroupARN,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// userRelationships returns every reported relationship for one Transfer user:
// the IAM access role and the S3 bucket or EFS file system backing the home
// directory. Home-directory edges carry only the path; object and file contents
// are never read.
func userRelationships(boundary awscloud.Boundary, user User) []awscloud.RelationshipObservation {
	userID := userResourceID(user)
	if userID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if role := userRoleRelationship(boundary, userID, user); role != nil {
		observations = append(observations, *role)
	}
	if home := userHomeDirectoryRelationship(boundary, userID, user); home != nil {
		observations = append(observations, *home)
	}
	return observations
}

// userRoleRelationship records the IAM access role a user assumes. IAM publishes
// a role's resource_id as its ARN, so the edge is ARN-keyed when AWS reports an
// ARN-shaped role.
func userRoleRelationship(boundary awscloud.Boundary, userID string, user User) *awscloud.RelationshipObservation {
	roleARN := strings.TrimSpace(user.RoleARN)
	if !isARN(roleARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransferUserUsesIAMRole,
		SourceResourceID: userID,
		SourceARN:        strings.TrimSpace(user.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   userID + "->" + awscloud.RelationshipTransferUserUsesIAMRole + ":" + roleARN,
	}
}

// userHomeDirectoryRelationship records the S3 bucket or EFS file system backing
// a user's home directory. AWS reports the home directory as a bare POSIX path
// (`/bucket/prefix` for S3, `/fs-id/path` for EFS), so the scanner derives the
// leading path segment and synthesizes the partition-aware target ARN the S3 or
// EFS scanner publishes. Only the path is recorded; the scanner never reads
// object or file contents.
func userHomeDirectoryRelationship(boundary awscloud.Boundary, userID string, user User) *awscloud.RelationshipObservation {
	segment, remainder, ok := firstPathSegment(user.HomeDirectory)
	if !ok {
		return nil
	}
	if looksLikeEFSFileSystemID(segment) {
		return userEFSHomeDirectoryRelationship(boundary, userID, user, segment, remainder)
	}
	return userS3HomeDirectoryRelationship(boundary, userID, user, segment, remainder)
}

// userS3HomeDirectoryRelationship synthesizes the S3 bucket ARN the S3 scanner
// publishes (`arn:<partition>:s3:::<bucket>`) and keys the edge by it.
func userS3HomeDirectoryRelationship(
	boundary awscloud.Boundary,
	userID string,
	user User,
	bucket string,
	prefix string,
) *awscloud.RelationshipObservation {
	bucketARN := "arn:" + partition(boundary) + ":s3:::" + bucket
	attributes := map[string]any{
		"home_directory": strings.TrimSpace(user.HomeDirectory),
		"bucket":         bucket,
	}
	if prefix != "" {
		attributes["object_key_prefix"] = prefix
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransferUserHomeDirectoryInS3Bucket,
		SourceResourceID: userID,
		SourceARN:        strings.TrimSpace(user.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   userID + "->" + awscloud.RelationshipTransferUserHomeDirectoryInS3Bucket + ":" + bucketARN,
	}
}

// userEFSHomeDirectoryRelationship synthesizes the EFS file-system ARN the EFS
// scanner publishes
// (`arn:<partition>:elasticfilesystem:<region>:<account>:file-system/<fs-id>`)
// and keys the edge by it.
func userEFSHomeDirectoryRelationship(
	boundary awscloud.Boundary,
	userID string,
	user User,
	fileSystemID string,
	path string,
) *awscloud.RelationshipObservation {
	region := strings.TrimSpace(boundary.Region)
	account := strings.TrimSpace(boundary.AccountID)
	if region == "" || account == "" {
		// Without a region and account the EFS scanner's ARN cannot be
		// reconstructed, so the edge would dangle; skip it rather than emit a
		// malformed join key.
		return nil
	}
	fileSystemARN := "arn:" + partition(boundary) + ":elasticfilesystem:" + region + ":" + account + ":file-system/" + fileSystemID
	attributes := map[string]any{
		"home_directory": strings.TrimSpace(user.HomeDirectory),
		"file_system_id": fileSystemID,
	}
	if path != "" {
		attributes["path_prefix"] = path
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipTransferUserHomeDirectoryInEFSFileSystem,
		SourceResourceID: userID,
		SourceARN:        strings.TrimSpace(user.ARN),
		TargetResourceID: fileSystemARN,
		TargetARN:        fileSystemARN,
		TargetType:       awscloud.ResourceTypeEFSFileSystem,
		Attributes:       attributes,
		SourceRecordID:   userID + "->" + awscloud.RelationshipTransferUserHomeDirectoryInEFSFileSystem + ":" + fileSystemARN,
	}
}

// userResourceID returns the stable identity for a Transfer user. The user ARN
// is preferred because it is globally unique; when AWS omits it the scanner
// falls back to a `<server-id>/<user-name>` composite so the user still has a
// stable identity within its server.
func userResourceID(user User) string {
	if arn := strings.TrimSpace(user.ARN); arn != "" {
		return arn
	}
	serverID := strings.TrimSpace(user.ServerID)
	userName := strings.TrimSpace(user.UserName)
	switch {
	case serverID != "" && userName != "":
		return serverID + "/" + userName
	case userName != "":
		return userName
	default:
		return ""
	}
}
