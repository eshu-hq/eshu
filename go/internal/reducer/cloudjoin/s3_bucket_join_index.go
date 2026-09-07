// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudjoin

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// s3BucketARNInfix marks the start of the bucket-name tail in an S3 bucket
// ARN. S3 ARNs are arn:aws:s3:::<name> (partition-bearing in the second
// segment, NO account or region), so the bucket name is everything after this
// ":::" infix. Splitting on the infix tolerates the aws / aws-cn /
// aws-us-gov partitions.
const s3BucketARNInfix = ":::"

// S3BucketJoinIndex resolves an S3 bucket NAME to a scanned S3 bucket
// CloudResource node uid. It is built once per scope generation from the
// aws_resource S3 bucket facts so resolution is O(1) per posture fact — no
// per-edge graph round trip and no N+1 Cypher.
//
// It indexes only aws_s3_bucket resources, because every consumer joins a
// bucket endpoint. A name absent from the index did not scan as a bucket node
// and resolves to no edge — the trust-boundary rule, never fabricated. Each
// entry is derived from an aws_resource fact that carried its own
// account_id/region, so a cross-account log bucket resolves only if that
// account's bucket was scanned in the same scope.
//
// Three S3 slices share this index (issue #6061): the s3logsto LOGS_TO edge
// projection, the s3grant external-principal grant projection, and the S3
// internet-exposure node projection. It lives here, below all three, because
// a family package may never import the reducer root and families may never
// import each other laterally.
type S3BucketJoinIndex struct {
	byName map[string]string
}

// BuildS3BucketJoinIndex builds the bounded in-memory index from the scope
// generation's aws_resource fact envelopes, keeping only aws_s3_bucket
// resources and keying each by its bucket name.
func BuildS3BucketJoinIndex(envelopes []facts.Envelope) (S3BucketJoinIndex, []factdecode.QuarantinedFact, error) {
	index := S3BucketJoinIndex{byName: make(map[string]string, len(envelopes))}
	var quarantined []factdecode.QuarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resource, err := schemadecode.DecodeAWSResource(env)
		if err != nil {
			q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
			if fatal != nil {
				return S3BucketJoinIndex{}, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if resource.ResourceType != awsv1.ResourceTypeS3Bucket {
			continue
		}
		arn := payloadcore.DerefString(resource.ARN)
		resourceID := resource.ResourceID
		if resourceID == "" {
			resourceID = arn
		}
		if resourceID == "" {
			continue
		}
		name := s3BucketName(resource)
		if name == "" {
			continue
		}
		uid := CloudResourceUID(resource.AccountID, resource.Region, awsv1.ResourceTypeS3Bucket, resourceID)
		// First writer wins on collision so a later duplicate cannot re-point a
		// name to a different node. S3 names are globally unique, so a collision
		// means duplicate facts for the same bucket.
		if _, exists := index.byName[name]; !exists {
			index.byName[name] = uid
		}
	}
	return index, quarantined, nil
}

// Resolve looks up a bucket name and returns the scanned node uid on an exact
// name hit.
func (i S3BucketJoinIndex) Resolve(name string) (string, bool) {
	uid, ok := i.byName[strings.TrimSpace(name)]
	return uid, ok
}

// s3BucketName derives the bucket name from a decoded aws_resource S3 bucket
// struct: the node's Name field first, then the tail of its arn:aws:s3:::<name>
// ARN (or ResourceID), then the s3:// correlation anchor. Returning the
// canonical name lets the by-name index match a bare logging_target_bucket
// value. It reads only the typed struct, never the raw envelope payload.
func s3BucketName(resource awsv1.Resource) string {
	if name := strings.TrimSpace(payloadcore.DerefString(resource.Name)); name != "" {
		return name
	}
	if name := S3BucketNameFromARN(payloadcore.DerefString(resource.ARN)); name != "" {
		return name
	}
	if name := S3BucketNameFromARN(resource.ResourceID); name != "" {
		return name
	}
	// uniqueSortedStrings preserves the pre-typing byte-identical result: the old
	// payloadStrings(env.Payload, "", "correlation_anchors") sorted the anchors,
	// so when a bucket carries more than one s3:// anchor the first match is
	// deterministic by sort order, not raw emit order.
	for _, anchor := range payloadcore.UniqueSortedStrings(resource.CorrelationAnchors) {
		if strings.HasPrefix(anchor, "s3://") {
			if name := strings.TrimSpace(strings.TrimPrefix(anchor, "s3://")); name != "" {
				return name
			}
		}
	}
	return ""
}

// S3BucketNameFromARN returns the bucket name from an arn:aws:s3:::<name> ARN
// (any partition), or "" when the value is not an S3 bucket ARN.
func S3BucketNameFromARN(arn string) string {
	arn = strings.TrimSpace(arn)
	if !strings.HasPrefix(arn, "arn:") {
		return ""
	}
	idx := strings.Index(arn, s3BucketARNInfix)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(arn[idx+len(s3BucketARNInfix):])
}

// S3PostureBucketName derives the source bucket name from a decoded
// s3_bucket_posture struct: the BucketName field first, then the tail of its
// BucketArn. The posture fact always carries at least one of these by
// construction (NewS3BucketPostureEnvelope requires bucket_arn or bucket_name),
// which is why both are optional either-or fields on the struct.
func S3PostureBucketName(posture awsv1.S3BucketPosture) string {
	if name := strings.TrimSpace(payloadcore.DerefString(posture.BucketName)); name != "" {
		return name
	}
	return S3BucketNameFromARN(payloadcore.DerefString(posture.BucketARN))
}
