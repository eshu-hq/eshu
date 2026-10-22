// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/mcp/router/eval"
	"github.com/eshu-hq/eshu/go/internal/mcp/router/rank"
)

// reportPathEnv names a file the report test also writes, so a CI step can
// append the numbers to the job summary.
const reportPathEnv = "ESHU_ROUTER_EVAL_REPORT"

func registryNames() []string {
	var names []string
	for _, d := range mcp.ReadOnlyTools() {
		names = append(names, d.Name)
	}
	return names
}

// catalogEntries builds the annotated tool entries the same way the server
// wiring does: parse the committed surface inventory, then annotate it.
func catalogEntries(t *testing.T) map[string]catalog.Entry {
	t.Helper()
	cat, err := catalog.Parse(capabilitycatalog.RawSurfaceArtifact())
	if err != nil {
		t.Fatalf("catalog.Parse: %v", err)
	}
	cat.Annotate()
	entries := map[string]catalog.Entry{}
	for _, e := range cat.Entries() {
		if e.Kind == catalog.KindMCPTool {
			entries[e.Name] = e
		}
	}
	return entries
}

// TestCorpusMatchesRegistry is the blocking part of the gate: the corpus must
// load, meet its floors, and name only registered tools. Accuracy is not
// checked here.
func TestCorpusMatchesRegistry(t *testing.T) {
	c, err := eval.Default()
	if err != nil {
		t.Fatalf("eval.Default: %v", err)
	}
	if err := eval.Validate(c, registryNames(), eval.DefaultRequirements()); err != nil {
		t.Fatalf("corpus is invalid:\n%v", err)
	}
}

// TestUnregisteredExpectedToolFailsTheGate is the seeded RED run: the same
// corpus with one expected tool renamed to an unregistered name must fail.
func TestUnregisteredExpectedToolFailsTheGate(t *testing.T) {
	c, err := eval.Default()
	if err != nil {
		t.Fatalf("eval.Default: %v", err)
	}
	c.Questions[0].Expected = "tool_that_is_not_registered"
	err = eval.Validate(c, registryNames(), eval.DefaultRequirements())
	if err == nil || !strings.Contains(err.Error(), "tool_that_is_not_registered") {
		t.Fatalf("Validate = %v, want an error naming the unregistered tool", err)
	}
}

// TestRoutingAccuracyReport runs the lexical ranker over the corpus and prints
// the numbers. It is report-only by design: it fails when the harness cannot
// run, never on accuracy. Thresholds come after five recorded runs on main.
func TestRoutingAccuracyReport(t *testing.T) {
	c, err := eval.Default()
	if err != nil {
		t.Fatalf("eval.Default: %v", err)
	}
	tools := mcp.ReadOnlyTools()
	entries := catalogEntries(t)
	rep, err := eval.Run(context.Background(), rank.New(tools, entries), c, eval.Env{
		Tools: tools, Entries: entries, Allowed: eval.Routable,
	})
	if err != nil {
		t.Fatalf("eval.Run: %v", err)
	}
	if rep.Total != len(c.Questions) {
		t.Fatalf("report covers %d questions, corpus has %d", rep.Total, len(c.Questions))
	}
	var out strings.Builder
	if err := eval.Format(&out, rep); err != nil {
		t.Fatalf("eval.Format: %v", err)
	}
	t.Log("\n" + out.String())
	if path := os.Getenv(reportPathEnv); path != "" {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}
