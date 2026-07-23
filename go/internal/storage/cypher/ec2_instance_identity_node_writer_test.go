// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func ec2InstanceIdentityNodeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":             "ec2-uid-" + string(rune('a'+i)),
			"ami_id":          "ami-0000000000000000a",
			"source_fact_id":  "fact-identity-1",
			"scope_id":        "scope-1",
			"generation_id":   "gen-1",
			"evidence_source": "reducer/ec2-instance-identity",
		})
	}
	return rows
}

func TestEC2InstanceIdentityNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceIdentityNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteEC2InstanceIdentityNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/ec2-instance-identity"); err != nil {
		t.Fatalf("WriteEC2InstanceIdentityNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestEC2InstanceIdentityNodeWriterMergesConfirmedExistingCloudResourceAndSetsAMIID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceIdentityNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteEC2InstanceIdentityNodes(context.Background(), ec2InstanceIdentityNodeRows(1), "scope-1", "gen-1", "reducer/ec2-instance-identity"); err != nil {
		t.Fatalf("WriteEC2InstanceIdentityNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Issue #5652: bare-MATCH-anchored UNWIND SET can silently drop its write on
	// the pinned production NornicDB image. MERGE is safe here only because
	// never-create is enforced in Go (filterRowsToExistingCloudResourceUIDs) —
	// see TestEC2InstanceIdentityNodeWriterNeverCreatesUnconfirmedCloudResource.
	if !strings.Contains(cypher, "MERGE (r:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE-anchor on the existing CloudResource uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "CREATE (r:CloudResource") {
		t.Fatalf("EC2 instance identity writer must not use bare CREATE:\n%s", cypher)
	}
	for _, want := range []string{
		"r.ami_id = row.ami_id",
		"r.ec2_identity_scope_id = row.scope_id",
		"r.ec2_identity_generation_id = row.generation_id",
		"r.ec2_identity_evidence_source = row.evidence_source",
		"r.ec2_identity_source_fact_id = row.source_fact_id",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
}

// TestEC2InstanceIdentityNodeWriterNeverCreatesUnconfirmedCloudResource proves
// the never-create contract: a candidate uid the existence reader does not
// confirm never reaches the write.
func TestEC2InstanceIdentityNodeWriterNeverCreatesUnconfirmedCloudResource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	reader := &echoingPostureExistenceReader{ExistingUIDs: map[string]bool{"ec2-uid-a": true}}
	writer := NewEC2InstanceIdentityNodeWriter(executor, reader, 0)

	rows := ec2InstanceIdentityNodeRows(1)
	rows = append(rows, map[string]any{"uid": "ec2-uid-missing", "ami_id": "ami-missing", "source_fact_id": "fact-2"})
	if err := writer.WriteEC2InstanceIdentityNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/ec2-instance-identity"); err != nil {
		t.Fatalf("WriteEC2InstanceIdentityNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	writtenRows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if len(writtenRows) != 1 {
		t.Fatalf("len(writtenRows) = %d, want 1 (only the confirmed-existing uid)", len(writtenRows))
	}
	if got := writtenRows[0]["uid"]; got != "ec2-uid-a" {
		t.Fatalf("writtenRows[0][uid] = %v, want ec2-uid-a", got)
	}
}

func TestEC2InstanceIdentityNodeWriterRequiresReader(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceIdentityNodeWriter(executor, nil, 0)

	if err := writer.WriteEC2InstanceIdentityNodes(context.Background(), ec2InstanceIdentityNodeRows(1), "scope-1", "gen-1", "reducer/ec2-instance-identity"); err == nil {
		t.Fatal("WriteEC2InstanceIdentityNodes() error = nil, want error for nil reader")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when reader is nil", len(executor.calls))
	}
}

func TestEC2InstanceIdentityNodeWriterRetractRemovesOnlyReducerOwnedProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceIdentityNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.RetractEC2InstanceIdentityNodes(context.Background(), []string{"scope-1"}, "gen-1", "reducer/ec2-instance-identity"); err != nil {
		t.Fatalf("RetractEC2InstanceIdentityNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (r:CloudResource)") {
		t.Fatalf("retract must match CloudResource nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "r.ec2_identity_scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by reducer identity scope id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "r.ec2_identity_evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by reducer evidence source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "REMOVE r.ami_id") {
		t.Fatalf("retract must REMOVE the ami_id property:\n%s", cypher)
	}
	if strings.Contains(cypher, "DELETE") || strings.Contains(cypher, "DETACH") {
		t.Fatalf("retract must not delete CloudResource nodes:\n%s", cypher)
	}
}

func TestEC2InstanceIdentityNodeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceIdentityNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.RetractEC2InstanceIdentityNodes(context.Background(), nil, "gen-1", "reducer/ec2-instance-identity"); err != nil {
		t.Fatalf("RetractEC2InstanceIdentityNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

func TestEC2InstanceIdentityNodeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	var _ interface {
		WriteEC2InstanceIdentityNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		RetractEC2InstanceIdentityNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
	} = NewEC2InstanceIdentityNodeWriter(&recordingExecutor{}, &echoingPostureExistenceReader{}, 0)
}

// setPropertyNames extracts every "r.<prop> = row." target property name from a
// writer's SET clause.
var setPropertyPattern = regexp.MustCompile(`r\.([a-z0-9_]+) = row\.`)

func setPropertyNames(cypher string) map[string]struct{} {
	names := make(map[string]struct{})
	for _, match := range setPropertyPattern.FindAllStringSubmatch(cypher, -1) {
		names[match[1]] = struct{}{}
	}
	return names
}

// removePropertyNames extracts every "r.<prop>" target named in a REMOVE
// clause.
var removePropertyPattern = regexp.MustCompile(`r\.([a-z0-9_]+)`)

func removePropertyNames(cypher string) map[string]struct{} {
	idx := strings.Index(cypher, "REMOVE")
	if idx < 0 {
		return nil
	}
	names := make(map[string]struct{})
	for _, match := range removePropertyPattern.FindAllStringSubmatch(cypher[idx:], -1) {
		names[match[1]] = struct{}{}
	}
	return names
}

// TestEC2InstanceIdentityWriterDisjointFromEC2InstancePostureWriter is the #5448
// CRUX-1 dual-writer safety proof: the identity writer's SET clause (and its
// retract's REMOVE clause) share ZERO property names with the EC2 instance
// posture node writer's SET clause (ec2_instance_node_writer.go). Both writers
// MERGE the identical CloudResource uid
// (cloudResourceUID(account,region,"aws_ec2_instance",instance_id)) for the
// same instance, dispatched as two SEPARATE reducer domains that may run in
// either order within a scope generation. Because the property sets are
// disjoint by construction, the writes commute: whichever domain runs first,
// second, or retries never overwrites a property the other domain owns, and
// the identity domain's retract can never delete a posture property (or vice
// versa, since the posture writer performs no generation-scoped retract at
// all today). This test fails the moment either writer's Cypher gains a
// property name already claimed by the other, closing the exact clobber risk
// #5448 raised.
func TestEC2InstanceIdentityWriterDisjointFromEC2InstancePostureWriter(t *testing.T) {
	t.Parallel()

	postureSet := setPropertyNames(canonicalEC2InstanceUpsertCypher)
	identitySet := setPropertyNames(canonicalEC2InstanceIdentityUpdateCypher)
	if len(postureSet) == 0 || len(identitySet) == 0 {
		t.Fatalf("expected non-empty SET property sets: posture=%d identity=%d", len(postureSet), len(identitySet))
	}
	for prop := range identitySet {
		if _, clash := postureSet[prop]; clash {
			t.Fatalf("identity writer SET property %q collides with the posture writer's base property set", prop)
		}
	}

	identityRemove := removePropertyNames(retractEC2InstanceIdentityPropertiesCypher)
	if len(identityRemove) == 0 {
		t.Fatal("expected the identity retract clause to name at least one property")
	}
	for prop := range identityRemove {
		if _, clash := postureSet[prop]; clash {
			t.Fatalf("identity retract REMOVE property %q collides with a posture writer base property; retracting identity would delete posture truth", prop)
		}
	}

	// The only property both writers legitimately reference by name is the
	// MERGE anchor "uid" itself, which is never part of either SET/REMOVE
	// clause's property set (it is the MERGE pattern's own key), so this also
	// asserts "uid" was correctly excluded from both extracted sets.
	if _, ok := postureSet["uid"]; ok {
		t.Fatal("uid must not appear as a SET property on the posture writer (it is the MERGE identity)")
	}
	if _, ok := identitySet["uid"]; ok {
		t.Fatal("uid must not appear as a SET property on the identity writer (it is the MERGE identity)")
	}
}
