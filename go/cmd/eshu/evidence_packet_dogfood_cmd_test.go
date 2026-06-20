package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
)

const passingDogfoodBenchmark = `{
  "schema": "evidence_packet_dogfood.v1",
  "run_kind": "fixture",
  "run_id": "test",
  "tasks": [
    {"name":"sc","family":"supply_chain_impact","approaches":[
      {"approach":"raw_files","answer_time_ms":50000,"found_answer":false,"missing_evidence_named":false,"token_budget":8000},
      {"approach":"evidence_packet","answer_time_ms":1200,"found_answer":true,"missing_evidence_named":true,"token_budget":700}]},
    {"name":"dr","family":"drift","approaches":[
      {"approach":"raw_files","answer_time_ms":60000,"found_answer":false,"missing_evidence_named":false,"token_budget":7000},
      {"approach":"evidence_packet","answer_time_ms":1300,"found_answer":true,"missing_evidence_named":true,"token_budget":800}]},
    {"name":"svc","family":"service_context","approaches":[
      {"approach":"raw_files","answer_time_ms":55000,"found_answer":false,"missing_evidence_named":false,"token_budget":6500},
      {"approach":"evidence_packet","answer_time_ms":1400,"found_answer":true,"missing_evidence_named":true,"token_budget":900}]}
  ]
}`

func runDogfoodCmd(t *testing.T, args []string, stdin string) (string, error) {
	t.Helper()
	cmd := newEvidencePacketDogfoodCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestEvidencePacketDogfoodPasses(t *testing.T) {
	out, err := runDogfoodCmd(t, []string{"--json"}, passingDogfoodBenchmark)
	if err != nil {
		t.Fatalf("dogfood: %v\n%s", err, out)
	}
	var verdict packetdogfood.Verdict
	if err := json.Unmarshal([]byte(out), &verdict); err != nil {
		t.Fatalf("decode verdict: %v\n%s", err, out)
	}
	if !verdict.Pass {
		t.Errorf("verdict did not pass: %+v", verdict.Criteria)
	}
}

func TestEvidencePacketDogfoodFailsAndExitsNonZero(t *testing.T) {
	// Remove the service_context task to break family coverage.
	bad := strings.Replace(passingDogfoodBenchmark,
		`,
    {"name":"svc","family":"service_context","approaches":[
      {"approach":"raw_files","answer_time_ms":55000,"found_answer":false,"missing_evidence_named":false,"token_budget":6500},
      {"approach":"evidence_packet","answer_time_ms":1400,"found_answer":true,"missing_evidence_named":true,"token_budget":900}]}`, "", 1)
	out, err := runDogfoodCmd(t, nil, bad)
	if err == nil {
		t.Fatalf("expected non-zero exit for a failing benchmark\n%s", out)
	}
	if !strings.Contains(out, "FAILED") {
		t.Errorf("output should report FAILED:\n%s", out)
	}
}

func TestEvidencePacketDogfoodRejectsBadJSON(t *testing.T) {
	if _, err := runDogfoodCmd(t, nil, "not json"); err == nil {
		t.Fatal("expected an error for malformed benchmark JSON")
	}
}
