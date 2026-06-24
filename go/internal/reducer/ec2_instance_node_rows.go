// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ec2InstanceSkipReason names a conservative no-fabrication skip class for the
// EC2 instance node extractor.
const (
	ec2InstanceSkipMissingIdentity = "missing_identity"
	ec2InstanceSkipTombstone       = "tombstone"
)

// ec2InstanceSkipTally counts conservative skips by reason for one generation so
// the handler can emit the bounded skip counter and completion log.
type ec2InstanceSkipTally map[string]int

// ExtractEC2InstanceNodeRows projects ec2_instance_posture fact envelopes into
// deterministic EC2 instance CloudResource node rows keyed by the canonical
// cloud_resource_uid. It is the public extractor used by tests and callers that
// do not need the skip tally; see ExtractEC2InstanceNodeRowsWithSkips for the
// telemetry-bearing variant.
func ExtractEC2InstanceNodeRows(envelopes []facts.Envelope) []map[string]any {
	rows, _ := ExtractEC2InstanceNodeRowsWithSkips(envelopes)
	return rows
}

// ExtractEC2InstanceNodeRowsWithSkips projects ec2_instance_posture fact envelopes
// into deterministic node rows and returns the conservative skip tally. Rows are
// deduplicated by uid so duplicate facts (retries, overlapping scans) converge on
// a single node; tombstoned instances (a terminated instance no longer running)
// and facts that carry neither an instance id nor an arn are skipped rather than
// fabricating a phantom node. The returned rows are sorted by uid for a
// byte-stable batch independent of input ordering.
func ExtractEC2InstanceNodeRowsWithSkips(envelopes []facts.Envelope) ([]map[string]any, ec2InstanceSkipTally) {
	skipped := ec2InstanceSkipTally{}
	if len(envelopes) == 0 {
		return nil, skipped
	}

	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.EC2InstancePostureFactKind {
			continue
		}
		// A terminated/tombstoned instance no longer runs, so it materializes no
		// node; reading it would assert a graph node for an instance that no longer
		// exists.
		if env.IsTombstone {
			skipped[ec2InstanceSkipTombstone]++
			continue
		}
		row, uid, ok := ec2InstanceNodeRow(env)
		if !ok {
			skipped[ec2InstanceSkipMissingIdentity]++
			continue
		}
		// Last fact for a uid wins; identity is stable so the choice only affects
		// mutable properties, and idempotent MERGE makes it safe.
		byUID[uid] = row
	}

	if len(byUID) == 0 {
		return nil, skipped
	}

	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)

	rows := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		rows = append(rows, byUID[uid])
	}
	return rows, skipped
}

// ec2InstanceNodeRow builds one EC2 instance CloudResource node row from a posture
// fact envelope, returning ok=false when the fact carries neither an instance id
// nor an arn to form a stable uid. The uid uses the canonical
// cloudResourceUID(account, region, "aws_ec2_instance", instance_id) scheme so an
// alarm or relationship that resolves an InstanceId resolves to this exact node;
// the identity falls back to the arn when the instance id is blank, matching the
// posture envelope's own identity derivation.
//
// The row carries metadata-only safe identifiers plus the derived posture
// booleans/scalars the fact emits. It NEVER carries user-data content (only the
// user_data_present boolean), the raw public IP, per-volume block devices, or any
// topology field the posture fact does not carry — materializing absent data would
// be fabrication.
func ec2InstanceNodeRow(env facts.Envelope) (map[string]any, string, bool) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	instanceID := payloadString(env.Payload, "instance_id")
	arn := payloadString(env.Payload, "arn")

	resourceID := instanceID
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return nil, "", false
	}

	resourceType := payloadString(env.Payload, "resource_type")
	if resourceType == "" {
		resourceType = "aws_ec2_instance"
	}

	uid := cloudResourceUID(accountID, region, resourceType, resourceID)
	row := map[string]any{
		"uid":           uid,
		"arn":           arn,
		"resource_id":   resourceID,
		"resource_type": resourceType,
		// The posture fact carries no Name tag; the instance id is the stable name
		// and no tag value (which could carry secrets) is ever read.
		"name":                resourceID,
		"state":               payloadString(env.Payload, "state"),
		"account_id":          accountID,
		"region":              region,
		"service_kind":        payloadString(env.Payload, "service_kind"),
		"correlation_anchors": payloadStrings(env.Payload, "", "correlation_anchors"),

		// Derived posture (nullable scalars/booleans preserved as nil when absent
		// so an unreported field stays distinct from an observed false/zero). Each
		// boolean is a plain scalar bool or nil — never a *bool pointer — so the
		// graph backend stores a clean scalar property.
		"imds_v2_required":            payloadBoolOrNil(env.Payload, "imds_v2_required"),
		"imds_http_endpoint":          payloadString(env.Payload, "imds_http_endpoint"),
		"imds_http_put_hop_limit":     payloadNumberOrNil(env.Payload, "imds_http_put_hop_limit"),
		"user_data_present":           payloadBoolOrNil(env.Payload, "user_data_present"),
		"detailed_monitoring_enabled": payloadBoolOrNil(env.Payload, "detailed_monitoring_enabled"),
		"ebs_optimized":               payloadBoolOrNil(env.Payload, "ebs_optimized"),
		"public_ip_associated":        payloadBoolOrNil(env.Payload, "public_ip_associated"),
		"instance_profile_arn":        payloadString(env.Payload, "instance_profile_arn"),
		"tenancy":                     payloadString(env.Payload, "tenancy"),
		"nitro_enclave_enabled":       payloadBoolOrNil(env.Payload, "nitro_enclave_enabled"),

		"source_fact_id":    env.FactID,
		"stable_fact_key":   env.StableFactKey,
		"source_system":     env.SourceRef.SourceSystem,
		"source_record_id":  env.SourceRef.SourceRecordID,
		"source_confidence": string(env.SourceConfidence),
		"collector_kind":    env.CollectorKind,
	}
	return row, uid, true
}

// payloadBoolOrNil reads a boolean payload value as a plain scalar bool, returning
// nil when the key is absent (or carries a blank/unparseable value). It returns a
// plain bool rather than a *bool so the value is a clean graph scalar property;
// preserving nil keeps an unreported field distinct from an observed false, so the
// node never fabricates absent posture data.
func payloadBoolOrNil(payload map[string]any, key string) any {
	value, ok := payloadBoolPointerValue(payload, key)
	if !ok {
		return nil
	}
	return value
}

// payloadNumberOrNil reads a numeric payload value into a clean int64 regardless
// of whether it arrived as an in-memory int/int32/int64 or a JSON-deserialized
// float64, and returns nil when the key is absent. Preserving nil keeps an
// unreported field (e.g. an IMDS hop limit DescribeInstances did not return)
// distinct from an observed zero, so the node never fabricates absent data.
func payloadNumberOrNil(payload map[string]any, key string) any {
	switch value := payload[key].(type) {
	case nil:
		return nil
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return nil
	}
}

// ec2InstanceNodeMaterializationTiming groups stage durations so the completion
// log can identify whether EC2 instance node work is fact loading, extraction, or
// graph backend time.
type ec2InstanceNodeMaterializationTiming struct {
	intent               Intent
	factCount            int
	nodeCount            int
	skipped              ec2InstanceSkipTally
	loadDuration         time.Duration
	extractDuration      time.Duration
	writeDuration        time.Duration
	phasePublishDuration time.Duration
	totalDuration        time.Duration
}

func logEC2InstanceNodeMaterializationCompleted(
	ctx context.Context,
	timing ec2InstanceNodeMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "ec2 instance node materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("node_count", timing.nodeCount),
		slog.Int("skipped_missing_identity", timing.skipped[ec2InstanceSkipMissingIdentity]),
		slog.Int("skipped_tombstone", timing.skipped[ec2InstanceSkipTombstone]),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
