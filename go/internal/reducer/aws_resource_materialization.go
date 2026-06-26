// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// awsResourceMaterializationDomainDefinition returns the additive definition
// for AWS resource node materialization. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// CloudResourceNodeWriter and FactLoader — registering it without them would
// silently drop every intent. See
// docs/internal/aws-relationship-edge-materialization-design.md.
func awsResourceMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainAWSResourceMaterialization,
		Summary: "materialize aws_resource facts into canonical CloudResource graph nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "aws_resource_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// awsResourceEvidenceSource tags CloudResource nodes written by this reducer so
// the prior-generation retract path (and future edge projection) can scope its
// writes to reducer-owned AWS resource materialization.
const awsResourceEvidenceSource = "reducer/aws-resources"

// CloudResourceNodeWriter persists canonical CloudResource graph nodes from
// extracted node rows. Implementations MUST be idempotent by node uid so
// reducer retries and duplicate facts converge on one node rather than
// duplicating or fabricating graph state.
type CloudResourceNodeWriter interface {
	WriteCloudResourceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
}

// AWSResourceMaterializationHandler reduces one AWS resource materialization
// follow-up into canonical CloudResource node writes. It loads the scope
// generation's aws_resource facts, projects them into deterministic node rows,
// and hands the bounded batch to the node writer.
//
// This handler is the node substrate that the AWS relationship edge projection
// (issue #805) joins against. It intentionally does not write edges: edges are
// resolved against these nodes in a separate, gated stage. See
// docs/internal/aws-relationship-edge-materialization-design.md.
//
// After the canonical node write succeeds, the handler publishes the
// GraphProjectionKeyspaceCloudResourceUID / GraphProjectionPhaseCanonicalNodesCommitted
// readiness phase. Stage B (PR #2) gates its edge projection on this phase, so
// edges never resolve against a generation whose nodes have not yet committed.
type AWSResourceMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter CloudResourceNodeWriter
	// PhasePublisher records the canonical-nodes-committed readiness phase that
	// gates the AWS relationship edge projection. A nil publisher is a no-op so
	// the additive domain stays safe to register before Stage B is wired.
	PhasePublisher GraphProjectionPhasePublisher
	// PresenceWriter records uid-exact endpoint presence for committed
	// CloudResource nodes so the cross-scope secrets/IAM projection gate can prove
	// a specific node committed (issue #1380). It is nil unless the secrets/IAM
	// graph projection feature is enabled, so the default hot path carries no
	// extra write.
	PresenceWriter EndpointPresenceWriter
}

// Handle executes one AWS resource materialization intent.
func (h AWSResourceMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainAWSResourceMaterialization {
		return Result{}, fmt.Errorf(
			"aws resource materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("aws resource materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("aws resource materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.AWSResourceFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for aws resource materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows := ExtractCloudResourceNodeRows(envelopes)
	extractDuration := time.Since(extractStart)

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteCloudResourceNodes(ctx, rows, awsResourceEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical cloud resource nodes: %w", err)
		}
		writeDuration = time.Since(writeStart)

		// Record uid-exact presence for the committed CloudResource nodes so the
		// cross-scope secrets/IAM projection gate can prove these endpoints exist.
		// Flag-gated: a nil PresenceWriter (the default) makes this a no-op.
		if err := publishEndpointPresence(
			ctx, h.PresenceWriter,
			GraphProjectionKeyspaceCloudResourceUID, intent.ScopeID, rows, time.Now().UTC(),
		); err != nil {
			return Result{}, fmt.Errorf("record cloud resource endpoint presence: %w", err)
		}
	}

	// Publish the canonical-nodes-committed readiness phase only after the node
	// write succeeds (or is a legitimate no-op for an empty generation). Stage B
	// gates its edge projection on this phase: publishing before a successful
	// write would let edges resolve against nodes that never committed, and not
	// publishing on an empty generation would block Stage B forever.
	phasePublishStart := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceCloudResourceUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return Result{}, fmt.Errorf("publish canonical cloud resource nodes phase: %w", err)
	}
	phasePublishDuration := time.Since(phasePublishStart)

	logAWSResourceMaterializationCompleted(ctx, awsResourceMaterializationTiming{
		intent:               intent,
		factCount:            len(envelopes),
		nodeCount:            len(rows),
		loadDuration:         loadDuration,
		extractDuration:      extractDuration,
		writeDuration:        writeDuration,
		phasePublishDuration: phasePublishDuration,
		totalDuration:        time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainAWSResourceMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d canonical cloud resource node(s) from %d aws resource fact(s)",
			len(rows),
			len(envelopes),
		),
		CanonicalWrites: len(rows),
	}, nil
}

// ExtractCloudResourceNodeRows projects aws_resource fact envelopes into
// deterministic CloudResource node rows keyed by a stable uid. Rows are
// deduplicated by uid so duplicate facts (retries, overlapping scans) converge
// on a single node, and incomplete identities (missing resource_type or both
// resource_id and arn) are dropped rather than fabricating a node. The returned
// rows are sorted by uid for deterministic batch output.
func ExtractCloudResourceNodeRows(envelopes []facts.Envelope) []map[string]any {
	if len(envelopes) == 0 {
		return nil
	}

	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		row, uid, ok := cloudResourceNodeRow(env)
		if !ok {
			continue
		}
		// Last fact for a uid wins; identity is stable so the choice only
		// affects mutable properties, and idempotent MERGE makes it safe.
		byUID[uid] = row
	}

	if len(byUID) == 0 {
		return nil
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
	return rows
}

// cloudResourceNodeRow builds one CloudResource node row from an aws_resource
// fact envelope, returning ok=false when the resource lacks the identity needed
// to form a stable node uid.
func cloudResourceNodeRow(env facts.Envelope) (map[string]any, string, bool) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	resourceType := payloadString(env.Payload, "resource_type")
	resourceID := payloadString(env.Payload, "resource_id")
	arn := payloadString(env.Payload, "arn")

	if resourceID == "" {
		resourceID = arn
	}
	if resourceType == "" || resourceID == "" {
		return nil, "", false
	}

	uid := cloudResourceUID(accountID, region, resourceType, resourceID)
	row := map[string]any{
		"uid":                 uid,
		"arn":                 arn,
		"resource_id":         resourceID,
		"resource_type":       resourceType,
		"name":                payloadString(env.Payload, "name"),
		"state":               payloadString(env.Payload, "state"),
		"account_id":          accountID,
		"region":              region,
		"service_kind":        payloadString(env.Payload, "service_kind"),
		"correlation_anchors": payloadStrings(env.Payload, "", "correlation_anchors"),
		"source_fact_id":      env.FactID,
		"stable_fact_key":     env.StableFactKey,
		"source_system":       env.SourceRef.SourceSystem,
		"source_record_id":    env.SourceRef.SourceRecordID,
		"source_confidence":   string(env.SourceConfidence),
		"collector_kind":      env.CollectorKind,
	}
	for key, value := range cloudResourceServiceAnchorFields(env.Payload) {
		row[key] = value
	}
	return row, uid, true
}

// cloudResourceUID computes the stable CloudResource node identity. The identity
// inputs match the aws_resource fact's StableFactKey inputs so the AWS
// relationship edge projection (issue #805) can recompute the same uid from a
// relationship fact's resolved target identity.
func cloudResourceUID(accountID, region, resourceType, resourceID string) string {
	return facts.StableID("CloudResource", map[string]any{
		"account_id":    accountID,
		"region":        region,
		"resource_id":   resourceID,
		"resource_type": resourceType,
	})
}

// awsResourceMaterializationTiming groups stage durations so the completion log
// can identify whether AWS resource work is fact loading, extraction, or graph
// backend time.
type awsResourceMaterializationTiming struct {
	intent               Intent
	factCount            int
	nodeCount            int
	loadDuration         time.Duration
	extractDuration      time.Duration
	writeDuration        time.Duration
	phasePublishDuration time.Duration
	totalDuration        time.Duration
}

func logAWSResourceMaterializationCompleted(
	ctx context.Context,
	timing awsResourceMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "aws resource materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("node_count", timing.nodeCount),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
