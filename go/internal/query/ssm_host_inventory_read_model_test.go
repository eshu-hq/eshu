// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

var ssmReadNow = time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)

var ssmReadBounds = ssmhost.Bounds{
	MaxCaptureAge:             2 * time.Hour,
	MaxCrossTypeSkew:          10 * time.Minute,
	MaxPlatformObservationAge: 24 * time.Hour,
}

func ssmReadAppNode(uid, capture, name string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": "i-1", "resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application",
		"schema_version": "1.0.0", "capture_time": capture, "name": name, "version": "1.0",
		"scope_id": "scope-1", "generation_id": "gen-1",
	}
}

func ssmReadPlatformNode(uid, source, observed string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": "i-1", "resource_type": "AWS::EC2::Instance", "source_type": source,
		"platform_type": "Linux", "platform_name": "ExampleLinux",
		"capture_time": "2026-10-29T22:31:19Z", "observation_time": observed,
		"scope_id": "scope-1", "generation_id": "gen-1",
	}
}

func ssmReadAppFact(factID, capture, name string) ssmAdmittedFact {
	return ssmAdmittedFact{
		FactID: factID, FactKind: "aws_ssm_host_application", ScopeID: "scope-1", GenerationID: "gen-2",
		Payload: map[string]any{
			"partition": "aws", "account_id": "000000000001", "region": "us-east-1", "instance_id": "i-1",
			"resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application", "schema_version": "1.0.0",
			"capture_time": capture, "name": name, "version": "1.0",
		},
	}
}

func ssmReadPlatformFact(factID, source, capture, observed string) ssmAdmittedFact {
	payload := map[string]any{
		"partition": "aws", "account_id": "000000000001", "region": "us-east-1", "instance_id": "i-1",
		"resource_type": "AWS::EC2::Instance", "source_type": source,
		"platform_type": "Linux", "platform_name": "ExampleLinux", "observation_time": observed,
	}
	if capture != "" {
		payload["capture_time"] = capture
	}
	return ssmAdmittedFact{FactID: factID, FactKind: "aws_ssm_host_platform", ScopeID: "scope-1", GenerationID: "gen-2", Payload: payload}
}

type stubSSMGraph struct {
	rows []map[string]any
	err  error
}

func (s *stubSSMGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

func (s *stubSSMGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.rows) == 0 {
		return nil, nil
	}
	return s.rows[0], nil
}

type stubSSMFacts struct {
	facts []ssmAdmittedFact
	err   error
}

func (s *stubSSMFacts) SSMHostActiveFacts(context.Context, ssmHostInventoryFilter) ([]ssmAdmittedFact, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.facts, nil
}

func ssmCommittedRows(apps []map[string]any, plats []map[string]any, edges [][2]string) []map[string]any {
	var rows []map[string]any
	for _, app := range apps {
		rows = append(rows, map[string]any{"app": app, "plat": nil})
	}
	for _, edge := range edges {
		rows = append(rows, map[string]any{"edge_from": edge[0], "edge_to": edge[1]})
	}
	for _, plat := range plats {
		rows = append(rows, map[string]any{"plat_only": plat})
	}
	return rows
}

func TestReadSSMHostInventoryPositiveAgrees(t *testing.T) {
	graph := &stubSSMGraph{rows: ssmCommittedRows(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		[]map[string]any{ssmReadPlatformNode("uid-plat-1", "ssm_inventory", "2026-10-29T22:33:02Z")},
		[][2]string{{"uid-app-1", "uid-plat-1"}},
	)}
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:31:19Z", "alpha"),
		ssmReadPlatformFact("p1", "ssm_inventory", "2026-10-29T22:31:19Z", "2026-10-29T22:33:02Z"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(model.Hosts))
	}
	host := model.Hosts[0]
	if host.Status != "successful" {
		t.Fatalf("status = %q, want successful", host.Status)
	}
	if len(host.Applications) != 1 || host.Applications[0]["name"] != "alpha" {
		t.Fatalf("applications = %v, want the committed alpha row", host.Applications)
	}
	if len(host.Platforms) != 1 {
		t.Fatalf("platforms = %d, want 1", len(host.Platforms))
	}
	if host.CaptureTime != "2026-10-29T22:31:19Z" {
		t.Fatalf("capture_time = %q, want the committed capture", host.CaptureTime)
	}
	// Observations must never synthesize findings: the whole host view
	// marshals without any vulnerability, package-correlation, or
	// supply-chain key (#9209 owns correlation, never this surface).
	encoded, err := json.Marshal(host)
	if err != nil {
		t.Fatalf("marshal host view: %v", err)
	}
	for _, key := range []string{"vulnerability", "os_package", "supply_chain_impact", "purl", "image_digest"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("host view carries %q: %s", key, encoded)
		}
	}
}

func TestReadSSMHostInventoryPartialServesCommitted(t *testing.T) {
	graph := &stubSSMGraph{rows: ssmCommittedRows(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		nil, nil,
	)}
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:31:19Z", "alpha"),
		ssmReadAppFact("a2", "2026-10-29T22:35:44Z", "beta"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1 (prior truth preserved and served)", len(model.Hosts))
	}
	if model.Hosts[0].Status != "partial" {
		t.Fatalf("status = %q, want partial", model.Hosts[0].Status)
	}
	if len(model.Hosts[0].Applications) != 1 {
		t.Fatalf("applications = %d, want the 1 committed row (never the incoherent mix)", len(model.Hosts[0].Applications))
	}
}

func TestReadSSMHostInventorySilentIsUnknown(t *testing.T) {
	graph := &stubSSMGraph{rows: ssmCommittedRows(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		nil, nil,
	)}
	store := &stubSSMFacts{}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 || model.Hosts[0].Status != "unknown" {
		t.Fatalf("silent generation must preserve prior truth as unknown, got %+v", model.Hosts)
	}
}

func TestReadSSMHostInventoryStaleCommitted(t *testing.T) {
	graph := &stubSSMGraph{rows: ssmCommittedRows(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T20:00:00Z", "alpha")},
		nil, nil,
	)}
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T20:00:00Z", "alpha"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 || model.Hosts[0].Status != "stale" {
		t.Fatalf("aged committed capture must read stale, got %+v", model.Hosts)
	}
}

func TestReadSSMHostInventoryPendingProjection(t *testing.T) {
	graph := &stubSSMGraph{rows: ssmCommittedRows(
		[]map[string]any{ssmReadAppNode("uid-app-old", "2026-10-29T22:31:19Z", "alpha")},
		nil, nil,
	)}
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:45:00Z", "alpha"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 || model.Hosts[0].Status != "unknown" {
		t.Fatalf("newer admitted capture than committed must read unknown (projection pending), got %+v", model.Hosts)
	}
	if len(model.Hosts[0].Applications) != 1 {
		t.Fatalf("pending projection must still serve committed rows, got %d", len(model.Hosts[0].Applications))
	}
}

func TestReadSSMHostInventoryAbsentHostIsAbsent(t *testing.T) {
	model, err := ReadSSMHostInventory(context.Background(), &stubSSMGraph{}, &stubSSMFacts{}, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 0 {
		t.Fatalf("hosts = %d with no committed nodes and no facts, want 0 (never an empty host)", len(model.Hosts))
	}
}

func TestReadSSMHostInventoryBoundsApplications(t *testing.T) {
	var apps []map[string]any
	for i := 0; i < 250; i++ {
		apps = append(apps, ssmReadAppNode("uid-app", "2026-10-29T22:31:19Z", "alpha"))
	}
	graph := &stubSSMGraph{rows: ssmCommittedRows(apps, nil, nil)}
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:31:19Z", "alpha"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts[0].Applications) != ssmHostInventoryMaxApplications {
		t.Fatalf("applications = %d, want the %d cap", len(model.Hosts[0].Applications), ssmHostInventoryMaxApplications)
	}
	if !model.Hosts[0].ApplicationsTruncated {
		t.Fatalf("ApplicationsTruncated = false on a capped list")
	}
}

func TestBuildSSMHostActiveFactsSQL(t *testing.T) {
	filter := ssmHostInventoryFilter{InstanceID: "i-1", Limit: 10}
	sqlQuery, args := buildSSMHostActiveFactsSQL(filter)
	for _, want := range []string{
		"fact_records.fact_kind IN",
		"aws_ssm_host_application",
		"aws_ssm_host_platform",
		"active",
		"payload->>'instance_id'",
	} {
		if !strings.Contains(sqlQuery, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sqlQuery)
		}
	}
	if len(args) == 0 {
		t.Fatalf("SQL has no args")
	}
}
