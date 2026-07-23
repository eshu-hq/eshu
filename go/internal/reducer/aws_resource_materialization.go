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
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
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
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// when an aws_resource fact is quarantined as input_invalid during node
	// extraction. Optional: a nil pointer skips the counter (the structured
	// per-fact error log still emits).
	Instruments *telemetry.Instruments
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
	rows, quarantined, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource fact (a missing required
	// identity field) is quarantined as a visible input_invalid dead-letter —
	// counter + structured error log — while every valid fact still materializes
	// its node below and the canonical-nodes-committed phase still publishes, so
	// one bad fact never stalls the scope generation's node substrate.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainAWSResourceMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
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
			"materialized %d canonical cloud resource node(s) from %d aws resource fact(s); %d input_invalid fact(s) quarantined",
			len(rows),
			len(envelopes),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// ExtractCloudResourceNodeRows projects aws_resource fact envelopes into
// deterministic CloudResource node rows keyed by a stable uid. Each fact is
// decoded through the factschema seam, so a payload missing a required identity
// field is quarantined as a per-fact input_invalid dead-letter (returned in the
// []quarantinedFact slice) rather than fabricating a node with an empty-string
// uid OR aborting the whole batch: every valid fact still projects. Rows are
// deduplicated by uid so duplicate facts (retries, overlapping scans) converge
// on a single node, and incomplete-but-valid identities (present-but-empty
// resource_type, or both resource_id and arn empty) are dropped rather than
// fabricating a node. The returned rows are sorted by uid for deterministic
// batch output.
func ExtractCloudResourceNodeRows(envelopes []facts.Envelope) ([]map[string]any, []quarantinedFact, error) {
	if len(envelopes) == 0 {
		return nil, nil, nil
	}

	var quarantined []quarantinedFact
	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		row, uid, ok, err := cloudResourceNodeRow(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !ok {
			continue
		}
		// #5007 Stage 1: the max-source_order_key contributor wins (latest
		// observation, source_fact_id tie-break), not the last fact by slice
		// order, so within-scope duplicate-uid resolution uses the identical
		// rule the owner ledger applies across scopes.
		if preferMaxSourceOrderKey(byUID[uid], row) {
			byUID[uid] = row
		}
	}

	if len(byUID) == 0 {
		return nil, quarantined, nil
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
	return rows, quarantined, nil
}

// cloudResourceNodeRow builds one CloudResource node row from an aws_resource
// fact envelope, returning ok=false when the resource lacks the identity needed
// to form a stable node uid, or a classified decode error when a required
// identity field is absent from the payload. Identity and common fields are read
// from the decoded factschema struct; service-specific fields (the service-anchor
// workload_id/service_name and any nested attributes) are read from the struct's
// untyped Attributes pass-through, never from the raw envelope payload.
//
// #5448: an aws_ec2_instance resource is intentionally excluded here (ok=false,
// same conservative "not a materializable node" return path as an incomplete
// identity). DomainEC2InstanceNodeMaterialization already owns creation of the
// EC2 instance CloudResource node's shared base properties from the
// ec2_instance_posture fact (go/internal/reducer/ec2_instance_node_materialization.go)
// — including "name" and "state" values this generic path would compute
// differently (the posture path deliberately never reads a Name tag). Letting
// this generic path ALSO write the same uid's base properties would race two
// reducer domains over the identical property set with different values. The
// #5448 identity aws_resource fact's ami_id is instead projected by the
// dedicated, disjoint-property EC2InstanceIdentityMaterialization domain (see
// ec2_instance_identity_rows.go), which only ever augments an
// already-materialized node and never creates or overwrites a base field.
func cloudResourceNodeRow(env facts.Envelope) (map[string]any, string, bool, error) {
	resource, err := decodeAWSResource(env)
	if err != nil {
		return nil, "", false, err
	}
	if resource.ResourceType == awsv1.ResourceTypeEC2Instance {
		return nil, "", false, nil
	}

	arn := derefString(resource.ARN)
	uid, ok := cloudResourceUIDForResource(resource)
	if !ok {
		return nil, "", false, nil
	}
	resourceID := resource.ResourceID
	if resourceID == "" {
		resourceID = arn
	}
	row := map[string]any{
		"uid":           uid,
		"arn":           arn,
		"resource_id":   resourceID,
		"resource_type": resource.ResourceType,
		"name":          derefString(resource.Name),
		"state":         derefString(resource.State),
		"account_id":    resource.AccountID,
		"region":        resource.Region,
		"service_kind":  derefString(resource.ServiceKind),
		// uniqueSortedStrings preserves the pre-typing byte-identical output: the
		// old payloadStrings(env.Payload, "", "correlation_anchors") trimmed,
		// deduplicated, and sorted the anchors. The typed decode returns the raw
		// []string as emitted, so the reducer must re-apply that normalization
		// here (and at every other CorrelationAnchors projection site).
		"correlation_anchors": uniqueSortedStrings(resource.CorrelationAnchors),
		"source_fact_id":      env.FactID,
		"stable_fact_key":     env.StableFactKey,
		"source_system":       env.SourceRef.SourceSystem,
		"source_record_id":    env.SourceRef.SourceRecordID,
		"source_confidence":   string(env.SourceConfidence),
		"collector_kind":      env.CollectorKind,
		sourceOrderKeyField:   sourceOrderKey(env),
	}
	anchorFields, err := cloudResourceServiceAnchorFields(resource)
	if err != nil {
		return nil, "", false, attributeShapeAsFactDecodeError(env.FactKind, err)
	}
	for key, value := range anchorFields {
		row[key] = value
	}
	runningImageFields, err := cloudResourceRunningImageFields(resource)
	if err != nil {
		return nil, "", false, attributeShapeAsFactDecodeError(env.FactKind, err)
	}
	for key, value := range runningImageFields {
		row[key] = value
	}
	return row, uid, true, nil
}

// cloudResourceUIDForResource derives the stable CloudResource node uid from an
// already-decoded aws_resource struct, returning ok=false when the resource
// lacks the identity needed to form a stable uid (empty resource_type, or both
// resource_id and arn empty). It shares the exact resource_id fallback and
// empty-identity rules cloudResourceNodeRow uses so a caller that already
// decoded the fact (for example ExtractWorkloadCloudRelationshipRows) can obtain
// the uid without decoding the same envelope a second time.
func cloudResourceUIDForResource(resource awsv1.Resource) (string, bool) {
	resourceID := resource.ResourceID
	if resourceID == "" {
		resourceID = derefString(resource.ARN)
	}
	if resource.ResourceType == "" || resourceID == "" {
		return "", false
	}
	return cloudResourceUID(resource.AccountID, resource.Region, resource.ResourceType, resourceID), true
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
