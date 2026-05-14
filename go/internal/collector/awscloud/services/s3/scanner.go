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
	case "":
		boundary.ServiceKind = awscloud.ServiceS3
	case awscloud.ServiceS3:
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
	arn := firstNonEmpty(bucket.ARN, arnForBucket(name))
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
	sourceARN := firstNonEmpty(bucket.ARN, arnForBucket(sourceName))
	targetName := strings.TrimSpace(bucket.Logging.TargetBucket)
	if sourceARN == "" || targetName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetARN := arnForBucket(targetName)
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

func arnForBucket(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "arn:aws:s3:::") {
		return name
	}
	return "arn:aws:s3:::" + name
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
