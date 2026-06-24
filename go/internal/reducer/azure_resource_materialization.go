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
	rows := ExtractAzureCloudResourceNodeRows(envelopes)
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
	}, nil
}

// ExtractAzureCloudResourceNodeRows projects azure_cloud_resource facts into
// deterministic CloudResource rows keyed by normalized ARM identity.
func ExtractAzureCloudResourceNodeRows(envelopes []facts.Envelope) []map[string]any {
	if len(envelopes) == 0 {
		return nil
	}
	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.AzureCloudResourceFactKind || env.IsTombstone {
			continue
		}
		row, uid, ok := azureCloudResourceNodeRow(env)
		if !ok {
			continue
		}
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

func azureCloudResourceNodeRow(env facts.Envelope) (map[string]any, string, bool) {
	resourceID := azureNormalizedResourceID(env.Payload)
	resourceType := payloadString(env.Payload, "resource_type")
	subscriptionID := payloadString(env.Payload, "subscription_id")
	location := payloadString(env.Payload, "location")
	if resourceID == "" || resourceType == "" {
		return nil, "", false
	}
	uid := cloudResourceUID(subscriptionID, location, resourceType, resourceID)
	row := map[string]any{
		"uid":                 uid,
		"arn":                 "",
		"resource_id":         resourceID,
		"resource_type":       resourceType,
		"name":                payloadString(env.Payload, "resource_name"),
		"state":               "",
		"account_id":          subscriptionID,
		"region":              location,
		"service_kind":        payloadString(env.Payload, "provider_namespace"),
		"correlation_anchors": payloadStrings(env.Payload, "", "correlation_anchors"),
		"source_fact_id":      env.FactID,
		"stable_fact_key":     env.StableFactKey,
		"source_system":       env.SourceRef.SourceSystem,
		"source_record_id":    env.SourceRef.SourceRecordID,
		"source_confidence":   string(env.SourceConfidence),
		"collector_kind":      env.CollectorKind,
	}
	return row, uid, true
}

func azureNormalizedResourceID(payload map[string]any) string {
	if normalized := payloadString(payload, "normalized_resource_id"); normalized != "" {
		return normalized
	}
	return payloadString(payload, "arm_resource_id")
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
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("node_count", timing.nodeCount),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
