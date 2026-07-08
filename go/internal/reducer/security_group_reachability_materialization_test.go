// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordingSecurityGroupReachabilityWriter captures the node, edge, and retract
// calls so tests assert the exact materialization request the handler issues.
type recordingSecurityGroupReachabilityWriter struct {
	ruleNodeCalls   int
	ruleNodeRows    []map[string]any
	sgEdgeCalls     int
	sgEdgeRows      []map[string]any
	toEdgeCalls     int
	toEdgeRows      []map[string]any
	retractCalls    int
	retractScopeIDs []string
	retractEvidence string
	nodeErr         error
	edgeErr         error

	// anchored-delete method (issue #4881)
	retractByUIDsCalls    int
	retractByUIDsUids     []string
	retractByUIDsScopes   []string
	retractByUIDsEvidence string
	retractByUIDsErr      error
}

func (w *recordingSecurityGroupReachabilityWriter) WriteSecurityGroupRuleNodes(
	_ context.Context, rows []map[string]any, _ string,
) error {
	w.ruleNodeCalls++
	w.ruleNodeRows = append(w.ruleNodeRows, rows...)
	return w.nodeErr
}

func (w *recordingSecurityGroupReachabilityWriter) WriteSecurityGroupSGRuleEdges(
	_ context.Context, rows []map[string]any, _, _, _ string,
) error {
	w.sgEdgeCalls++
	w.sgEdgeRows = append(w.sgEdgeRows, rows...)
	return w.edgeErr
}

func (w *recordingSecurityGroupReachabilityWriter) WriteSecurityGroupRuleEndpointEdges(
	_ context.Context, rows []map[string]any, _, _, _ string,
) error {
	w.toEdgeCalls++
	w.toEdgeRows = append(w.toEdgeRows, rows...)
	return w.edgeErr
}

func (w *recordingSecurityGroupReachabilityWriter) RetractSecurityGroupReachability(
	_ context.Context, scopeIDs []string, _, evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

func (w *recordingSecurityGroupReachabilityWriter) RetractSecurityGroupReachabilityByUIDs(
	_ context.Context, sourceUIDs []string, scopeIDs []string, evidenceSource string,
) error {
	w.retractByUIDsCalls++
	w.retractByUIDsUids = append(w.retractByUIDsUids, sourceUIDs...)
	w.retractByUIDsScopes = append(w.retractByUIDsScopes, scopeIDs...)
	w.retractByUIDsEvidence = evidenceSource
	return w.retractByUIDsErr
}

// allKeyspacesReady is a readiness lookup that reports the requested phase ready
// for every keyspace, so a test can exercise the post-gate path. The
// per-keyspace gating itself is covered by the not-ready tests below, which use a
// lookup that withholds exactly one keyspace.
func allKeyspacesReady() GraphProjectionReadinessLookup {
	return func(_ GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
		return true, true
	}
}

// readyExceptKeyspace reports every keyspace ready except the named one, which is
// reported not-found, so a test can prove the triple gate blocks until ALL three
// phases commit.
func readyExceptKeyspace(withheld GraphProjectionKeyspace) GraphProjectionReadinessLookup {
	return func(key GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
		if key.Keyspace == withheld {
			return false, false
		}
		return true, true
	}
}

func securityGroupReachabilityIntent() Intent {
	return Intent{
		IntentID:     "intent-sg-edges-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupReachabilityMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestSecurityGroupReachabilityRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		Writer:          &recordingSecurityGroupReachabilityWriter{},
		ReadinessLookup: allKeyspacesReady(),
	}
	intent := securityGroupReachabilityIntent()
	intent.Domain = DomainSQLRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestSecurityGroupReachabilityRequiresFactLoaderAndWriter(t *testing.T) {
	t.Parallel()

	if _, err := (SecurityGroupReachabilityMaterializationHandler{
		Writer:          &recordingSecurityGroupReachabilityWriter{},
		ReadinessLookup: allKeyspacesReady(),
	}).Handle(context.Background(), securityGroupReachabilityIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
	if _, err := (SecurityGroupReachabilityMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: allKeyspacesReady(),
	}).Handle(context.Background(), securityGroupReachabilityIntent()); err == nil {
		t.Fatal("expected error when writer is nil")
	}
}

// TestSecurityGroupReachabilityTripleGate proves the edge domain blocks (with a
// retryable error and zero writes) until ALL THREE canonical-nodes phases are
// committed: the rule keyspace, the endpoint keyspace, and the cloud-resource
// keyspace. Withholding any one keyspace must block.
func TestSecurityGroupReachabilityTripleGate(t *testing.T) {
	t.Parallel()

	for _, withheld := range []GraphProjectionKeyspace{
		GraphProjectionKeyspaceSecurityGroupRuleUID,
		GraphProjectionKeyspaceSecurityGroupEndpointUID,
		GraphProjectionKeyspaceCloudResourceUID,
	} {
		withheld := withheld
		t.Run(string(withheld), func(t *testing.T) {
			t.Parallel()
			writer := &recordingSecurityGroupReachabilityWriter{}
			handler := SecurityGroupReachabilityMaterializationHandler{
				FactLoader:      &stubFactLoader{},
				Writer:          writer,
				ReadinessLookup: readyExceptKeyspace(withheld),
			}
			_, err := handler.Handle(context.Background(), securityGroupReachabilityIntent())
			if err == nil {
				t.Fatalf("expected a retryable error while %s is not committed", withheld)
			}
			if !IsRetryable(err) {
				t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
			}
			if writer.ruleNodeCalls != 0 || writer.sgEdgeCalls != 0 || writer.toEdgeCalls != 0 || writer.retractCalls != 0 {
				t.Fatalf("no graph writes allowed before all phases commit: %+v", writer)
			}
		})
	}
}

func securityGroupReachabilityFacts() []facts.Envelope {
	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))
	return []facts.Envelope{anchor, rule}
}

// TestSecurityGroupReachabilityProjectsResolvedGraph proves the post-gate happy
// path writes one rule node, one SG->rule edge, one rule->endpoint edge, and
// returns the right canonical-write count.
func TestSecurityGroupReachabilityProjectsResolvedGraph(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupReachabilityWriter{}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: securityGroupReachabilityFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	result, err := handler.Handle(context.Background(), securityGroupReachabilityIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.ruleNodeCalls != 1 || len(writer.ruleNodeRows) != 1 {
		t.Fatalf("rule node writes wrong: calls=%d rows=%d", writer.ruleNodeCalls, len(writer.ruleNodeRows))
	}
	if writer.sgEdgeCalls != 1 || len(writer.sgEdgeRows) != 1 {
		t.Fatalf("SG->rule edge writes wrong: calls=%d rows=%d", writer.sgEdgeCalls, len(writer.sgEdgeRows))
	}
	if writer.toEdgeCalls != 1 || len(writer.toEdgeRows) != 1 {
		t.Fatalf("rule->endpoint edge writes wrong: calls=%d rows=%d", writer.toEdgeCalls, len(writer.toEdgeRows))
	}
	// one rule node + one SG edge + one TO edge = three canonical writes.
	if result.CanonicalWrites != 3 {
		t.Fatalf("CanonicalWrites = %d, want 3", result.CanonicalWrites)
	}
}

// TestSecurityGroupReachabilityIdempotentReprojection proves re-running the same
// generation issues the same writes (idempotent MERGE on node/edge uids) and
// retracts the prior generation's edges first.
func TestSecurityGroupReachabilityIdempotentReprojection(t *testing.T) {
	t.Parallel()

	newHandler := func(writer *recordingSecurityGroupReachabilityWriter) SecurityGroupReachabilityMaterializationHandler {
		return SecurityGroupReachabilityMaterializationHandler{
			FactLoader:           &stubFactLoader{envelopes: securityGroupReachabilityFacts()},
			Writer:               writer,
			ReadinessLookup:      allKeyspacesReady(),
			PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
		}
	}

	first := &recordingSecurityGroupReachabilityWriter{}
	if _, err := newHandler(first).Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("first projection error: %v", err)
	}
	second := &recordingSecurityGroupReachabilityWriter{}
	if _, err := newHandler(second).Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("second projection error: %v", err)
	}
	if first.ruleNodeRows[0]["uid"] != second.ruleNodeRows[0]["uid"] {
		t.Fatal("reprojection must produce the same rule node uid (idempotent MERGE)")
	}
	if second.retractCalls != 1 {
		t.Fatalf("reprojection with a prior generation must retract first, got %d", second.retractCalls)
	}
	if second.retractEvidence != securityGroupReachabilityEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", second.retractEvidence, securityGroupReachabilityEvidenceSource)
	}
}

// TestSecurityGroupReachabilitySkipsFirstGenerationRetract proves the first
// generation does not retract (no prior edges to remove).
func TestSecurityGroupReachabilitySkipsFirstGenerationRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupReachabilityWriter{}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: securityGroupReachabilityFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := handler.Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("first generation must not retract, got %d", writer.retractCalls)
	}
	if writer.ruleNodeCalls != 1 {
		t.Fatalf("first generation must still write nodes, got %d", writer.ruleNodeCalls)
	}
}

// TestSecurityGroupReachabilityUnresolvedAnchorIsGracefulNoEdge proves a rule
// whose SG anchor was not scanned produces no node and no edges but still
// succeeds (graceful degradation), and the skip counter records it.
func TestSecurityGroupReachabilityUnresolvedAnchorIsGracefulNoEdge(t *testing.T) {
	t.Parallel()

	// Only the rule fact, no anchor resource.
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))

	writer := &recordingSecurityGroupReachabilityWriter{}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{rule}},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	result, err := handler.Handle(context.Background(), securityGroupReachabilityIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.ruleNodeCalls != 0 || writer.sgEdgeCalls != 0 || writer.toEdgeCalls != 0 {
		t.Fatalf("unresolved anchor must write nothing: %+v", writer)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded (graceful degrade)", result.Status)
	}
}

func TestSecurityGroupReachabilityEmptyGenerationIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupReachabilityWriter{}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	result, err := handler.Handle(context.Background(), securityGroupReachabilityIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.ruleNodeCalls != 0 || writer.sgEdgeCalls != 0 || writer.toEdgeCalls != 0 {
		t.Fatalf("empty generation must write nothing: %+v", writer)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
}

func TestSecurityGroupReachabilityPropagatesWriteError(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupReachabilityWriter{nodeErr: errors.New("boom")}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: securityGroupReachabilityFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	if _, err := handler.Handle(context.Background(), securityGroupReachabilityIntent()); err == nil {
		t.Fatal("expected the node write error to propagate")
	}
}

// TestSecurityGroupReachabilityMetricsCountNodesEdgesAndSkips pins the telemetry
// contract: rule nodes counter, edges counter split by edge_type, and the
// skipped counter split by skip_reason.
func TestSecurityGroupReachabilityMetricsCountNodesEdgesAndSkips(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	resolved := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))
	// Anchor unscanned -> unresolved_anchor skip.
	unresolved := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-9xyz", "ingress", "tcp", int32(80), int32(80), "cidr_ipv4", "10.0.0.0/8",
	))

	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{anchor, resolved, unresolved}},
		Writer:               &recordingSecurityGroupReachabilityWriter{},
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
		Instruments:          inst,
	}
	if _, err := handler.Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if !metricHasAttrs(rm, "eshu_dp_security_group_reachability_edges_total", map[string]string{
		telemetry.MetricDimensionEdgeType: "sg_rule",
	}) {
		t.Fatal("edges counter must carry edge_type=sg_rule")
	}
	if !metricHasAttrs(rm, "eshu_dp_security_group_reachability_edges_total", map[string]string{
		telemetry.MetricDimensionEdgeType: "rule_endpoint",
	}) {
		t.Fatal("edges counter must carry edge_type=rule_endpoint")
	}
	if !metricHasAttrs(rm, "eshu_dp_security_group_reachability_skipped_total", map[string]string{
		telemetry.MetricDimensionSkipReason: "unresolved_anchor",
	}) {
		t.Fatal("skipped counter must carry skip_reason=unresolved_anchor")
	}
}
