// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
      {"approach":"eshu_tools","answer_time_ms":3800,"found_answer":true,"missing_evidence_named":false,"token_budget":2400},
      {"approach":"evidence_packet","answer_time_ms":1200,"found_answer":true,"missing_evidence_named":true,"token_budget":700}]},
    {"name":"dr","family":"drift","approaches":[
      {"approach":"raw_files","answer_time_ms":60000,"found_answer":false,"missing_evidence_named":false,"token_budget":7000},
      {"approach":"eshu_tools","answer_time_ms":4100,"found_answer":true,"missing_evidence_named":false,"token_budget":2600},
      {"approach":"evidence_packet","answer_time_ms":1300,"found_answer":true,"missing_evidence_named":true,"token_budget":800}]},
    {"name":"svc","family":"service_context","approaches":[
      {"approach":"raw_files","answer_time_ms":55000,"found_answer":false,"missing_evidence_named":false,"token_budget":6500},
      {"approach":"eshu_tools","answer_time_ms":4500,"found_answer":true,"missing_evidence_named":false,"token_budget":2500},
      {"approach":"evidence_packet","answer_time_ms":1400,"found_answer":true,"missing_evidence_named":true,"token_budget":900}]}
  ]
}`

// runDogfoodCmd runs the command and returns its stdout, its stderr, and the
// error cobra returned, in that order. The two streams get their own buffer on
// purpose: with one shared buffer a caller can see what the command wrote but
// not which stream carried it, and every assertion below that names a stream
// would then be guessing.
func runDogfoodCmd(t *testing.T, args []string, stdin string) (string, string, error) {
	t.Helper()
	cmd := newEvidencePacketDogfoodCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestEvidencePacketDogfoodPasses(t *testing.T) {
	out, errOut, err := runDogfoodCmd(t, []string{"--json"}, passingDogfoodBenchmark)
	if err != nil {
		t.Fatalf("dogfood: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}
	// --json puts the verdict on stdout, so decoding stdout is also the
	// assertion that the JSON did not go to stderr instead.
	var verdict packetdogfood.Verdict
	if err := json.Unmarshal([]byte(out), &verdict); err != nil {
		t.Fatalf("decode verdict: %v\n%s", err, out)
	}
	if !verdict.Pass {
		t.Errorf("verdict did not pass: %+v", verdict.Criteria)
	}
	if errOut != "" {
		t.Errorf("stderr was not empty on a passing run\ngot:\n%q", errOut)
	}
}

// wantFailingDogfoodReport is every byte the command writes to stdout for the
// failing benchmark below. The test that uses it makes a second, separate
// assertion: stderr stays empty. Only the pair pins where the bytes went. An
// earlier version of runDogfoodCmd handed both streams one buffer, and that
// version stayed green with the whole report routed to stderr instead.
//
// The empty-stderr half is what holds SilenceErrors in place. Without it cobra
// echoes the returned error, and this benchmark always returns one.
//
// The constant exists to pin the seam between this wrapper and
// evidpacket.RenderVerdict: the renderer's string already ends in a newline, so
// the wrapper has to use fmt.Fprint. Switching it to fmt.Fprintln appends a
// second newline and lands the operator's shell prompt a blank line low, which
// no other assertion in this file notices.
//
// A criterion detail reworded in internal/packetdogfood changes this constant
// too. That is the point -- the text is what an operator reads.
const wantFailingDogfoodReport = "Evidence-packet dogfood FAILED\n" +
	"  run     : test (fixture)\n" +
	"  tasks   : 3\n" +
	"  families: drift, service_context, supply_chain_impact\n" +
	"--------------------------------------------\n" +
	"  [ok] family_coverage: covers supply-chain impact, deployable drift, and service context\n" +
	"  [ok] answer_correctness: packet found the correct answer on every task\n" +
	"  [!!] answer_time: task \"sc\": packet 999999ms slower than best baseline 3800ms\n" +
	"  [ok] token_efficiency: packet stayed within the best baseline token budget on every task\n" +
	"  [ok] missing_evidence_clarity: packet named missing evidence on every task, including gaps the baselines missed\n"

func TestEvidencePacketDogfoodFailsAndExitsNonZero(t *testing.T) {
	// Make the supply-chain packet slower than every baseline so it loses the
	// answer_time dimension: the verdict must render FAILED and exit non-zero.
	bad := strings.Replace(passingDogfoodBenchmark, `"answer_time_ms":1200`, `"answer_time_ms":999999`, 1)
	out, errOut, err := runDogfoodCmd(t, nil, bad)
	if err == nil {
		t.Fatalf("expected non-zero exit for a failing benchmark\n%s", out)
	}
	if out != wantFailingDogfoodReport {
		t.Errorf("stdout bytes differ\ngot:\n%q\nwant:\n%q", out, wantFailingDogfoodReport)
	}
	if errOut != "" {
		t.Errorf("stderr was not empty\ngot:\n%q", errOut)
	}
}

func TestEvidencePacketDogfoodRejectsBadJSON(t *testing.T) {
	if _, _, err := runDogfoodCmd(t, nil, "not json"); err == nil {
		t.Fatal("expected an error for malformed benchmark JSON")
	}
}
