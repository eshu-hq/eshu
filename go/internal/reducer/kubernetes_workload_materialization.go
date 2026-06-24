package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// kubernetesWorkloadMaterializationDomainDefinition returns the additive
// definition for live KubernetesWorkload node materialization. It is additive
// (not part of DefaultDomainDefinitions) because the handler requires an
// explicitly wired KubernetesWorkloadNodeWriter and FactLoader; registering it
// without them would silently drop every intent. The live-workload edge slice
// (#388 PR3) joins against the nodes this domain commits. See issue #388 and
// docs/internal/design/388-kubernetes-correlation-readmodel.md.
func kubernetesWorkloadMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainKubernetesWorkloadMaterialization,
		Summary: "materialize kubernetes_live.pod_template facts into canonical KubernetesWorkload graph nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "kubernetes_workload_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// kubernetesWorkloadEvidenceSource tags KubernetesWorkload nodes written by this
// reducer so the prior-generation retract path (and the future edge projection)
// can scope its writes to reducer-owned live-workload materialization.
const kubernetesWorkloadEvidenceSource = "reducer/kubernetes-workloads"

// KubernetesWorkloadNodeWriter persists canonical KubernetesWorkload graph nodes
// from extracted node rows. Implementations MUST be idempotent by node uid (the
// collector-emitted object_id) so reducer retries and duplicate facts converge
// on one node rather than duplicating or fabricating graph state.
type KubernetesWorkloadNodeWriter interface {
	WriteKubernetesWorkloadNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
}

// KubernetesWorkloadMaterializationHandler reduces one live Kubernetes workload
// materialization follow-up into canonical KubernetesWorkload node writes. It
// loads the cluster scope generation's kubernetes_live.pod_template facts,
// projects them into deterministic node rows keyed by the collector-emitted
// object_id, and hands the bounded batch to the node writer.
//
// This handler is the live-workload node substrate that the #388 edge projection
// (PR3) joins against. It intentionally does not write edges: edges are resolved
// against these nodes in a separate, gated stage, mirroring how the AWS
// relationship edge projection (#805) joins against CloudResource nodes.
//
// After the canonical node write succeeds, the handler publishes the
// GraphProjectionKeyspaceKubernetesWorkloadUID /
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase. The edge slice
// gates its projection on this phase, so edges never resolve against a
// generation whose nodes have not yet committed.
type KubernetesWorkloadMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter KubernetesWorkloadNodeWriter
	// PhasePublisher records the canonical-nodes-committed readiness phase that
	// gates the live-workload edge projection. A nil publisher is a no-op so the
	// additive domain stays safe to register before the edge slice is wired.
	PhasePublisher GraphProjectionPhasePublisher
	// PresenceWriter records uid-exact endpoint presence for committed
	// KubernetesWorkload nodes so the cross-scope secrets/IAM projection gate can
	// prove a specific node committed (issue #1380). It is nil unless the
	// secrets/IAM graph projection feature is enabled, so the default hot path
	// carries no extra write.
	PresenceWriter EndpointPresenceWriter
	// Instruments records the nodes-materialized counter. Nil-safe.
	Instruments *telemetry.Instruments
}

// Handle executes one live Kubernetes workload materialization intent.
func (h KubernetesWorkloadMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainKubernetesWorkloadMaterialization {
		return Result{}, fmt.Errorf(
			"kubernetes workload materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("kubernetes workload materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("kubernetes workload materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.KubernetesPodTemplateFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for kubernetes workload materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows := ExtractKubernetesWorkloadNodeRows(envelopes)
	extractDuration := time.Since(extractStart)

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteKubernetesWorkloadNodes(ctx, rows, kubernetesWorkloadEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical kubernetes workload nodes: %w", err)
		}
		writeDuration = time.Since(writeStart)

		// Record uid-exact presence for the committed KubernetesWorkload nodes so
		// the cross-scope secrets/IAM projection gate can prove these endpoints
		// exist. Flag-gated: a nil PresenceWriter (the default) makes this a no-op.
		if err := publishEndpointPresence(
			ctx, h.PresenceWriter,
			GraphProjectionKeyspaceKubernetesWorkloadUID, intent.ScopeID, rows, time.Now().UTC(),
		); err != nil {
			return Result{}, fmt.Errorf("record kubernetes workload endpoint presence: %w", err)
		}
	}

	// Publish the canonical-nodes-committed readiness phase only after the node
	// write succeeds (or is a legitimate no-op for an empty generation). The edge
	// slice gates its projection on this phase: publishing before a successful
	// write would let edges resolve against nodes that never committed, and not
	// publishing on an empty generation would block the edge slice forever.
	phasePublishStart := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceKubernetesWorkloadUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return Result{}, fmt.Errorf("publish canonical kubernetes workload nodes phase: %w", err)
	}
	phasePublishDuration := time.Since(phasePublishStart)

	h.recordNodesMaterialized(ctx, len(rows))
	logKubernetesWorkloadMaterializationCompleted(ctx, kubernetesWorkloadMaterializationTiming{
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
		Domain:   DomainKubernetesWorkloadMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d canonical kubernetes workload node(s) from %d pod-template fact(s)",
			len(rows),
			len(envelopes),
		),
		CanonicalWrites: len(rows),
	}, nil
}

// recordNodesMaterialized emits the KubernetesWorkloadNodes counter so an
// operator can see how many live-workload nodes one generation committed, which
// is the substrate the later edge slice resolves against. A zero count for a
// non-empty generation is itself a signal (every pod template lacked an
// object_id), so the counter is recorded even when no rows materialized.
func (h KubernetesWorkloadMaterializationHandler) recordNodesMaterialized(ctx context.Context, count int) {
	if h.Instruments == nil || h.Instruments.KubernetesWorkloadNodes == nil {
		return
	}
	h.Instruments.KubernetesWorkloadNodes.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrDomain(string(DomainKubernetesWorkloadMaterialization)),
	))
}

// ExtractKubernetesWorkloadNodeRows projects kubernetes_live.pod_template fact
// envelopes into deterministic KubernetesWorkload node rows keyed by the stable
// collector-emitted object_id. Rows are deduplicated by object_id so duplicate
// facts (retries, overlapping snapshots) converge on a single node; tombstoned
// pod templates (a deleted workload no longer running) and facts missing an
// object_id are dropped rather than fabricating or asserting a phantom node. The
// returned rows are sorted by uid for deterministic batch output.
func ExtractKubernetesWorkloadNodeRows(envelopes []facts.Envelope) []map[string]any {
	if len(envelopes) == 0 {
		return nil
	}

	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.KubernetesPodTemplateFactKind {
			continue
		}
		// A tombstoned live workload no longer runs, so it materializes no node;
		// reading it would assert a graph node for a workload that no longer exists.
		if env.IsTombstone {
			continue
		}
		row, uid, ok := kubernetesWorkloadNodeRow(env)
		if !ok {
			continue
		}
		// Last fact for a uid wins; identity is stable so the choice only affects
		// mutable properties, and idempotent MERGE makes it safe.
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

// kubernetesWorkloadNodeRow builds one KubernetesWorkload node row from a
// pod-template fact envelope, returning ok=false when the fact lacks the
// collector-emitted object_id that forms the stable node uid. The node uid is
// the object_id exactly; the raw Kubernetes metadata.uid is carried as the
// workload_uid property only, never the node identity (the object_id already
// folds metadata.uid into its identity tuple).
func kubernetesWorkloadNodeRow(env facts.Envelope) (map[string]any, string, bool) {
	objectID := payloadString(env.Payload, "object_id")
	if objectID == "" {
		return nil, "", false
	}
	row := map[string]any{
		"uid":                    objectID,
		"cluster_id":             payloadString(env.Payload, "cluster_id"),
		"namespace":              payloadString(env.Payload, "namespace"),
		"name":                   payloadString(env.Payload, "name"),
		"workload_uid":           payloadString(env.Payload, "uid"),
		"group_version_resource": payloadString(env.Payload, "group_version_resource"),
		"service_account":        payloadString(env.Payload, "service_account"),
		"image_refs":             payloadStrings(env.Payload, "", "image_refs"),
		"selector":               flattenSelector(env.Payload["selector"]),
		"correlation_anchors":    payloadStrings(env.Payload, "", "correlation_anchors"),
		"source_fact_id":         env.FactID,
		"stable_fact_key":        env.StableFactKey,
		"source_system":          env.SourceRef.SourceSystem,
		"source_record_id":       env.SourceRef.SourceRecordID,
		"source_confidence":      string(env.SourceConfidence),
		"collector_kind":         env.CollectorKind,
	}
	return row, objectID, true
}

// flattenSelector renders a pod-template label selector map into a deterministic
// sorted slice of "key=value" strings. Graph property values must be scalar or a
// homogeneous list, so the selector map is flattened rather than stored as a map.
// The order is sorted so retries and reprojections produce a byte-stable row.
func flattenSelector(raw any) []string {
	pairs := selectorPairs(raw)
	if len(pairs) == 0 {
		return nil
	}
	sort.Strings(pairs)
	return pairs
}

func selectorPairs(raw any) []string {
	switch typed := raw.(type) {
	case map[string]string:
		pairs := make([]string, 0, len(typed))
		for key, value := range typed {
			if key = strings.TrimSpace(key); key != "" {
				pairs = append(pairs, key+"="+value)
			}
		}
		return pairs
	case map[string]any:
		pairs := make([]string, 0, len(typed))
		for key, value := range typed {
			if key = strings.TrimSpace(key); key != "" {
				pairs = append(pairs, key+"="+fmt.Sprint(value))
			}
		}
		return pairs
	default:
		return nil
	}
}

// kubernetesWorkloadMaterializationTiming groups stage durations so the
// completion log can identify whether live-workload node work is fact loading,
// extraction, or graph backend time.
type kubernetesWorkloadMaterializationTiming struct {
	intent               Intent
	factCount            int
	nodeCount            int
	loadDuration         time.Duration
	extractDuration      time.Duration
	writeDuration        time.Duration
	phasePublishDuration time.Duration
	totalDuration        time.Duration
}

func logKubernetesWorkloadMaterializationCompleted(
	ctx context.Context,
	timing kubernetesWorkloadMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "kubernetes workload materialization completed",
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
