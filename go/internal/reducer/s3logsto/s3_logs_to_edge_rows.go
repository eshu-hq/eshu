// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3logsto

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

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
) ([]map[string]any, s3LogsToEdgeTally, []factdecode.QuarantinedFact, error) {
	tally := newS3LogsToEdgeTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	index, quarantined, err := cloudjoin.BuildS3BucketJoinIndex(resourceEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}

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

		posture, err := schemadecode.DecodeS3BucketPosture(env)
		if err != nil {
			q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, tally, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}

		target := strings.TrimSpace(payloadcore.DerefString(posture.LoggingTargetBucket))
		if target == "" {
			// Logging disabled — the normal no-edge state, not a skip-error.
			continue
		}

		sourceName := cloudjoin.S3PostureBucketName(posture)
		sourceUID, sourceOK := index.Resolve(sourceName)
		if !sourceOK {
			// The bucket emitting the posture fact did not scan as a node, so the
			// whole statement cannot anchor an edge. Count it once.
			tally.skipped[s3LogsToSkipSourceUnresolved]++
			continue
		}

		targetUID, targetOK := index.Resolve(target)
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
		return nil, tally, quarantined, nil
	}

	sort.Slice(rows, func(a, b int) bool {
		left := payloadcore.AnyToString(rows[a]["source_uid"]) + "->" + payloadcore.AnyToString(rows[a]["target_uid"])
		right := payloadcore.AnyToString(rows[b]["source_uid"]) + "->" + payloadcore.AnyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally, quarantined, nil
}
