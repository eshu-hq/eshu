// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	log "github.com/eshu-hq/eshu/go/pkg/log"
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
func ExtractEC2InstanceNodeRows(envelopes []facts.Envelope) ([]map[string]any, error) {
	rows, _, err := ExtractEC2InstanceNodeRowsWithSkips(envelopes)
	return rows, err
}

// ExtractEC2InstanceNodeRowsWithSkips projects ec2_instance_posture fact envelopes
// into deterministic node rows and returns the conservative skip tally. Rows are
// deduplicated by uid so duplicate facts (retries, overlapping scans) converge on
// a single node; tombstoned instances (a terminated instance no longer running)
// and facts that carry neither an instance id nor an arn are skipped rather than
// fabricating a phantom node. The returned rows are sorted by uid for a
// byte-stable batch independent of input ordering.
func ExtractEC2InstanceNodeRowsWithSkips(envelopes []facts.Envelope) ([]map[string]any, ec2InstanceSkipTally, error) {
	skipped := ec2InstanceSkipTally{}
	if len(envelopes) == 0 {
		return nil, skipped, nil
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
		row, uid, ok, err := ec2InstanceNodeRow(env)
		if err != nil {
			return nil, skipped, err
		}
		if !ok {
			skipped[ec2InstanceSkipMissingIdentity]++
			continue
		}
		// Last fact for a uid wins; identity is stable so the choice only affects
		// mutable properties, and idempotent MERGE makes it safe.
		byUID[uid] = row
	}

	if len(byUID) == 0 {
		return nil, skipped, nil
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
	return rows, skipped, nil
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
func ec2InstanceNodeRow(env facts.Envelope) (map[string]any, string, bool, error) {
	posture, err := decodeEC2InstancePosture(env)
	if err != nil {
		return nil, "", false, err
	}
	instanceID := derefString(posture.InstanceID)
	arn := derefString(posture.ARN)

	resourceID := instanceID
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return nil, "", false, nil
	}

	resourceType := derefString(posture.ResourceType)
	if resourceType == "" {
		resourceType = "aws_ec2_instance"
	}

	uid := cloudResourceUID(posture.AccountID, posture.Region, resourceType, resourceID)
	row := map[string]any{
		"uid":           uid,
		"arn":           arn,
		"resource_id":   resourceID,
		"resource_type": resourceType,
		// The posture fact carries no Name tag; the instance id is the stable name
		// and no tag value (which could carry secrets) is ever read.
		"name":                resourceID,
		"state":               derefString(posture.State),
		"account_id":          posture.AccountID,
		"region":              posture.Region,
		"service_kind":        derefString(posture.ServiceKind),
		"correlation_anchors": posture.CorrelationAnchors,

		// Derived posture (nullable scalars/booleans preserved as nil when absent
		// so an unreported field stays distinct from an observed false/zero). Each
		// boolean is a plain scalar bool or nil — never a *bool pointer — so the
		// graph backend stores a clean scalar property.
		"imds_v2_required":            boolPtrToAny(posture.IMDSv2Required),
		"imds_http_endpoint":          derefString(posture.IMDSHTTPEndpoint),
		"imds_http_put_hop_limit":     int32PtrToInt64Any(posture.IMDSHTTPPutHopLimit),
		"user_data_present":           boolPtrToAny(posture.UserDataPresent),
		"detailed_monitoring_enabled": boolPtrToAny(posture.DetailedMonitoringEnabled),
		"ebs_optimized":               boolPtrToAny(posture.EBSOptimized),
		"public_ip_associated":        boolPtrToAny(posture.PublicIPAssociated),
		"instance_profile_arn":        derefString(posture.InstanceProfileARN),
		"tenancy":                     derefString(posture.Tenancy),
		"nitro_enclave_enabled":       boolPtrToAny(posture.NitroEnclaveEnabled),

		"source_fact_id":    env.FactID,
		"stable_fact_key":   env.StableFactKey,
		"source_system":     env.SourceRef.SourceSystem,
		"source_record_id":  env.SourceRef.SourceRecordID,
		"source_confidence": string(env.SourceConfidence),
		"collector_kind":    env.CollectorKind,
	}
	return row, uid, true, nil
}

// boolPtrToAny converts an optional *bool posture field to the any shape the node
// row stores: nil for an unreported field (distinct from an observed false) or
// the plain scalar bool value, matching the pre-typing payloadBoolOrNil result
// so the graph backend stores a clean scalar property.
func boolPtrToAny(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

// int32PtrToInt64Any converts an optional *int32 posture field to the any shape
// the node row stores: nil for an unreported field or the value widened to int64,
// matching the pre-typing payloadNumberOrNil result (which normalized every
// integral representation to int64).
func int32PtrToInt64Any(value *int32) any {
	if value == nil {
		return nil
	}
	return int64(*value)
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
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
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
