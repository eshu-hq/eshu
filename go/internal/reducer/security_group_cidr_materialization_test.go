// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingSecurityGroupEndpointNodeWriter captures the rows handed to the
// CidrBlock and PrefixList node writers so tests can assert on the exact
// materialization request the reducer issues.
type recordingSecurityGroupEndpointNodeWriter struct {
	cidrCalls       int
	cidrRows        []map[string]any
	prefixCalls     int
	prefixRows      []map[string]any
	evidenceSources []string
	err             error
}

func (w *recordingSecurityGroupEndpointNodeWriter) WriteCidrBlockNodes(
	_ context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	w.cidrCalls++
	w.cidrRows = append(w.cidrRows, rows...)
	w.evidenceSources = append(w.evidenceSources, evidenceSource)
	return w.err
}

func (w *recordingSecurityGroupEndpointNodeWriter) WritePrefixListNodes(
	_ context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	w.prefixCalls++
	w.prefixRows = append(w.prefixRows, rows...)
	w.evidenceSources = append(w.evidenceSources, evidenceSource)
	return w.err
}

func securityGroupRuleEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSSecurityGroupRuleFactKind,
		Payload:  payload,
	}
}

// sgRulePayload builds a minimal aws_security_group_rule payload for the given
// normalized source endpoint. account_id/region match the shape the scanner
// envelope emits.
func sgRulePayload(sourceKind, sourceValue string) map[string]any {
	return map[string]any{
		"account_id":   "111122223333",
		"region":       "us-east-1",
		"service_kind": "ec2",
		"group_id":     "sg-0abc",
		"direction":    "ingress",
		"ip_protocol":  "tcp",
		"from_port":    int32(22),
		"to_port":      int32(22),
		"source_kind":  sourceKind,
		"source_value": sourceValue,
		"is_internet":  sourceValue == "0.0.0.0/0" || sourceValue == "::/0",
	}
}

func TestSecurityGroupCidrMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: &recordingSecurityGroupEndpointNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestSecurityGroupCidrMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := SecurityGroupCidrMaterializationHandler{
		NodeWriter: &recordingSecurityGroupEndpointNodeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestSecurityGroupCidrMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestExtractSecurityGroupEndpointRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	cidr, prefix := ExtractSecurityGroupEndpointRows(nil)
	if cidr != nil || prefix != nil {
		t.Fatalf("cidr=%v prefix=%v, want both nil", cidr, prefix)
	}
}

func TestExtractSecurityGroupEndpointRowsBuildsIPv4CidrNode(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/8")),
	}

	cidr, prefix := ExtractSecurityGroupEndpointRows(envelopes)
	if len(prefix) != 0 {
		t.Fatalf("len(prefix) = %d, want 0", len(prefix))
	}
	if len(cidr) != 1 {
		t.Fatalf("len(cidr) = %d, want 1", len(cidr))
	}
	if got := anyToString(cidr[0]["cidr"]); got != "10.0.0.0/8" {
		t.Fatalf("cidr = %q, want canonical 10.0.0.0/8", got)
	}
	if got := anyToString(cidr[0]["address_family"]); got != "ipv4" {
		t.Fatalf("address_family = %q, want ipv4", got)
	}
	if got, ok := cidr[0]["is_internet"].(bool); !ok || got {
		t.Fatalf("is_internet = %v, want false for a private block", cidr[0]["is_internet"])
	}
	if anyToString(cidr[0]["uid"]) == "" {
		t.Fatal("uid must be a non-empty deterministic identity")
	}
}

func TestExtractSecurityGroupEndpointRowsCanonicalizesHostBits(t *testing.T) {
	t.Parallel()

	// Two rules naming the same network with different host bits are the same
	// reachability endpoint and must converge on one canonical node.
	envelopes := []facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.1.2.3/8")),
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/8")),
	}

	cidr, _ := ExtractSecurityGroupEndpointRows(envelopes)
	if len(cidr) != 1 {
		t.Fatalf("len(cidr) = %d, want 1 (host bits must canonicalize to one node)", len(cidr))
	}
	if got := anyToString(cidr[0]["cidr"]); got != "10.0.0.0/8" {
		t.Fatalf("cidr = %q, want masked 10.0.0.0/8", got)
	}
}

func TestExtractSecurityGroupEndpointRowsCanonicalizesIPv6Casing(t *testing.T) {
	t.Parallel()

	// IPv6 differing only in casing / zero-compression is one endpoint.
	envelopes := []facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv6", "2001:DB8::/32")),
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv6", "2001:db8:0:0::/32")),
	}

	cidr, _ := ExtractSecurityGroupEndpointRows(envelopes)
	if len(cidr) != 1 {
		t.Fatalf("len(cidr) = %d, want 1 (IPv6 casing must canonicalize)", len(cidr))
	}
	if got := anyToString(cidr[0]["cidr"]); got != "2001:db8::/32" {
		t.Fatalf("cidr = %q, want 2001:db8::/32", got)
	}
	if got := anyToString(cidr[0]["address_family"]); got != "ipv6" {
		t.Fatalf("address_family = %q, want ipv6", got)
	}
}

func TestExtractSecurityGroupEndpointRowsDerivesInternetIPv4(t *testing.T) {
	t.Parallel()

	cidr, _ := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "0.0.0.0/0")),
	})
	if len(cidr) != 1 {
		t.Fatalf("len(cidr) = %d, want 1", len(cidr))
	}
	if got, ok := cidr[0]["is_internet"].(bool); !ok || !got {
		t.Fatalf("is_internet = %v, want true for 0.0.0.0/0", cidr[0]["is_internet"])
	}
}

func TestExtractSecurityGroupEndpointRowsDerivesInternetIPv6(t *testing.T) {
	t.Parallel()

	cidr, _ := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv6", "::/0")),
	})
	if len(cidr) != 1 {
		t.Fatalf("len(cidr) = %d, want 1", len(cidr))
	}
	if got, ok := cidr[0]["is_internet"].(bool); !ok || !got {
		t.Fatalf("is_internet = %v, want true for ::/0", cidr[0]["is_internet"])
	}
}

func TestExtractSecurityGroupEndpointRowsBuildsPrefixListNode(t *testing.T) {
	t.Parallel()

	cidr, prefix := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("prefix_list", "pl-123")),
	})
	if len(cidr) != 0 {
		t.Fatalf("len(cidr) = %d, want 0", len(cidr))
	}
	if len(prefix) != 1 {
		t.Fatalf("len(prefix) = %d, want 1", len(prefix))
	}
	if got := anyToString(prefix[0]["prefix_list_id"]); got != "pl-123" {
		t.Fatalf("prefix_list_id = %q, want pl-123", got)
	}
	if got := anyToString(prefix[0]["account_id"]); got != "111122223333" {
		t.Fatalf("account_id = %q, want 111122223333", got)
	}
	if got := anyToString(prefix[0]["region"]); got != "us-east-1" {
		t.Fatalf("region = %q, want us-east-1", got)
	}
	if anyToString(prefix[0]["uid"]) == "" {
		t.Fatal("prefix list uid must be a non-empty deterministic identity")
	}
}

func TestExtractSecurityGroupEndpointRowsSkipsReferencedGroupAndUnknown(t *testing.T) {
	t.Parallel()

	// A referenced security group already has a CloudResource node; this slice
	// must not materialize it. An unknown source materializes no endpoint node.
	cidr, prefix := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("referenced_security_group", "sg-9999")),
		securityGroupRuleEnvelope(sgRulePayload("unknown", "")),
	})
	if len(cidr) != 0 || len(prefix) != 0 {
		t.Fatalf("cidr=%d prefix=%d, want 0/0 for referenced-group and unknown sources", len(cidr), len(prefix))
	}
}

func TestExtractSecurityGroupEndpointRowsSkipsUnparseableCidr(t *testing.T) {
	t.Parallel()

	// A malformed CIDR cannot be canonicalized into a deterministic identity, so
	// it materializes no node rather than fabricating one.
	cidr, _ := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/33")),
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "not-a-cidr")),
	})
	if len(cidr) != 0 {
		t.Fatalf("len(cidr) = %d, want 0 for unparseable CIDRs", len(cidr))
	}
}

func TestExtractSecurityGroupEndpointRowsSkipsNonRuleFacts(t *testing.T) {
	t.Parallel()

	cidr, _ := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{"resource_id": "ignored"}},
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/8")),
	})
	if len(cidr) != 1 {
		t.Fatalf("len(cidr) = %d, want 1 (non-rule facts must be skipped)", len(cidr))
	}
}

func TestExtractSecurityGroupEndpointRowsSkipsTombstone(t *testing.T) {
	t.Parallel()

	tombstone := securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "172.16.0.0/12"))
	tombstone.IsTombstone = true
	cidr, _ := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		tombstone,
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/8")),
	})
	if len(cidr) != 1 {
		t.Fatalf("len(cidr) = %d, want 1 (a tombstoned rule must not materialize its endpoint)", len(cidr))
	}
	if got := anyToString(cidr[0]["cidr"]); got != "10.0.0.0/8" {
		t.Fatalf("cidr = %q, want 10.0.0.0/8", got)
	}
}

func TestExtractSecurityGroupEndpointRowsSortedByUID(t *testing.T) {
	t.Parallel()

	cidr, prefix := ExtractSecurityGroupEndpointRows([]facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "192.168.0.0/16")),
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/8")),
		securityGroupRuleEnvelope(sgRulePayload("prefix_list", "pl-zzz")),
		securityGroupRuleEnvelope(sgRulePayload("prefix_list", "pl-aaa")),
	})
	if len(cidr) != 2 {
		t.Fatalf("len(cidr) = %d, want 2", len(cidr))
	}
	if !sortedByUID(cidr) {
		t.Fatal("cidr rows must be sorted by uid for deterministic batch output")
	}
	if len(prefix) != 2 {
		t.Fatalf("len(prefix) = %d, want 2", len(prefix))
	}
	if !sortedByUID(prefix) {
		t.Fatal("prefix rows must be sorted by uid for deterministic batch output")
	}
}

func sortedByUID(rows []map[string]any) bool {
	for i := 1; i < len(rows); i++ {
		if anyToString(rows[i-1]["uid"]) > anyToString(rows[i]["uid"]) {
			return false
		}
	}
	return true
}
