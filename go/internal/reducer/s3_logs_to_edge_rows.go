// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// s3LogsToResourceTypeBucket is the aws_resource resource_type the S3 scanner
// emits for a bucket node. It mirrors awscloud.ResourceTypeS3Bucket; the
// duplication is intentional so the reducer does not import the collector
// package for one string constant.
const s3LogsToResourceTypeBucket = "aws_s3_bucket"

// s3LogsToARNInfix marks the start of the bucket-name tail in an S3 bucket ARN.
// S3 ARNs are arn:aws:s3:::<name> (partition-bearing in the second segment, NO
// account or region), so the bucket name is everything after this ":::" infix.
// Splitting on the infix tolerates the aws / aws-cn / aws-us-gov partitions.
const s3LogsToARNInfix = ":::"

// s3LogsToRelationshipType is the closed single-member relationship vocabulary
// this slice projects. It is the static token the cypher writer interpolates
// into the relationship-type position after validation.
const s3LogsToRelationshipType = string(edgetype.LogsTo)

// s3LogsToModeName is the only resolution mode for the LOGS_TO edge counter:
// the target log bucket is resolved by bucket-name equality against the
// in-memory join index. S3 bucket names are globally unique, so name equality
// is a precise identity, not a heuristic.
const s3LogsToModeName = "name"

// Skip reasons for the bounded completion-log tally. Each posture fact that
// names a log target but produces no edge is counted under exactly one reason so
// an operator can see why LOGS_TO edges were lost without a per-edge log line.
// A blank logging_target_bucket (logging disabled) is NOT counted here — it is
// the normal no-edge state, not a lost edge.
const (
	// s3LogsToSkipSourceUnresolved: the posture fact's own bucket did not
	// resolve to a scanned S3 CloudResource node, so the statement cannot anchor
	// an edge. Counted once.
	s3LogsToSkipSourceUnresolved = "source_unresolved"
	// s3LogsToSkipTargetUnresolved: logging_target_bucket named a bucket that was
	// not scanned as an S3 CloudResource node in this scope generation
	// (cross-account central log account, out-of-scope region). The
	// trust-boundary rule — no dangling node, no fabrication.
	s3LogsToSkipTargetUnresolved = "target_unresolved"
)

// s3LogsToEdgeTally is the bounded, honest accounting surface for the LOGS_TO
// projection. The metric counts materialized edges by resolution_mode; the
// completion log keeps the skip-reason breakdown so an operator can answer
// "which buckets are losing LOGS_TO edges, and why?" without a per-edge log
// line.
type s3LogsToEdgeTally struct {
	// resolved counts materialized edges keyed by resolution mode (name) for the
	// metric and the completion log's resolved field.
	resolved map[string]int
	// skipped counts posture facts that named a log target but produced no edge,
	// keyed by the closed skip-reason set, for the completion log.
	skipped map[string]int
}

func newS3LogsToEdgeTally() s3LogsToEdgeTally {
	return s3LogsToEdgeTally{
		resolved: make(map[string]int),
		skipped:  make(map[string]int),
	}
}

// totalSkipped returns the count of posture facts that named a log target but
// produced no edge because an endpoint was not scanned.
func (t s3LogsToEdgeTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// s3BucketJoinIndex resolves an S3 bucket NAME to a scanned S3 bucket
// CloudResource node uid. It is built once per scope generation from the
// aws_resource S3 bucket facts so resolution is O(1) per posture fact — no
// per-edge graph round trip and no N+1 Cypher.
//
// It indexes only aws_s3_bucket resources, because both endpoints of a LOGS_TO
// edge must be S3 buckets. A name absent from the index did not scan as a bucket
// node and resolves to no edge — the trust-boundary rule, never fabricated. Each
// entry is derived from an aws_resource fact that carried its own
// account_id/region, so a cross-account log bucket resolves only if that
// account's bucket was scanned in the same scope.
type s3BucketJoinIndex struct {
	byName map[string]string
}

// buildS3BucketJoinIndex builds the bounded in-memory index from the scope
// generation's aws_resource fact envelopes, keeping only aws_s3_bucket
// resources and keying each by its bucket name.
func buildS3BucketJoinIndex(envelopes []facts.Envelope) s3BucketJoinIndex {
	index := s3BucketJoinIndex{byName: make(map[string]string, len(envelopes))}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if payloadString(env.Payload, "resource_type") != s3LogsToResourceTypeBucket {
			continue
		}
		accountID := payloadString(env.Payload, "account_id")
		region := payloadString(env.Payload, "region")
		resourceID := payloadString(env.Payload, "resource_id")
		arn := payloadString(env.Payload, "arn")
		if resourceID == "" {
			resourceID = arn
		}
		if resourceID == "" {
			continue
		}
		name := s3BucketName(env)
		if name == "" {
			continue
		}
		uid := cloudResourceUID(accountID, region, s3LogsToResourceTypeBucket, resourceID)
		// First writer wins on collision so a later duplicate cannot re-point a
		// name to a different node. S3 names are globally unique, so a collision
		// means duplicate facts for the same bucket.
		if _, exists := index.byName[name]; !exists {
			index.byName[name] = uid
		}
	}
	return index
}

// resolve looks up a bucket name and returns the scanned node uid on an exact
// name hit.
func (i s3BucketJoinIndex) resolve(name string) (string, bool) {
	uid, ok := i.byName[strings.TrimSpace(name)]
	return uid, ok
}

// s3BucketName derives the bucket name from an aws_resource S3 bucket fact: the
// node's name field first, then the tail of its arn:aws:s3:::<name> ARN, then
// the s3:// correlation anchor. Returning the canonical name lets the by-name
// index match a bare logging_target_bucket value.
func s3BucketName(env facts.Envelope) string {
	if name := strings.TrimSpace(payloadString(env.Payload, "name")); name != "" {
		return name
	}
	if name := s3BucketNameFromARN(payloadString(env.Payload, "arn")); name != "" {
		return name
	}
	if name := s3BucketNameFromARN(payloadString(env.Payload, "resource_id")); name != "" {
		return name
	}
	for _, anchor := range payloadStrings(env.Payload, "", "correlation_anchors") {
		anchor = strings.TrimSpace(anchor)
		if strings.HasPrefix(anchor, "s3://") {
			if name := strings.TrimSpace(strings.TrimPrefix(anchor, "s3://")); name != "" {
				return name
			}
		}
	}
	return ""
}

// s3BucketNameFromARN returns the bucket name from an arn:aws:s3:::<name> ARN
// (any partition), or "" when the value is not an S3 bucket ARN.
func s3BucketNameFromARN(arn string) string {
	arn = strings.TrimSpace(arn)
	if !strings.HasPrefix(arn, "arn:") {
		return ""
	}
	idx := strings.Index(arn, s3LogsToARNInfix)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(arn[idx+len(s3LogsToARNInfix):])
}

// ExtractS3LogsToEdgeRows builds canonical LOGS_TO edge rows from the scope
// generation's s3_bucket_posture facts, resolving both the source bucket and the
// logging_target_bucket against an in-memory bucket-name index built from the
// generation's aws_resource S3 facts. It never fabricates a node: a target whose
// name is not scanned as a bucket node in this scope is counted in the returned
// tally and produces no row.
//
// A blank logging_target_bucket (logging disabled) produces no row and is not
// counted as a skip. A self-target (a bucket logging to itself) is a legal,
// real S3 configuration and DOES produce an edge — the deliberate divergence
// from the IAM self-assume skip rule.
//
// Returned rows are deduplicated by (source_uid, LOGS_TO, target_uid) and sorted
// deterministically so the batched write is stable across retries and
// reprojections.
func ExtractS3LogsToEdgeRows(
	resourceEnvelopes []facts.Envelope,
	postureEnvelopes []facts.Envelope,
) ([]map[string]any, s3LogsToEdgeTally) {
	tally := newS3LogsToEdgeTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally
	}

	index := buildS3BucketJoinIndex(resourceEnvelopes)

	type edgeKey struct {
		source string
		target string
	}
	seen := make(map[edgeKey]struct{}, len(postureEnvelopes))
	rows := make([]map[string]any, 0, len(postureEnvelopes))

	for _, env := range postureEnvelopes {
		if env.FactKind != facts.S3BucketPostureFactKind {
			continue
		}

		target := strings.TrimSpace(payloadString(env.Payload, "logging_target_bucket"))
		if target == "" {
			// Logging disabled — the normal no-edge state, not a skip-error.
			continue
		}

		sourceName := s3PostureBucketName(env)
		sourceUID, sourceOK := index.resolve(sourceName)
		if !sourceOK {
			// The bucket emitting the posture fact did not scan as a node, so the
			// whole statement cannot anchor an edge. Count it once.
			tally.skipped[s3LogsToSkipSourceUnresolved]++
			continue
		}

		targetUID, targetOK := index.resolve(target)
		if !targetOK {
			// The log target bucket was not scanned in this scope (cross-account
			// central log account, out-of-scope region). No dangling node.
			tally.skipped[s3LogsToSkipTargetUnresolved]++
			continue
		}

		key := edgeKey{source: sourceUID, target: targetUID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		tally.resolved[s3LogsToModeName]++
		rows = append(rows, map[string]any{
			"source_uid":        sourceUID,
			"target_uid":        targetUID,
			"relationship_type": s3LogsToRelationshipType,
			"resolution_mode":   s3LogsToModeName,
		})
	}

	if len(rows) == 0 {
		return nil, tally
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["source_uid"]) + "->" + anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["source_uid"]) + "->" + anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally
}

// s3PostureBucketName derives the source bucket name from an s3_bucket_posture
// fact: the bucket_name field first, then the tail of its bucket_arn. The
// posture fact always carries at least one of these by construction
// (NewS3BucketPostureEnvelope requires bucket_arn or bucket_name).
func s3PostureBucketName(env facts.Envelope) string {
	if name := strings.TrimSpace(payloadString(env.Payload, "bucket_name")); name != "" {
		return name
	}
	return s3BucketNameFromARN(payloadString(env.Payload, "bucket_arn"))
}
