package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingSecurityGroupRuleNodeWriter captures the rule-node write so tests can
// assert the exact materialization request the node handler issues.
type recordingSecurityGroupRuleNodeWriter struct {
	calls int
	rows  []map[string]any
	err   error
}

func (w *recordingSecurityGroupRuleNodeWriter) WriteSecurityGroupRuleNodes(
	_ context.Context, rows []map[string]any, _ string,
) error {
	w.calls++
	w.rows = append(w.rows, rows...)
	return w.err
}

// recordingPhasePublisher captures published readiness phases so tests can assert
// the node handler publishes the rule-uid canonical-nodes phase.
type recordingPhasePublisher struct {
	states []GraphProjectionPhaseState
	err    error
}

func (p *recordingPhasePublisher) PublishGraphProjectionPhases(_ context.Context, states []GraphProjectionPhaseState) error {
	p.states = append(p.states, states...)
	return p.err
}

func securityGroupRuleIntent() Intent {
	return Intent{
		IntentID:     "intent-sg-rule-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupRuleMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestSecurityGroupRuleMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := SecurityGroupRuleMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: &recordingSecurityGroupRuleNodeWriter{},
	}
	intent := securityGroupRuleIntent()
	intent.Domain = DomainSQLRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestSecurityGroupRuleMaterializationRequiresDependencies(t *testing.T) {
	t.Parallel()

	if _, err := (SecurityGroupRuleMaterializationHandler{NodeWriter: &recordingSecurityGroupRuleNodeWriter{}}).
		Handle(context.Background(), securityGroupRuleIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
	if _, err := (SecurityGroupRuleMaterializationHandler{FactLoader: &stubFactLoader{}}).
		Handle(context.Background(), securityGroupRuleIntent()); err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

// TestSecurityGroupRuleMaterializationWritesNodesAndPublishesPhase proves the
// node handler writes one rule node per resolved rule and publishes the
// security_group_rule_uid canonical-nodes-committed phase the edge slice gates on.
func TestSecurityGroupRuleMaterializationWritesNodesAndPublishesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupRuleNodeWriter{}
	publisher := &recordingPhasePublisher{}
	handler := SecurityGroupRuleMaterializationHandler{
		FactLoader:     &stubFactLoader{envelopes: securityGroupReachabilityFacts()},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}
	result, err := handler.Handle(context.Background(), securityGroupRuleIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.calls != 1 || len(writer.rows) != 1 {
		t.Fatalf("rule node writes wrong: calls=%d rows=%d", writer.calls, len(writer.rows))
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if len(publisher.states) != 1 {
		t.Fatalf("expected exactly one published phase, got %d", len(publisher.states))
	}
	state := publisher.states[0]
	if state.Key.Keyspace != GraphProjectionKeyspaceSecurityGroupRuleUID {
		t.Fatalf("phase keyspace = %q, want security_group_rule_uid", state.Key.Keyspace)
	}
	if state.Phase != GraphProjectionPhaseCanonicalNodesCommitted {
		t.Fatalf("phase = %q, want canonical_nodes_committed", state.Phase)
	}
}

// TestSecurityGroupRuleMaterializationEmptyGenerationStillPublishes proves an
// empty generation still publishes the readiness phase so the edge slice is not
// blocked forever on a scope that legitimately has no rules.
func TestSecurityGroupRuleMaterializationEmptyGenerationStillPublishes(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupRuleNodeWriter{}
	publisher := &recordingPhasePublisher{}
	handler := SecurityGroupRuleMaterializationHandler{
		FactLoader:     &stubFactLoader{},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}
	if _, err := handler.Handle(context.Background(), securityGroupRuleIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.calls != 0 {
		t.Fatalf("empty generation must not write nodes, got %d", writer.calls)
	}
	if len(publisher.states) != 1 {
		t.Fatalf("empty generation must still publish the phase, got %d", len(publisher.states))
	}
}

// TestSecurityGroupRuleMaterializationDoesNotPublishOnWriteError proves the phase
// is published only after a successful node write, so the edge slice never gates
// open against nodes that failed to commit.
func TestSecurityGroupRuleMaterializationDoesNotPublishOnWriteError(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupRuleNodeWriter{err: errors.New("boom")}
	publisher := &recordingPhasePublisher{}
	handler := SecurityGroupRuleMaterializationHandler{
		FactLoader:     &stubFactLoader{envelopes: securityGroupReachabilityFacts()},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}
	if _, err := handler.Handle(context.Background(), securityGroupRuleIntent()); err == nil {
		t.Fatal("expected the node write error to propagate")
	}
	if len(publisher.states) != 0 {
		t.Fatalf("phase must not publish on a failed node write, got %d", len(publisher.states))
	}
}

// TestSecurityGroupRuleMaterializationUnresolvedAnchorWritesNoNode proves a rule
// whose SG anchor was not scanned produces no rule node (no fabrication) but the
// phase still publishes so the edge slice can drain.
func TestSecurityGroupRuleMaterializationUnresolvedAnchorWritesNoNode(t *testing.T) {
	t.Parallel()

	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))
	writer := &recordingSecurityGroupRuleNodeWriter{}
	publisher := &recordingPhasePublisher{}
	handler := SecurityGroupRuleMaterializationHandler{
		FactLoader:     &stubFactLoader{envelopes: []facts.Envelope{rule}},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}
	if _, err := handler.Handle(context.Background(), securityGroupRuleIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.calls != 0 {
		t.Fatalf("unresolved anchor must write no node, got %d", writer.calls)
	}
	if len(publisher.states) != 1 {
		t.Fatalf("phase must still publish, got %d", len(publisher.states))
	}
}
