// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packetdogfood

import (
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T) Benchmark {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fixture_benchmark.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	benchmark, err := ParseBenchmark(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return benchmark
}

func TestScoreFixturePasses(t *testing.T) {
	verdict := Score(loadFixture(t))
	if !verdict.Pass {
		t.Fatalf("fixture benchmark did not pass: %+v", verdict.Criteria)
	}
	if verdict.TaskCount != 4 {
		t.Errorf("task count = %d, want 4", verdict.TaskCount)
	}
	for _, family := range requiredFamilies {
		if !containsFamily(verdict.Families, family) {
			t.Errorf("families %v missing required %q", verdict.Families, family)
		}
	}
}

func TestParseBenchmarkRejectsBadSchema(t *testing.T) {
	if _, err := ParseBenchmark([]byte(`{"schema":"nope","tasks":[]}`)); err == nil {
		t.Fatal("expected error for an unknown schema")
	}
}

func TestParseBenchmarkRejectsTaskWithoutPacket(t *testing.T) {
	raw := `{"schema":"evidence_packet_dogfood.v1","run_kind":"fixture","tasks":[
	  {"name":"t","family":"drift","approaches":[{"approach":"raw_files","answer_time_ms":1,"found_answer":true,"token_budget":1}]}]}`
	if _, err := ParseBenchmark([]byte(raw)); err == nil {
		t.Fatal("expected error for a task with no evidence_packet approach")
	}
}

func TestParseBenchmarkRejectsNonPositiveBaseline(t *testing.T) {
	// A placeholder baseline with a zero cost would make the gate meaningless.
	raw := `{"schema":"evidence_packet_dogfood.v1","run_kind":"fixture","tasks":[
	  {"name":"t","family":"drift","approaches":[
	    {"approach":"raw_files","answer_time_ms":0,"found_answer":false,"token_budget":5000},
	    {"approach":"eshu_tools","answer_time_ms":3000,"found_answer":true,"token_budget":2000},
	    {"approach":"evidence_packet","answer_time_ms":1,"found_answer":true,"missing_evidence_named":true,"token_budget":1}]}]}`
	if _, err := ParseBenchmark([]byte(raw)); err == nil {
		t.Fatal("expected error for a baseline with a non-positive answer_time_ms")
	}
}

func TestParseBenchmarkRequiresBothBaselines(t *testing.T) {
	// A task that omits the eshu_tools baseline must be rejected: the gate cannot
	// claim the packet beat Eshu tools without measuring that approach.
	raw := `{"schema":"evidence_packet_dogfood.v1","run_kind":"fixture","tasks":[
	  {"name":"t","family":"drift","approaches":[
	    {"approach":"raw_files","answer_time_ms":60000,"found_answer":false,"token_budget":7000},
	    {"approach":"evidence_packet","answer_time_ms":1300,"found_answer":true,"missing_evidence_named":true,"token_budget":800}]}]}`
	if _, err := ParseBenchmark([]byte(raw)); err == nil {
		t.Fatal("expected error for a task missing the eshu_tools baseline")
	}
}

func TestParseBenchmarkRejectsUnknownApproach(t *testing.T) {
	raw := `{"schema":"evidence_packet_dogfood.v1","run_kind":"fixture","tasks":[
	  {"name":"t","family":"drift","approaches":[
	    {"approach":"raw_files","answer_time_ms":60000,"found_answer":false,"token_budget":7000},
	    {"approach":"eshu_tools","answer_time_ms":4000,"found_answer":true,"token_budget":2500},
	    {"approach":"web_search","answer_time_ms":5000,"found_answer":true,"token_budget":3000},
	    {"approach":"evidence_packet","answer_time_ms":1300,"found_answer":true,"missing_evidence_named":true,"token_budget":800}]}]}`
	if _, err := ParseBenchmark([]byte(raw)); err == nil {
		t.Fatal("expected error for a task with an unsupported approach")
	}
}

func TestScoreFailsOnMissingFamily(t *testing.T) {
	benchmark := loadFixture(t)
	// Drop the service_context task to break family coverage.
	kept := benchmark.Tasks[:0]
	for _, task := range benchmark.Tasks {
		if task.Family != "service_context" {
			kept = append(kept, task)
		}
	}
	benchmark.Tasks = kept
	verdict := Score(benchmark)
	if verdict.Pass {
		t.Fatal("expected fail when service_context family is missing")
	}
	if criterionStatus(verdict, "family_coverage") != CriterionFail {
		t.Error("family_coverage should fail")
	}
}

func TestScoreFailsWhenPacketSlower(t *testing.T) {
	benchmark := loadFixture(t)
	setPacket(&benchmark.Tasks[0], func(r *ApproachResult) { r.AnswerTimeMS = 999999 })
	verdict := Score(benchmark)
	if verdict.Pass || criterionStatus(verdict, "answer_time") != CriterionFail {
		t.Fatal("expected answer_time fail when the packet is slower than the baseline")
	}
}

func TestScoreFailsWhenPacketOverTokenBudget(t *testing.T) {
	benchmark := loadFixture(t)
	setPacket(&benchmark.Tasks[0], func(r *ApproachResult) { r.TokenBudget = 999999 })
	verdict := Score(benchmark)
	if verdict.Pass || criterionStatus(verdict, "token_efficiency") != CriterionFail {
		t.Fatal("expected token_efficiency fail when the packet exceeds the baseline budget")
	}
}

func TestScoreFailsWhenPacketHidesGaps(t *testing.T) {
	benchmark := loadFixture(t)
	for i := range benchmark.Tasks {
		setPacket(&benchmark.Tasks[i], func(r *ApproachResult) { r.MissingEvidenceNamed = false })
	}
	verdict := Score(benchmark)
	if verdict.Pass || criterionStatus(verdict, "missing_evidence_clarity") != CriterionFail {
		t.Fatal("expected missing_evidence_clarity fail when the packet never names gaps")
	}
}

func TestScoreFailsWithoutDifferentiator(t *testing.T) {
	benchmark := loadFixture(t)
	// Make every baseline also name missing evidence: the packet no longer
	// differentiates on trustworthiness.
	for i := range benchmark.Tasks {
		for j := range benchmark.Tasks[i].Approaches {
			benchmark.Tasks[i].Approaches[j].MissingEvidenceNamed = true
		}
	}
	verdict := Score(benchmark)
	if verdict.Pass || criterionStatus(verdict, "missing_evidence_clarity") != CriterionFail {
		t.Fatal("expected missing_evidence_clarity fail with no differentiating task")
	}
}

func TestScoreFailsWhenPacketWrong(t *testing.T) {
	benchmark := loadFixture(t)
	setPacket(&benchmark.Tasks[0], func(r *ApproachResult) { r.FoundAnswer = false })
	verdict := Score(benchmark)
	if verdict.Pass || criterionStatus(verdict, "answer_correctness") != CriterionFail {
		t.Fatal("expected answer_correctness fail when the packet misses the answer")
	}
}

func setPacket(task *Task, mutate func(*ApproachResult)) {
	for i := range task.Approaches {
		if task.Approaches[i].Approach == ApproachEvidencePacket {
			mutate(&task.Approaches[i])
		}
	}
}

func criterionStatus(verdict Verdict, name string) CriterionStatus {
	for _, criterion := range verdict.Criteria {
		if criterion.Name == name {
			return criterion.Status
		}
	}
	return CriterionSkip
}

func containsFamily(families []string, want string) bool {
	for _, family := range families {
		if family == want {
			return true
		}
	}
	return false
}
