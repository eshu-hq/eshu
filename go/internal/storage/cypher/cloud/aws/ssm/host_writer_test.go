// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssm

import (
	"context"
	"strings"
	"testing"
	"time"
)

func ssmTestHost() HostTuple {
	return HostTuple{Partition: "aws", AccountID: "000000000001", Region: "us-east-1", InstanceID: "i-1"}
}

func ssmAppRow(uid, capture string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": "i-1", "resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application",
		"schema_version": "1.0.0", "capture_time": capture, "name": "alpha",
		"source_fact_id": "a1", "source_order_key": "k1",
	}
}

func ssmPlatformRow(uid string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": "i-1", "resource_type": "AWS::EC2::Instance", "source_type": "ssm_inventory",
		"platform_type": "Linux", "observation_time": "2026-10-29T22:33:02Z",
		"source_fact_id": "p1", "source_order_key": "k1",
	}
}

func TestHostWriterMergesApplicationNodesByStableUID(t *testing.T) {
	executor := &recordingExecutor{}
	writer := NewHostNodeWriter(executor, nil, 0)
	rows := []map[string]any{ssmAppRow("uid-app-1", "2026-10-29T22:31:19Z")}
	if err := writer.WriteApplicationNodes(context.Background(), rows, "scope-1", "gen-2", "reducer/ssm-host-inventory"); err != nil {
		t.Fatalf("WriteApplicationNodes: %v", err)
	}
	if len(executor.statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(executor.statements))
	}
	cypher := executor.statements[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MERGE (n:SSMHostApplication {uid: row.uid})",
		"n.capture_time = row.capture_time",
		"n.name = row.name",
		"n.scope_id = row.scope_id",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "CREATE ") {
		t.Fatalf("cypher must never CREATE unconditionally:\n%s", cypher)
	}
	params := executor.statements[0].Parameters["rows"].([]map[string]any)
	if params[0]["scope_id"] != "scope-1" || params[0]["generation_id"] != "gen-2" {
		t.Fatalf("rows not annotated with scope/generation: %v", params[0])
	}
	if params[0]["capture_time_unix"] != int64(1793313079) {
		t.Fatalf("capture_time_unix = %v, want 1793313079", params[0]["capture_time_unix"])
	}
}

func TestHostWriterEmptyRowsAreNoOp(t *testing.T) {
	executor := &recordingExecutor{}
	writer := NewHostNodeWriter(executor, nil, 0)
	if err := writer.WriteApplicationNodes(context.Background(), nil, "scope-1", "gen-2", "src"); err != nil {
		t.Fatalf("WriteApplicationNodes: %v", err)
	}
	if err := writer.WritePlatformNodes(context.Background(), nil, "scope-1", "gen-2", "src"); err != nil {
		t.Fatalf("WritePlatformNodes: %v", err)
	}
	if err := writer.WriteReportedWithEdges(context.Background(), nil, "scope-1", "gen-2", "src"); err != nil {
		t.Fatalf("WriteReportedWithEdges: %v", err)
	}
	if len(executor.statements) != 0 {
		t.Fatalf("statements = %d, want 0", len(executor.statements))
	}
}

func TestHostWriterEdgesDispatchAfterNodesCommit(t *testing.T) {
	executor := &recordingExecutor{}
	writer := NewHostNodeWriter(executor, nil, 0)
	apps := []map[string]any{ssmAppRow("uid-app-1", "2026-10-29T22:31:19Z")}
	plats := []map[string]any{ssmPlatformRow("uid-plat-1")}
	edges := []map[string]any{{"from_uid": "uid-app-1", "to_uid": "uid-plat-1", "relationship": "REPORTED_WITH"}}
	if err := writer.WriteHostObservationBatch(context.Background(), apps, plats, edges, "scope-1", "gen-2", "src"); err != nil {
		t.Fatalf("WriteHostObservationBatch: %v", err)
	}
	if len(executor.statements) != 3 {
		t.Fatalf("statements = %d, want 3 (apps, platforms, edges as separate dispatches)", len(executor.statements))
	}
	edgeCypher := executor.statements[2].Cypher
	for _, want := range []string{
		"MATCH (source:SSMHostApplication {uid: row.from_uid})",
		"MATCH (target:SSMHostPlatform {uid: row.to_uid})",
		"MERGE (source)-[rel:REPORTED_WITH]->(target)",
	} {
		if !strings.Contains(edgeCypher, want) {
			t.Fatalf("edge cypher missing %q:\n%s", want, edgeCypher)
		}
	}
}

func TestHostWriterRejectsForeignRelationshipType(t *testing.T) {
	executor := &recordingExecutor{}
	writer := NewHostNodeWriter(executor, nil, 0)
	edges := []map[string]any{{"from_uid": "a", "to_uid": "b", "relationship": "DEPENDS_ON"}}
	if err := writer.WriteReportedWithEdges(context.Background(), edges, "scope-1", "gen-2", "src"); err == nil {
		t.Fatalf("WriteReportedWithEdges accepted DEPENDS_ON, want a closed-vocabulary rejection")
	}
	if len(executor.statements) != 0 {
		t.Fatalf("statements = %d after rejection, want 0", len(executor.statements))
	}
}

func TestHostWriterRetractsOlderCapturesOnly(t *testing.T) {
	executor := &recordingExecutor{}
	reader := &stubCommittedReader{rows: []map[string]any{{"stale_count": 2}}}
	writer := NewHostNodeWriter(executor, reader, 0)
	cutoff, _ := time.Parse(time.RFC3339, "2026-10-29T22:31:19Z")
	retracted, err := writer.RetractStaleApplicationNodes(context.Background(), "scope-1", "src", ssmTestHost(), cutoff)
	if err != nil {
		t.Fatalf("RetractStaleApplicationNodes: %v", err)
	}
	if retracted != 2 {
		t.Fatalf("retracted = %d, want 2", retracted)
	}
	if len(executor.statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(executor.statements))
	}
	cypher := executor.statements[0].Cypher
	for _, want := range []string{
		"SSMHostApplication",
		"n.capture_time_unix < $cutoff_unix",
		"DETACH DELETE n",
		"n.scope_id = $scope_id",
		"n.evidence_source = $evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "n.capture_time < ") {
		t.Fatalf("retract must compare capture_time_unix ints, never raw RFC3339 strings (offsets break lexicographic order):\n%s", cypher)
	}
}

func TestHostWriterRetractNeverRunsGrouped(t *testing.T) {
	executor := &recordingGroupExecutor{}
	reader := &stubCommittedReader{rows: []map[string]any{{"stale_count": 0}}}
	writer := NewHostNodeWriter(executor, reader, 0)
	cutoff, _ := time.Parse(time.RFC3339, "2026-10-29T22:31:19Z")
	if _, err := writer.RetractStaleApplicationNodes(context.Background(), "scope-1", "src", ssmTestHost(), cutoff); err != nil {
		t.Fatalf("RetractStaleApplicationNodes: %v", err)
	}
	if executor.groupCalls != 0 {
		t.Fatalf("groupCalls = %d, want 0 (retracts run sequential Execute, never ExecuteGroup)", executor.groupCalls)
	}
}

func TestHostWriterReadsCommittedCaptures(t *testing.T) {
	executor := &recordingExecutor{}
	hostColumns := map[string]any{
		"host_partition": "aws", "host_account_id": "000000000001", "host_region": "us-east-1", "host_instance_id": "i-1",
	}
	appRow := map[string]any{"node_label": "SSMHostApplication", "capture_time_unix": int64(1793313079)}
	platRow := map[string]any{"node_label": "SSMHostPlatform", "source_type": "ssm_inventory", "capture_time_unix": int64(1793313079)}
	diiRow := map[string]any{"node_label": "SSMHostPlatform", "source_type": "describe_instance_information", "observation_time_unix": int64(1793314200)}
	for _, row := range []map[string]any{appRow, platRow, diiRow} {
		for key, value := range hostColumns {
			row[key] = value
		}
	}
	reader := &stubCommittedReader{rows: []map[string]any{appRow, platRow, diiRow}}
	writer := NewHostNodeWriter(executor, reader, 0)
	got, err := writer.ReadCommittedHosts(context.Background(), "scope-1", []HostTuple{ssmTestHost()})
	if err != nil {
		t.Fatalf("ReadCommittedHosts: %v", err)
	}
	committed, ok := got[HostKey(ssmTestHost())]
	if !ok {
		t.Fatalf("committed map missing the host")
	}
	if !committed.HasAppCapture {
		t.Fatalf("HasAppCapture = false, want true")
	}
	if len(committed.PlatformCaptures) != 2 {
		t.Fatalf("platform captures = %d, want 2", len(committed.PlatformCaptures))
	}
}
