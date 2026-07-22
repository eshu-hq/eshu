// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/environment"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// namespaceEnvironmentLabelKeys is the small documented set of Kubernetes
// namespace label keys checked, in order, for an alias-recognized
// environment declaration (issue #5434). No file in this repo sanctions a
// single well-known key --
// docs/public/reference/environment-alias-contract.md lists namespace_label
// as "Defined for #5434" with no key named -- so this domain checks a plain
// "environment" key plus the Kubernetes-recommended
// "app.kubernetes.io/environment" label (the "common labels" convention,
// https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/).
// The first key present whose value normalizes to a KNOWN token wins,
// exactly mirroring cross_repo_evidence_artifacts.go's isKnownEnvironmentToken
// gate: environment.Canonical never rejects an unknown value, so the
// no-invented-environments rule requires the IsKnownToken check before a
// label is trusted, not merely non-empty.
var namespaceEnvironmentLabelKeys = []string{
	"environment",
	"app.kubernetes.io/environment",
}

// namespaceEnvironmentFromLabels decides, for one namespace's label set,
// whether an alias-recognized environment label is present. It returns the
// canonical environment name and true when a recognized label is found; an
// absent or unrecognized value at every checked key returns ("", false) --
// StateEnvironmentUnbound, never an invented environment.
func namespaceEnvironmentFromLabels(labels map[string]string) (canonical string, bound bool) {
	for _, key := range namespaceEnvironmentLabelKeys {
		raw, present := labels[key]
		if !present {
			continue
		}
		normalized := environment.Normalize(raw)
		if !environment.IsKnownToken(normalized) {
			continue
		}
		return environment.Canonical(normalized), true
	}
	return "", false
}

// kubernetesNamespaceMaterializationDomainDefinition returns the additive
// definition for live KubernetesNamespace node materialization. It is
// additive (not part of DefaultDomainDefinitions) because the handler
// requires an explicitly wired KubernetesNamespaceNodeWriter and FactLoader;
// registering it without them would silently drop every intent, mirroring
// kubernetesWorkloadMaterializationDomainDefinition. See issue #5434.
func kubernetesNamespaceMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainKubernetesNamespaceMaterialization,
		Summary: "materialize kubernetes_live.namespace facts into canonical KubernetesNamespace graph nodes, binding an Environment only via an alias-recognized label",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "kubernetes_namespace_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// kubernetesNamespaceEvidenceSource tags KubernetesNamespace nodes (and any
// Environment binding) written by this reducer so retract/repair paths can
// scope their writes to this domain's own materialization.
const kubernetesNamespaceEvidenceSource = "reducer/kubernetes-namespaces"

// KubernetesNamespaceNodeWriter persists canonical KubernetesNamespace graph
// nodes from extracted node rows, binding an Environment node only for rows
// carrying a non-empty "environment" property. Implementations MUST be
// idempotent by node uid (the collector-emitted object_id) so reducer
// retries and duplicate facts converge on one node rather than duplicating
// or fabricating graph state, and MUST NOT create an Environment node for a
// row with no environment value. Implementations MUST also retract stale
// namespace nodes only when the handler explicitly identifies a complete
// cluster snapshot; partial snapshots must remain additive.
type KubernetesNamespaceNodeWriter interface {
	WriteKubernetesNamespaceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	RetractStaleKubernetesNamespaceNodes(ctx context.Context, clusterID, generationID, evidenceSource string) error
}

// KubernetesNamespaceMaterializationHandler reduces one live Kubernetes
// namespace materialization follow-up into canonical KubernetesNamespace
// node writes, binding each namespace's Environment only when an
// alias-recognized label is present (issue #5434). It loads the cluster
// scope generation's kubernetes_live.namespace facts, projects them into
// deterministic node rows keyed by the collector-emitted object_id, and
// hands the bounded batch to the node writer.
type KubernetesNamespaceMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter KubernetesNamespaceNodeWriter
	// Instruments records the nodes-materialized counter. Nil-safe.
	Instruments *telemetry.Instruments
}

// Handle executes one live Kubernetes namespace materialization intent.
func (h KubernetesNamespaceMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainKubernetesNamespaceMaterialization {
		return Result{}, fmt.Errorf(
			"kubernetes namespace materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("kubernetes namespace materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("kubernetes namespace materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.KubernetesNamespaceFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for kubernetes namespace materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, boundCount, quarantined, err := ExtractKubernetesNamespaceNodeRows(envelopes)
	if err != nil {
		return Result{}, err
	}
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainKubernetesNamespaceMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	reconcileComplete, _ := intent.Payload["reconcile_complete"].(bool)
	clusterID := ""
	if reconcileComplete {
		clusterID, _ = intent.Payload["cluster_id"].(string)
		clusterID = strings.TrimSpace(clusterID)
		if clusterID == "" {
			return Result{}, fmt.Errorf("reconcile complete kubernetes namespace snapshot requires cluster_id")
		}
		for _, row := range rows {
			rowClusterID, _ := row["cluster_id"].(string)
			if strings.TrimSpace(rowClusterID) != clusterID {
				return Result{}, fmt.Errorf(
					"reconcile complete kubernetes namespace snapshot cluster_id %q does not match row cluster_id %q",
					clusterID,
					strings.TrimSpace(rowClusterID),
				)
			}
		}
	}
	for _, row := range rows {
		row["generation_id"] = intent.GenerationID
	}
	extractDuration := time.Since(extractStart)

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteKubernetesNamespaceNodes(ctx, rows, kubernetesNamespaceEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical kubernetes namespace nodes: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}
	reconciliationStatus := "not_requested"
	if reconcileComplete {
		reconciliationStatus = "suppressed_input_invalid"
	}
	if reconcileComplete && inputInvalidCount == 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.RetractStaleKubernetesNamespaceNodes(
			ctx,
			clusterID,
			intent.GenerationID,
			kubernetesNamespaceEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract stale canonical kubernetes namespace nodes: %w", err)
		}
		writeDuration += time.Since(writeStart)
		reconciliationStatus = "applied"
	}

	logKubernetesNamespaceMaterializationCompleted(ctx, kubernetesNamespaceMaterializationTiming{
		intent:          intent,
		factCount:       len(envelopes),
		nodeCount:       len(rows),
		boundCount:      boundCount,
		reconciliation:  reconciliationStatus,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainKubernetesNamespaceMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d canonical kubernetes namespace node(s) (%d environment-bound) from %d namespace fact(s); %d input_invalid fact(s) quarantined; reconciliation=%s",
			len(rows),
			boundCount,
			len(envelopes),
			inputInvalidCount,
			reconciliationStatus,
		),
		CanonicalWrites: len(rows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// ExtractKubernetesNamespaceNodeRows projects kubernetes_live.namespace fact
// envelopes into deterministic KubernetesNamespace node rows keyed by the
// stable collector-emitted object_id. Each fact is decoded through the
// factschema seam, so a payload missing the required object_id key is
// quarantined as a per-fact input_invalid dead-letter (returned in the
// []quarantinedFact slice) rather than fabricating a node with an
// empty-string uid: every valid fact still projects. For each namespace,
// namespaceEnvironmentFromLabels decides the environment binding; a row
// carries a non-empty "environment" property ONLY when an alias-recognized
// label was found, so the writer never creates an Environment node for an
// unbound namespace. Rows are deduplicated by object_id so duplicate facts
// converge on a single node. The returned rows are sorted by uid for
// deterministic batch output. Mirrors ExtractKubernetesWorkloadNodeRows.
func ExtractKubernetesNamespaceNodeRows(envelopes []facts.Envelope) ([]map[string]any, int, []quarantinedFact, error) {
	if len(envelopes) == 0 {
		return nil, 0, nil, nil
	}

	var quarantined []quarantinedFact
	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.KubernetesNamespaceFactKind {
			continue
		}
		if env.IsTombstone {
			continue
		}
		row, uid, ok, err := kubernetesNamespaceNodeRow(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, 0, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !ok {
			continue
		}
		if preferMaxSourceOrderKey(byUID[uid], row) {
			byUID[uid] = row
		}
	}

	if len(byUID) == 0 {
		return nil, 0, quarantined, nil
	}

	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)

	rows := make([]map[string]any, 0, len(uids))
	boundCount := 0
	for _, uid := range uids {
		row := byUID[uid]
		rows = append(rows, row)
		if env, _ := row["environment"].(string); env != "" {
			boundCount++
		}
	}
	return rows, boundCount, quarantined, nil
}

// kubernetesNamespaceNodeRow builds one KubernetesNamespace node row from a
// namespace fact envelope, decoding the payload through the factschema seam.
// It returns ok=false (with a nil error) only for a decoded-but-empty
// object_id.
func kubernetesNamespaceNodeRow(env facts.Envelope) (map[string]any, string, bool, error) {
	namespace, err := decodeKubernetesLiveNamespace(env)
	if err != nil {
		return nil, "", false, err
	}

	objectID := namespace.ObjectID
	if objectID == "" {
		return nil, "", false, nil
	}

	canonicalEnv, bound := namespaceEnvironmentFromLabels(namespace.Labels)
	state := string(environment.StateEnvironmentUnbound)
	evidenceClass := ""
	if bound {
		state = string(environment.StateBound)
		evidenceClass = string(environment.EvidenceClassNamespaceLabel)
	}

	row := map[string]any{
		"uid":                 objectID,
		"cluster_id":          derefString(namespace.ClusterID),
		"namespace":           derefString(namespace.Namespace),
		"labels":              flattenSelectorMap(namespace.Labels),
		"correlation_anchors": uniqueSortedStrings(namespace.CorrelationAnchors),
		"environment":         canonicalEnv,
		"environment_state":   state,
		"evidence_class":      evidenceClass,
		"source_fact_id":      env.FactID,
		"stable_fact_key":     env.StableFactKey,
		"source_system":       env.SourceRef.SourceSystem,
		"source_record_id":    env.SourceRef.SourceRecordID,
		"source_confidence":   string(env.SourceConfidence),
		"collector_kind":      env.CollectorKind,
		sourceOrderKeyField:   sourceOrderKey(env),
	}
	return row, objectID, true, nil
}

type kubernetesNamespaceMaterializationTiming struct {
	intent          Intent
	factCount       int
	nodeCount       int
	boundCount      int
	reconciliation  string
	loadDuration    time.Duration
	extractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logKubernetesNamespaceMaterializationCompleted(
	ctx context.Context,
	timing kubernetesNamespaceMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "kubernetes namespace materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("node_count", timing.nodeCount),
		slog.Int("environment_bound_count", timing.boundCount),
		slog.String("reconciliation_status", timing.reconciliation),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
