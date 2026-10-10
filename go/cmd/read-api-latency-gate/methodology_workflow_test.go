// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"gopkg.in/yaml.v3"
)

type observedScaleTimeout struct {
	RunID                         int64  `json:"run_id"`
	JobID                         int64  `json:"job_id"`
	HeadSHA                       string `json:"head_sha"`
	SourceURL                     string `json:"source_url"`
	JobStartedAt                  string `json:"job_started_at"`
	SerialExercisedAt             string `json:"serial_exercised_at"`
	StepCanceledAt                string `json:"step_canceled_at"`
	TimeoutAnnotation             string `json:"timeout_annotation"`
	Backend                       string `json:"backend"`
	Runs                          int    `json:"runs"`
	Workers                       int    `json:"workers"`
	RequestsPerOperation          int    `json:"requests_per_operation"`
	RequestTimeoutSeconds         int    `json:"request_timeout_seconds"`
	EstimatedCleanupSeconds       int    `json:"estimated_cleanup_seconds"`
	RequiredReplayHeadroomSeconds int    `json:"required_replay_headroom_seconds"`
}

type scaleWorkflowJob struct {
	Name           string `yaml:"name"`
	If             string `yaml:"if"`
	TimeoutMinutes int    `yaml:"timeout-minutes"`
	Env            struct {
		Backend  string `yaml:"ESHU_GRAPH_BACKEND"`
		Runs     int    `yaml:"GATE_RUNS"`
		Workers  int    `yaml:"GATE_CONCURRENT_WORKERS"`
		Requests int    `yaml:"GATE_CONCURRENT_REQUESTS"`
	} `yaml:"env"`
	Steps []struct {
		Name string `yaml:"name"`
		Run  string `yaml:"run"`
	} `yaml:"steps"`
}

func requestTimeoutDefault(t *testing.T) time.Duration {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var timeout time.Duration
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fun.Sel.Name != "Duration" {
			return true
		}
		pkg, ok := fun.X.(*ast.Ident)
		if !ok || pkg.Name != "flag" {
			return true
		}
		name, ok := call.Args[0].(*ast.BasicLit)
		if !ok || name.Value != `"request-timeout"` {
			return true
		}
		product, ok := call.Args[1].(*ast.BinaryExpr)
		if !ok || product.Op != token.MUL {
			t.Fatal("request-timeout default is not seconds multiplication")
		}
		seconds, ok := product.X.(*ast.BasicLit)
		if !ok {
			t.Fatal("request-timeout default is not numeric")
		}
		unit, ok := product.Y.(*ast.SelectorExpr)
		if !ok || unit.Sel.Name != "Second" {
			t.Fatal("request-timeout default is not in seconds")
		}
		unitPkg, ok := unit.X.(*ast.Ident)
		if !ok || unitPkg.Name != "time" {
			t.Fatal("request-timeout default does not use time.Second")
		}
		value, err := strconv.Atoi(seconds.Value)
		if err != nil {
			t.Fatal(err)
		}
		timeout = time.Duration(value) * time.Second
		return false
	})
	if timeout <= 0 {
		t.Fatal("request-timeout flag default not found")
	}
	return timeout
}

func TestMethodologyScaleDeadlineContainsObservedWork(t *testing.T) {
	// The timestamps come from the public hosted job log. Cleanup and extra
	// headroom are replay allowances, not durations observed in that run.
	fixtureBytes, err := os.ReadFile(filepath.Join("testdata", "methodology-scale-timeout-38032713508.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observed observedScaleTimeout
	if err := json.Unmarshal(fixtureBytes, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.SourceURL != fmt.Sprintf("https://github.com/eshu-hq/eshu/actions/runs/%d/job/%d", observed.RunID, observed.JobID) || len(observed.HeadSHA) != 40 {
		t.Fatal("observed hosted run has no stable public identity")
	}
	parseTime := func(value string) time.Time {
		t.Helper()
		instant, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatal(err)
		}
		return instant
	}
	started := parseTime(observed.JobStartedAt)
	serial := parseTime(observed.SerialExercisedAt)
	canceled := parseTime(observed.StepCanceledAt)
	if !started.Before(serial) || !serial.Before(canceled) {
		t.Fatal("hosted phase timestamps are out of order")
	}
	var priorDeadlineMinutes int
	if _, err := fmt.Sscanf(observed.TimeoutAnnotation, "The job has exceeded the maximum execution time of %dm0s", &priorDeadlineMinutes); err != nil || priorDeadlineMinutes <= 0 {
		t.Fatalf("unrecognized hosted timeout annotation: %q: %v", observed.TimeoutAnnotation, err)
	}
	if serial.Sub(started) <= time.Duration(priorDeadlineMinutes)*time.Minute {
		t.Fatal("hosted serial phase did not exceed the annotated old deadline")
	}

	root := filepath.Join("..", "..", "..")
	workflowBytes, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "read-api-latency-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]scaleWorkflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatal(err)
	}
	manual, ok := workflow.Jobs["methodology-scale"]
	if !ok || manual.Name != "methodology scale (neo4j)" || manual.If != "github.event_name == 'workflow_dispatch'" {
		t.Fatalf("manual Neo4j job selection changed: %+v", manual)
	}
	nornic, ok := workflow.Jobs["latency-gate"]
	if !ok || nornic.Env.Backend != "nornicdb" || nornic.TimeoutMinutes != priorDeadlineMinutes {
		t.Fatalf("NornicDB blocking job policy changed: %+v", nornic)
	}
	if manual.Env.Backend != observed.Backend || manual.Env.Runs != observed.Runs || manual.Env.Workers != observed.Workers || manual.Env.Requests != observed.RequestsPerOperation {
		t.Fatalf("manual workflow run shape differs from observed trace: %+v", manual.Env)
	}
	if manual.Env.Workers <= 0 || manual.Env.Requests < manual.Env.Workers {
		t.Fatal("manual concurrency shape is invalid")
	}
	runsGate := false
	for _, step := range manual.Steps {
		if step.Name == "Run scale and concurrent HTTP/MCP proof" && strings.Contains(step.Run, "bash scripts/verify-read-api-latency-gate.sh") {
			runsGate = true
		}
	}
	if !runsGate {
		t.Fatal("manual job no longer invokes the latency gate")
	}
	script, err := os.ReadFile(filepath.Join(root, "scripts", "verify-read-api-latency-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{`-runs "${GATE_RUNS}"`, `-concurrent-workers "${GATE_CONCURRENT_WORKERS}"`, `-concurrent-requests "${GATE_CONCURRENT_REQUESTS}"`} {
		if !strings.Contains(string(script), flag) {
			t.Fatalf("runner no longer passes %s", flag)
		}
	}
	if strings.Contains(string(script), "-request-timeout") {
		t.Fatal("runner overrides the measured CLI request timeout")
	}
	requestTimeout := requestTimeoutDefault(t)
	if requestTimeout != time.Duration(observed.RequestTimeoutSeconds)*time.Second {
		t.Fatalf("request timeout = %s; observed %ds", requestTimeout, observed.RequestTimeoutSeconds)
	}
	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatal(err)
	}
	operations, err := PilotOperations(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 3 {
		t.Fatalf("pilot operation count = %d, observed 3", len(operations))
	}
	requestBatches := (manual.Env.Requests + manual.Env.Workers - 1) / manual.Env.Workers
	remaining := time.Duration(len(operations)*requestBatches) * requestTimeout
	required := serial.Sub(started) + remaining + time.Duration(observed.EstimatedCleanupSeconds+observed.RequiredReplayHeadroomSeconds)*time.Second
	deadline := time.Duration(manual.TimeoutMinutes) * time.Minute
	if deadline < required {
		t.Fatalf("manual deadline %s cannot contain hosted serial %s + remaining requests %s + cleanup/headroom %ds (need %s)", deadline, serial.Sub(started), remaining, observed.EstimatedCleanupSeconds+observed.RequiredReplayHeadroomSeconds, required)
	}
}
