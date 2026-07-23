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
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

func azureResourceMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainAzureResourceMaterialization,
		Summary: "materialize azure_cloud_resource facts into canonical CloudResource graph nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "azure_resource_materialization",
			SourceLayers:  []truth.Layer{truth.LayerObservedResource},
		},
	}
}

const azureResourceEvidenceSource = "reducer/azure-resources"

// AzureResourceMaterializationHandler reduces one Azure resource materialization
// intent into canonical CloudResource node writes and publishes the
// canonical-nodes readiness phase that Azure relationship projection gates on.
type AzureResourceMaterializationHandler struct {
	FactLoader     FactLoader
	NodeWriter     CloudResourceNodeWriter
	PhasePublisher GraphProjectionPhasePublisher
	PresenceWriter EndpointPresenceWriter
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// for a per-fact quarantined azure_cloud_resource decode failure. A nil
	// Instruments only skips the counter increment; the quarantine and
	// structured error log still happen via recordQuarantinedFacts.
	Instruments *telemetry.Instruments
}

// Handle executes one Azure resource materialization intent.
func (h AzureResourceMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainAzureResourceMaterialization {
		return Result{}, fmt.Errorf("azure resource materialization handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("azure resource materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("azure resource materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, []string{facts.AzureCloudResourceFactKind})
	if err != nil {
		return Result{}, fmt.Errorf("load facts for azure resource materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, quarantined, err := ExtractAzureCloudResourceNodeRows(envelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed azure_cloud_resource fact (a missing
	// required identity field) is quarantined as a visible input_invalid
	// dead-letter — counter + structured error log — while every valid fact
	// still materializes its node below and the canonical-nodes-committed phase
	// still publishes, so one bad fact never stalls the scope generation's node
	// substrate.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainAzureResourceMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteCloudResourceNodes(ctx, rows, azureResourceEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical azure cloud resource nodes: %w", err)
		}
		writeDuration = time.Since(writeStart)
		if err := publishEndpointPresence(ctx, h.PresenceWriter, GraphProjectionKeyspaceCloudResourceUID, intent.ScopeID, rows, time.Now().UTC()); err != nil {
			return Result{}, fmt.Errorf("record azure cloud resource endpoint presence: %w", err)
		}
	}

	phasePublishStart := time.Now()
	if err := publishIntentGraphPhase(ctx, h.PhasePublisher, intent, GraphProjectionKeyspaceCloudResourceUID, GraphProjectionPhaseCanonicalNodesCommitted, time.Now().UTC()); err != nil {
		return Result{}, fmt.Errorf("publish canonical azure cloud resource nodes phase: %w", err)
	}
	phasePublishDuration := time.Since(phasePublishStart)

	logAzureResourceMaterializationCompleted(ctx, azureResourceMaterializationTiming{
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
		IntentID:        intent.IntentID,
		Domain:          DomainAzureResourceMaterialization,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("materialized %d canonical cloud resource node(s) from %d azure resource fact(s)", len(rows), len(envelopes)),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// ExtractAzureCloudResourceNodeRows projects azure_cloud_resource facts into
// deterministic CloudResource rows keyed by normalized ARM identity. Facts
// decode through the factschema.DecodeAzureCloudResource contracts seam; a
// fact missing a required identity field (arm_resource_id, resource_type,
// subscription_id, location) is routed through partitionDecodeFailures into
// the returned quarantine list rather than silently entering the join index
// under an empty-string identity segment. Any OTHER decode error (a transient
// condition partitionDecodeFailures does not quarantine) is returned fatally
// so the caller fails the whole intent.
func ExtractAzureCloudResourceNodeRows(envelopes []facts.Envelope) ([]map[string]any, []quarantinedFact, error) {
	if len(envelopes) == 0 {
		return nil, nil, nil
	}

	var quarantined []quarantinedFact
	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.AzureCloudResourceFactKind || env.IsTombstone {
			continue
		}
		row, uid, _, ok, err := azureCloudResourceNodeRow(env)
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

// azureCloudResourceNodeRow decodes env through the contracts seam and builds
// its CloudResource node row. It returns the row, the row's uid, the join
// identity (NormalizedResourceID when present, falling back to
// ARMResourceID) the relationship join index keys on, whether the fact
// produced a materializable row, and a decode error. Returning the join
// identity here — rather than making a caller re-decode env to recover it —
// avoids a second full contracts-seam decode per resource fact in
// buildAzureCloudResourceJoinIndex, which the AWS migration's benchmark
// discipline (~10% diagnostic band) would otherwise flag as an avoidable
// regression on the relationship join-index build path.
func azureCloudResourceNodeRow(env facts.Envelope) (row map[string]any, uid string, resourceID string, ok bool, err error) {
	resource, err := decodeAzureCloudResource(env)
	if err != nil {
		return nil, "", "", false, err
	}

	resourceID = azureNormalizedResourceIDForResource(resource)
	if resourceID == "" || resource.ResourceType == "" {
		return nil, "", "", false, nil
	}
	uid = cloudResourceUID(resource.SubscriptionID, resource.Location, resource.ResourceType, resourceID)
	row = map[string]any{
		"uid":           uid,
		"arn":           "",
		"resource_id":   resourceID,
		"resource_type": resource.ResourceType,
		"name":          derefString(resource.ResourceName),
		"state":         "",
		"account_id":    resource.SubscriptionID,
		"region":        resource.Location,
		"service_kind":  derefString(resource.ProviderNamespace),
		// correlation_anchors is always empty for azure_cloud_resource: the
		// Azure collector emitter never populates this key, so the typed
		// struct carries no CorrelationAnchors field and this preserves the
		// pre-typing byte-identical (always nil) output.
		"correlation_anchors": []string(nil),
		"source_fact_id":      env.FactID,
		"stable_fact_key":     env.StableFactKey,
		"source_system":       env.SourceRef.SourceSystem,
		"source_record_id":    env.SourceRef.SourceRecordID,
		"source_confidence":   string(env.SourceConfidence),
		"collector_kind":      env.CollectorKind,
		sourceOrderKeyField:   sourceOrderKey(env),
		// running_image_ref/running_image_digest (issue #5450): Azure
		// resources are never a running-image source, but both keys must still
		// be PRESENT (not omitted) so canonicalCloudResourceUpsertCypher's
		// unconditional SET clause does not hit the pinned NornicDB
		// missing-map-key-in-UNWIND bug — see go/internal/reducer/
		// aws_resource_running_image.go's runningImageFieldsAbsent doc for the
		// live-proved mechanism, and gcpCloudResourceNodeRow's parity-key
		// comment for the earlier #4995 precedent (workload_id/service_name/
		// service_anchor_*, which Azure rows do NOT yet carry — a pre-existing
		// gap tracked separately and out of scope here).
		"running_image_ref":    "",
		"running_image_digest": "",
	}
	return row, uid, resourceID, true, nil
}

// azureNormalizedResourceIDForResource returns the preferred join/uid identity
// for a decoded azurev1.CloudResource: NormalizedResourceID when present,
// falling back to the required ARMResourceID otherwise. It mirrors the
// pre-typing azureNormalizedResourceID payload-map helper.
func azureNormalizedResourceIDForResource(resource azurev1.CloudResource) string {
	if normalized := derefString(resource.NormalizedResourceID); normalized != "" {
		return normalized
	}
	return resource.ARMResourceID
}

type azureResourceMaterializationTiming struct {
	intent               Intent
	factCount            int
	nodeCount            int
	loadDuration         time.Duration
	extractDuration      time.Duration
	writeDuration        time.Duration
	phasePublishDuration time.Duration
	totalDuration        time.Duration
}

func logAzureResourceMaterializationCompleted(ctx context.Context, timing azureResourceMaterializationTiming) {
	slog.InfoContext(
		ctx, "azure resource materialization completed",
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
