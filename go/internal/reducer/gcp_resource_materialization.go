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
)

// gcpResourceMaterializationDomainDefinition returns the additive definition for
// GCP cloud-resource node materialization. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// CloudResourceNodeWriter and FactLoader — registering it without them would
// silently drop every intent. It reuses the provider-neutral CloudResource node
// writer that the AWS path already wires, so GCP resources land as canonical
// CloudResource graph nodes that the GCP relationship edge projection (#2348)
// joins against. See docs/internal/gcp-cloud-resource-materialization-design.md.
func gcpResourceMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainGCPResourceMaterialization,
		Summary: "materialize gcp_cloud_resource facts into canonical CloudResource graph nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "gcp_resource_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// gcpResourceEvidenceSource tags CloudResource nodes written by this reducer so
// the prior-generation retract path (and the GCP relationship edge projection)
// can scope its writes to reducer-owned GCP resource materialization. It is
// distinct from awsResourceEvidenceSource so the two provider node sets never
// contend on each other's evidence scoping.
const gcpResourceEvidenceSource = "reducer/gcp-resources"

// GCPResourceMaterializationHandler reduces one GCP resource materialization
// follow-up into canonical CloudResource node writes. It loads the scope
// generation's gcp_cloud_resource facts, projects them into deterministic node
// rows keyed by a stable uid, and hands the bounded batch to the
// provider-neutral node writer.
//
// This handler is the GCP node substrate that the GCP relationship edge
// projection (#2348) joins against. It intentionally does not write edges: edges
// are resolved against these nodes in a separate, gated stage. After the node
// write succeeds it publishes the GraphProjectionPhaseCanonicalNodesCommitted
// readiness phase for the GCP acceptance unit, so the edge stage never resolves
// against a generation whose nodes have not committed.
//
// See docs/internal/gcp-cloud-resource-materialization-design.md.
type GCPResourceMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter CloudResourceNodeWriter
	// PhasePublisher records the canonical-nodes-committed readiness phase that
	// gates the GCP relationship edge projection. A nil publisher is a no-op so
	// the additive domain stays safe to register before the edge stage is wired.
	PhasePublisher GraphProjectionPhasePublisher
	// PresenceWriter records uid-exact endpoint presence for committed
	// CloudResource nodes so a cross-scope projection gate can prove a specific
	// node committed. It is nil on the default hot path, making presence a no-op.
	PresenceWriter EndpointPresenceWriter
	// Instruments records bounded Prometheus counters and histograms for GCP
	// resource materialization. Nil preserves existing no-metric behavior.
	Instruments *telemetry.Instruments
}

// Handle executes one GCP resource materialization intent.
func (h GCPResourceMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainGCPResourceMaterialization {
		return Result{}, fmt.Errorf(
			"gcp resource materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("gcp resource materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("gcp resource materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.GCPCloudResourceFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for gcp resource materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, quarantined, err := ExtractGCPCloudResourceNodeRows(envelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed gcp_cloud_resource fact (a missing required
	// identity field) is quarantined as a visible input_invalid dead-letter —
	// counter + structured error log — while every valid fact still materializes
	// its node below and the canonical-nodes-committed phase still publishes, so
	// one bad fact never stalls the scope generation's node substrate.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainGCPResourceMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteCloudResourceNodes(ctx, rows, gcpResourceEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical cloud resource nodes: %w", err)
		}
		writeDuration = time.Since(writeStart)

		// Record uid-exact presence for the committed CloudResource nodes so a
		// cross-scope projection gate can prove these endpoints exist. Flag-gated:
		// a nil PresenceWriter (the default) makes this a no-op.
		if err := publishEndpointPresence(
			ctx, h.PresenceWriter,
			GraphProjectionKeyspaceCloudResourceUID, intent.ScopeID, rows, time.Now().UTC(),
		); err != nil {
			return Result{}, fmt.Errorf("record cloud resource endpoint presence: %w", err)
		}
	}

	// Publish the canonical-nodes-committed readiness phase only after the node
	// write succeeds (or is a legitimate no-op for an empty generation). The GCP
	// relationship edge stage gates its projection on this phase: publishing
	// before a successful write would let edges resolve against nodes that never
	// committed, and not publishing on an empty generation would block it forever.
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

	timing := gcpResourceMaterializationTiming{
		intent:               intent,
		factCount:            len(envelopes),
		nodeCount:            len(rows),
		loadDuration:         loadDuration,
		extractDuration:      extractDuration,
		writeDuration:        writeDuration,
		phasePublishDuration: phasePublishDuration,
		totalDuration:        time.Since(totalStart),
	}
	logGCPResourceMaterializationCompleted(ctx, timing)
	h.recordMetrics(ctx, timing)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainGCPResourceMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d canonical cloud resource node(s) from %d gcp resource fact(s); %d input_invalid fact(s) quarantined",
			len(rows),
			len(envelopes),
			inputInvalidCount,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func (h GCPResourceMaterializationHandler) recordMetrics(
	ctx context.Context,
	timing gcpResourceMaterializationTiming,
) {
	if h.Instruments == nil {
		return
	}
	recordGCPMaterializationFact(ctx, h.Instruments, DomainGCPResourceMaterialization, facts.GCPCloudResourceFactKind, timing.factCount)
	recordGCPMaterializationGraphWrites(ctx, h.Instruments, DomainGCPResourceMaterialization, "node", timing.nodeCount)
	recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPResourceMaterialization, "load_facts", timing.loadDuration)
	recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPResourceMaterialization, "extract", timing.extractDuration)
	if timing.nodeCount > 0 {
		recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPResourceMaterialization, "graph_write", timing.writeDuration)
	}
	recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPResourceMaterialization, "phase_publish", timing.phasePublishDuration)
	recordGCPMaterializationDuration(ctx, h.Instruments, DomainGCPResourceMaterialization, "total", timing.totalDuration)
}

// ExtractGCPCloudResourceNodeRows projects gcp_cloud_resource fact envelopes into
// deterministic CloudResource node rows keyed by a stable uid. Each fact is
// decoded through the factschema seam, so a payload missing a required identity
// field (full_resource_name, asset_type) is quarantined as a per-fact
// input_invalid dead-letter (returned in the []quarantinedFact slice) rather
// than fabricating a node with an empty-string uid OR aborting the whole batch:
// every valid fact still projects. Rows are deduplicated by uid so duplicate
// facts (retries, overlapping scans) converge on a single node. The returned
// rows are sorted by uid for deterministic batch output. Mirrors
// ExtractCloudResourceNodeRows (aws_resource_materialization.go).
func ExtractGCPCloudResourceNodeRows(envelopes []facts.Envelope) ([]map[string]any, []quarantinedFact, error) {
	if len(envelopes) == 0 {
		return nil, nil, nil
	}

	var quarantined []quarantinedFact
	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		row, uid, ok, err := gcpCloudResourceNodeRow(env)
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

// gcpCloudResourceNodeRow builds one CloudResource node row from a
// gcp_cloud_resource fact envelope, returning ok=false when the resource lacks
// the identity needed to form a stable node uid, or a classified decode error
// when a required identity field is absent from the payload. Identity and
// common fields are read from the decoded factschema struct, never the raw
// envelope payload. This node row does not yet read the decoded struct's
// untyped Attributes pass-through (the nested "attributes" bounded typed-depth
// map); a future per-asset-type consumer would read it via a typed
// gcpv1.DecodeResource<AssetType>Attributes accessor, mirroring the bounded
// AWS resource-attribute typing in sdk/go/factschema/aws/v1 (issue #4631),
// once such a consumer exists.
//
// GCP resource identity is the globally-unique Cloud Asset Inventory
// full_resource_name; the uid folds it together with asset type, project, and
// location so it shares the CloudResource keyspace with the AWS path while
// staying collision-free. The GCP relationship edge join (#2348) resolves
// endpoints by full_resource_name (stored as resource_id), so a resource that
// does not materialize here is also not a join target — never a fabricated node.
func gcpCloudResourceNodeRow(env facts.Envelope) (map[string]any, string, bool, error) {
	resource, err := decodeGCPCloudResource(env)
	if err != nil {
		return nil, "", false, err
	}

	fullResourceName := resource.FullResourceName
	assetType := resource.AssetType
	projectID := derefString(resource.ProjectID)
	location := derefString(resource.Location)

	if fullResourceName == "" || assetType == "" {
		// Present-but-empty identity is a valid decode, distinct from an absent
		// required key, which the decode seam already rejected above.
		return nil, "", false, nil
	}

	uid := cloudResourceUID(projectID, location, assetType, fullResourceName)
	row := map[string]any{
		"uid":           uid,
		"arn":           "",
		"resource_id":   fullResourceName,
		"resource_type": assetType,
		"name":          derefString(resource.DisplayName),
		"state":         derefString(resource.State),
		"account_id":    projectID,
		"region":        location,
		"service_kind":  derefString(resource.AssetTypeFamily),
		// Present for parity with the AWS node row and the shared writer's SET
		// clause; GCP carries no correlation anchors today (relationship edges
		// resolve on full_resource_name), so this is an empty slice until a GCP
		// anchor source exists.
		"correlation_anchors": uniqueSortedStrings(resource.CorrelationAnchors),
		"source_fact_id":      env.FactID,
		"stable_fact_key":     env.StableFactKey,
		"source_system":       env.SourceRef.SourceSystem,
		"source_record_id":    env.SourceRef.SourceRecordID,
		"source_confidence":   string(env.SourceConfidence),
		"collector_kind":      env.CollectorKind,
		sourceOrderKeyField:   sourceOrderKey(env),
		// The 7 keys below are explicit empty-value parity fields for
		// canonicalCloudResourceUpsertCypher's unconditional SET clause
		// (go/internal/storage/cypher/cloud_resource_node_writer.go), which reads
		// row.workload_id, row.service_name, row.service_anchor_status,
		// row.service_anchor_source, row.service_anchor_reason,
		// row.service_anchor_names, and row.service_anchor_name_tokens for every
		// batch row. They must be PRESENT keys, not omitted ones (issue #4995):
		// the pinned NornicDB backend does not evaluate a missing UNWIND row map
		// key as null in a SET clause, it persists a stringified representation
		// of the row expression instead (proved against
		// timothyswt/nornicdb-cpu-bge:v1.1.9 — see
		// TestExtractGCPCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeys
		// for the regression and this package's README "#4995" entry for the
		// Cypher shim reproduction). The service_name gap was masked in the API
		// read by a "row.service_name" placeholder drop in
		// go/internal/query/cloud_resources.go, but the corrupted literal was
		// still persisted to the graph. GCP has no service-anchor decision
		// source today (mirrors the AWS row for a resource with no
		// service-anchor decision, aws_resource_service_anchor.go), so these are
		// the correct no-anchor values, just present rather than absent.
		"workload_id":                "",
		"service_name":               "",
		"service_anchor_status":      "",
		"service_anchor_source":      "",
		"service_anchor_reason":      "",
		"service_anchor_names":       []string{},
		"service_anchor_name_tokens": "",
		// running_image_ref/running_image_digest: same #4995-shaped parity
		// requirement, added for issue #5450. GCP resources are never a
		// running-image source; both keys must still be PRESENT (not omitted)
		// so canonicalCloudResourceUpsertCypher's unconditional SET clause does
		// not hit the pinned NornicDB missing-map-key-in-UNWIND bug (see
		// go/internal/reducer/aws_resource_running_image.go's
		// runningImageFieldsAbsent doc for the live-proved mechanism).
		"running_image_ref":    "",
		"running_image_digest": "",
	}
	// No-op in every normal build; under the ifadeterminismteeth build tag
	// this stamps a process-order-dependent debug property the determinism
	// matrix's --teeth mode uses to prove a non-idempotent write is caught.
	// See gcp_resource_materialization_teeth.go's doc for the full picture.
	ifaTeethStampCloudResourceRow(row)
	return row, uid, true, nil
}

// gcpResourceMaterializationTiming groups stage durations so the completion log
// can identify whether GCP resource work is fact loading, extraction, or graph
// backend time.
type gcpResourceMaterializationTiming struct {
	intent               Intent
	factCount            int
	nodeCount            int
	loadDuration         time.Duration
	extractDuration      time.Duration
	writeDuration        time.Duration
	phasePublishDuration time.Duration
	totalDuration        time.Duration
}

func logGCPResourceMaterializationCompleted(
	ctx context.Context,
	timing gcpResourceMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "gcp resource materialization completed",
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
