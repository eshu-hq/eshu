// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS S3 bucket metadata facts for one claimed account and
// region. It never reads objects, persists bucket policy JSON, records ACL
// grants, or calls mutation APIs.
type Scanner struct {
	Client Client
}

// Scan observes S3 buckets through the configured metadata-only client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("s3 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceS3:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceS3
	default:
		return nil, fmt.Errorf("s3 scanner received service_kind %q", boundary.ServiceKind)
	}

	buckets, err := s.Client.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list S3 buckets: %w", err)
	}
	var envelopes []facts.Envelope
	for _, bucket := range buckets {
		resource, err := awscloud.NewResourceEnvelope(bucketObservation(boundary, bucket))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		posture, err := awscloud.NewS3BucketPostureEnvelope(bucketPostureObservation(boundary, bucket))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, posture)
		for _, observation := range externalPrincipalGrantObservations(boundary, bucket) {
			grant, err := awscloud.NewS3ExternalPrincipalGrantEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, grant)
		}
		for _, observation := range resourcePolicyPermissionObservations(boundary, bucket) {
			permission, err := awscloud.NewResourcePolicyPermissionEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, permission)
		}
		logging, ok := loggingRelationship(boundary, bucket)
		if !ok {
			continue
		}
		relationship, err := awscloud.NewRelationshipEnvelope(logging)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func bucketObservation(boundary awscloud.Boundary, bucket Bucket) awscloud.ResourceObservation {
	name := strings.TrimSpace(bucket.Name)
	arn := firstNonEmpty(bucket.ARN, arnForBucket(awscloud.PartitionForBoundary(boundary), name))
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   firstNonEmpty(arn, name),
		ResourceType: awscloud.ResourceTypeS3Bucket,
		Name:         name,
		Tags:         cloneStringMap(bucket.Tags),
		Attributes:   bucketAttributes(bucket),
		CorrelationAnchors: []string{
			arn,
			name,
			"s3://" + name,
		},
		SourceRecordID: firstNonEmpty(arn, name),
	}
}

func bucketAttributes(bucket Bucket) map[string]any {
	algorithms, keyIDs, bucketKeyEnabled := encryptionSummary(bucket.Encryption)
	return map[string]any{
		"bucket_region":                 strings.TrimSpace(bucket.Region),
		"creation_time":                 timeOrNil(bucket.CreationTime),
		"versioning_status":             strings.TrimSpace(bucket.Versioning.Status),
		"mfa_delete":                    strings.TrimSpace(bucket.Versioning.MFADelete),
		"default_encryption_algorithms": algorithms,
		"kms_master_key_ids":            keyIDs,
		"bucket_key_enabled":            bucketKeyEnabled,
		"block_public_acls":             boolOrNil(bucket.PublicAccessBlock.BlockPublicACLs),
		"ignore_public_acls":            boolOrNil(bucket.PublicAccessBlock.IgnorePublicACLs),
		"block_public_policy":           boolOrNil(bucket.PublicAccessBlock.BlockPublicPolicy),
		"restrict_public_buckets":       boolOrNil(bucket.PublicAccessBlock.RestrictPublicBuckets),
		"policy_is_public":              boolOrNil(bucket.PolicyIsPublic),
		"ownership_controls":            cloneStrings(bucket.OwnershipControls),
		"website_enabled":               bucket.Website.Enabled,
		"website_has_index_document":    bucket.Website.HasIndexDocument,
		"website_has_error_document":    bucket.Website.HasErrorDocument,
		"website_redirect_host_name":    strings.TrimSpace(bucket.Website.RedirectAllRequestsTo),
		"website_routing_rule_count":    bucket.Website.RoutingRuleCount,
		"logging_enabled":               bucket.Logging.Enabled,
		"logging_target_bucket":         strings.TrimSpace(bucket.Logging.TargetBucket),
		"logging_target_prefix":         strings.TrimSpace(bucket.Logging.TargetPrefix),
	}
}

// bucketPostureObservation derives the metadata-only security posture for one
// bucket: block-public-access flags, default-encryption detail, versioning and
// MFA-delete state, object ownership / ACL-disabled state, access-logging
// target, replication presence, and the policy-derived booleans. It never
// reads or stores the raw bucket policy document.
func bucketPostureObservation(boundary awscloud.Boundary, bucket Bucket) awscloud.S3BucketPostureObservation {
	name := strings.TrimSpace(bucket.Name)
	arn := firstNonEmpty(bucket.ARN, arnForBucket(awscloud.PartitionForBoundary(boundary), name))
	algorithms, keyIDs, bucketKeyEnabled := encryptionSummary(bucket.Encryption)
	return awscloud.S3BucketPostureObservation{
		Boundary:   boundary,
		BucketARN:  arn,
		BucketName: name,

		BlockPublicACLs:             bucket.PublicAccessBlock.BlockPublicACLs,
		IgnorePublicACLs:            bucket.PublicAccessBlock.IgnorePublicACLs,
		BlockPublicPolicy:           bucket.PublicAccessBlock.BlockPublicPolicy,
		RestrictPublicBuckets:       bucket.PublicAccessBlock.RestrictPublicBuckets,
		BlockPublicAccessAllEnabled: blockPublicAccessAll(bucket.PublicAccessBlock),

		DefaultEncryptionEnabled: len(algorithms) > 0,
		EncryptionAlgorithms:     algorithms,
		SSEKMSKeyARN:             firstNonEmpty(keyIDs...),
		BucketKeyEnabled:         bucketKeyEnabled,

		VersioningStatus:  strings.TrimSpace(bucket.Versioning.Status),
		VersioningEnabled: strings.EqualFold(strings.TrimSpace(bucket.Versioning.Status), "Enabled"),
		MFADeleteEnabled:  strings.EqualFold(strings.TrimSpace(bucket.Versioning.MFADelete), "Enabled"),

		ObjectOwnership: cloneStrings(bucket.OwnershipControls),
		ACLDisabled:     ownershipDisablesACL(bucket.OwnershipControls),

		LoggingEnabled:      bucket.Logging.Enabled,
		LoggingTargetBucket: strings.TrimSpace(bucket.Logging.TargetBucket),

		ReplicationEnabled: bucket.Replication.Enabled,

		PolicyPresent:            bucket.PolicyPresent,
		PolicyGrantsPublic:       bucket.PolicyGrantsPublic,
		PolicyGrantsCrossAccount: bucket.PolicyGrantsCrossAccount,
	}
}

func externalPrincipalGrantObservations(
	boundary awscloud.Boundary,
	bucket Bucket,
) []awscloud.S3ExternalPrincipalGrantObservation {
	if len(bucket.ExternalPrincipalGrants) == 0 {
		return nil
	}
	name := strings.TrimSpace(bucket.Name)
	arn := firstNonEmpty(bucket.ARN, arnForBucket(awscloud.PartitionForBoundary(boundary), name))
	observations := make([]awscloud.S3ExternalPrincipalGrantObservation, 0, len(bucket.ExternalPrincipalGrants))
	for _, grant := range bucket.ExternalPrincipalGrants {
		observations = append(observations, awscloud.S3ExternalPrincipalGrantObservation{
			Boundary:           boundary,
			BucketARN:          arn,
			BucketName:         name,
			PrincipalKind:      grant.PrincipalKind,
			PrincipalValue:     grant.PrincipalValue,
			PrincipalAccountID: grant.PrincipalAccountID,
			PrincipalPartition: grant.PrincipalPartition,
			PrincipalService:   grant.PrincipalService,
			GrantOutcome:       grant.GrantOutcome,
			Public:             grant.Public,
			CrossAccount:       grant.CrossAccount,
			ServicePrincipal:   grant.ServicePrincipal,
			Unsupported:        grant.Unsupported,
			UnsupportedKey:     grant.UnsupportedKey,
			SourceStatementID:  grant.SourceStatementID,
			SourceURI:          s3BucketURI(name),
		})
	}
	return observations
}

// resourcePolicyPermissionObservations maps the bucket's normalized resource
// policy statements into aws_resource_policy_permission observations. The
// statements arrive already derived on the Bucket model; this package never sees
// the raw policy document. A bucket with no attached policy carries no
// statements, so it emits no fact.
func resourcePolicyPermissionObservations(
	boundary awscloud.Boundary,
	bucket Bucket,
) []awscloud.ResourcePolicyPermissionObservation {
	if len(bucket.ResourcePolicyStatements) == 0 {
		return nil
	}
	name := strings.TrimSpace(bucket.Name)
	arn := firstNonEmpty(bucket.ARN, arnForBucket(awscloud.PartitionForBoundary(boundary), name))
	observations := make([]awscloud.ResourcePolicyPermissionObservation, 0, len(bucket.ResourcePolicyStatements))
	for _, statement := range bucket.ResourcePolicyStatements {
		observations = append(observations, awscloud.ResourcePolicyPermissionObservation{
			Boundary:            boundary,
			ResourceARN:         arn,
			ResourceType:        awscloud.ResourceTypeS3Bucket,
			StatementSID:        statement.StatementSID,
			Effect:              statement.Effect,
			Actions:             statement.Actions,
			NotActions:          statement.NotActions,
			Resources:           statement.Resources,
			NotResources:        statement.NotResources,
			ConditionKeys:       statement.ConditionKeys,
			ConditionOperators:  statement.ConditionOperators,
			PrincipalAccountIDs: statement.PrincipalAccountIDs,
			PrincipalARNs:       statement.PrincipalARNs,
			PrincipalTypes:      statement.PrincipalTypes,
			IsPublic:            statement.IsPublic,
			IsCrossAccount:      statement.IsCrossAccount,
			SourceURI:           s3BucketURI(name),
		})
	}
	return observations
}

// blockPublicAccessAll reports whether all four block-public-access flags are
// explicitly enabled. It returns nil when any flag is absent so an
// unconfigured bucket stays distinct from one with all four disabled.
func blockPublicAccessAll(block PublicAccessBlock) *bool {
	flags := []*bool{
		block.BlockPublicACLs,
		block.IgnorePublicACLs,
		block.BlockPublicPolicy,
		block.RestrictPublicBuckets,
	}
	all := true
	for _, flag := range flags {
		if flag == nil {
			return nil
		}
		all = all && *flag
	}
	return &all
}

// ownershipDisablesACL reports whether the bucket's object-ownership controls
// disable ACLs (BucketOwnerEnforced).
func ownershipDisablesACL(controls []string) bool {
	for _, control := range controls {
		if strings.EqualFold(strings.TrimSpace(control), "BucketOwnerEnforced") {
			return true
		}
	}
	return false
}

func encryptionSummary(encryption Encryption) ([]string, []string, bool) {
	var algorithms []string
	var keyIDs []string
	var bucketKeyEnabled bool
	for _, rule := range encryption.Rules {
		if algorithm := strings.TrimSpace(rule.Algorithm); algorithm != "" {
			algorithms = append(algorithms, algorithm)
		}
		if keyID := strings.TrimSpace(rule.KMSMasterKeyID); keyID != "" {
			keyIDs = append(keyIDs, keyID)
		}
		bucketKeyEnabled = bucketKeyEnabled || rule.BucketKey
	}
	return algorithms, keyIDs, bucketKeyEnabled
}

func loggingRelationship(
	boundary awscloud.Boundary,
	bucket Bucket,
) (awscloud.RelationshipObservation, bool) {
	sourceName := strings.TrimSpace(bucket.Name)
	sourceARN := firstNonEmpty(bucket.ARN, arnForBucket(awscloud.PartitionForBoundary(boundary), sourceName))
	targetName := strings.TrimSpace(bucket.Logging.TargetBucket)
	if sourceARN == "" || targetName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetARN := arnForBucket(awscloud.PartitionForBoundary(boundary), targetName)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipS3BucketLogsToBucket,
		SourceResourceID: sourceARN,
		SourceARN:        sourceARN,
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes: map[string]any{
			"target_prefix": strings.TrimSpace(bucket.Logging.TargetPrefix),
		},
		SourceRecordID: sourceARN + "->logging:" + targetName,
	}, true
}

// arnForBucket synthesizes the S3 bucket ARN for the given partition, or returns
// an already-formed ARN unchanged. S3 buckets have no API ARN, so the scanner
// synthesizes one carrying the boundary partition (aws / aws-cn / aws-us-gov) so
// the node identity and bucket->bucket targets match what partition-aware
// consumers join against. This is the fallback when the SDK adapter did not
// already supply a (partition-aware) ARN.
func arnForBucket(partition, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "arn:") {
		return name
	}
	return "arn:" + partition + ":s3:::" + name
}

func s3BucketURI(bucketName string) string {
	bucketName = strings.TrimSpace(bucketName)
	if bucketName == "" {
		return ""
	}
	return "s3://" + bucketName
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func boolOrNil(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
