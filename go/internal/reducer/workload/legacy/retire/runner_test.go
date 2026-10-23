// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// stubGraph answers the detection reads from in-memory rows. It dispatches on
// statement text, which is stable because the reads are package constants.
type stubGraph struct {
	candidates []map[string]any
	edgeInst   []string
	propInst   []string
	twins      []map[string]any
	rels       []map[string]any
	reads      int
}

func (g *stubGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.reads++
	ids, _ := params["ids"].([]string)
	pick := func(have []string) []map[string]any {
		var out []map[string]any
		for _, id := range have {
			if slices.Contains(ids, id) {
				out = append(out, map[string]any{"id": id})
			}
		}
		return out
	}
	switch {
	case strings.Contains(cypher, "w.id > $after"):
		after, _ := params["after"].(string)
		limit, _ := params["limit"].(int)
		var out []map[string]any
		for _, row := range g.candidates {
			if row["id"].(string) > after && len(out) < limit {
				out = append(out, row)
			}
		}
		return out, nil
	case strings.Contains(cypher, "w.id IN $ids AND 'workload:'"):
		var out []map[string]any
		for _, row := range g.candidates {
			if slices.Contains(ids, row["id"].(string)) {
				out = append(out, row)
			}
		}
		return out, nil
	case strings.Contains(cypher, "[r:INSTANCE_OF]"):
		return pick(g.edgeInst), nil
	case strings.Contains(cypher, "i.workload_id IN"):
		return pick(g.propInst), nil
	case strings.Contains(cypher, "$twin_ids"):
		var out []map[string]any
		for _, row := range g.twins {
			if slices.Contains(params["twin_ids"].([]string), row["id"].(string)) {
				out = append(out, row)
			}
		}
		return out, nil
	case strings.Contains(cypher, "type(r) AS type"):
		var out []map[string]any
		for _, row := range g.rels {
			if slices.Contains(ids, row["id"].(string)) {
				out = append(out, row)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected statement %q", cypher)
}

func (g *stubGraph) addEligible(name string) {
	repo := "repository:" + name
	g.candidates = append(g.candidates, map[string]any{"id": "workload:" + name, "name": name, "repo_id": repo})
	g.twins = append(g.twins, map[string]any{"id": legacyworkload.TwinID(repo, name), "repo_id": repo})
	g.rels = append(g.rels, map[string]any{"id": "workload:" + name, "type": "DEFINES", "outgoing": false, "other_id": repo, "other_labels": []any{"Repository"}})
}

func (g *stubGraph) addNoTwin(name string) {
	g.candidates = append(g.candidates, map[string]any{"id": "workload:" + name, "name": name, "repo_id": "repository:" + name})
}

type recordingWriter struct {
	mu      sync.Mutex
	calls   [][]string
	cypher  []string
	deleted int64
	err     error
}

func (w *recordingWriter) ExecuteCypherCountingNodeDeletes(_ context.Context, cypher string, params map[string]any) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cypher = append(w.cypher, cypher)
	w.calls = append(w.calls, params["ids"].([]string))
	return w.deleted, w.err
}

type recordingAudit struct{ events []governanceaudit.Event }

func (a *recordingAudit) Append(_ context.Context, events []governanceaudit.Event) error {
	a.events = append(a.events, events...)
	return nil
}

func newRunner(g *stubGraph, w *recordingWriter) *Runner {
	return &Runner{Reader: g, Writer: w, ExecuteSupported: true, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
}

func TestDryRunNeverWritesAndReportsEligibleAndBlocked(t *testing.T) {
	g := &stubGraph{}
	g.addEligible("a")
	g.addNoTwin("b")
	w := &recordingWriter{}
	resp, err := newRunner(g, w).Run(context.Background(), Request{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 0 {
		t.Fatalf("dry run wrote: %v", w.calls)
	}
	if !resp.DryRun || resp.Eligible != 1 || resp.Blocked != 1 || resp.Deleted != 0 || len(resp.Candidates) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Candidates[1].BlockedReason != legacyworkload.BlockedNoTwin || resp.Candidates[0].BlockedReason != "" {
		t.Fatalf("candidates = %+v", resp.Candidates)
	}
}

func TestExecuteDeletesOnlyEligibleIDsThroughTheGateStatement(t *testing.T) {
	g := &stubGraph{}
	g.addEligible("a")
	g.addNoTwin("b")
	g.addEligible("c")
	w := &recordingWriter{deleted: 2}
	resp, err := newRunner(g, w).Run(context.Background(), Request{Reason: "post-cutover cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 1 || !slices.Equal(w.calls[0], []string{"workload:a", "workload:c"}) {
		t.Fatalf("delete calls = %v, want one call with the eligible ids only", w.calls)
	}
	if w.cypher[0] != legacyworkload.DeleteNeo4jCypher {
		t.Fatal("execute must run the gate statement that re-evaluates every clause")
	}
	if resp.Deleted != 2 || resp.Eligible != 2 || resp.Blocked != 1 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestExecuteRespectsLimitAndKeepsLookingPastBlockedHead(t *testing.T) {
	g := &stubGraph{}
	for i := 0; i < 30; i++ {
		g.addNoTwin(fmt.Sprintf("a%02d", i)) // blocked nodes sort first
	}
	for i := 0; i < 5; i++ {
		g.addEligible(fmt.Sprintf("z%02d", i))
	}
	w := &recordingWriter{deleted: 3}
	resp, err := newRunner(g, w).Run(context.Background(), Request{Reason: "r", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 1 || len(w.calls[0]) != 3 {
		t.Fatalf("delete calls = %v, want one batch of 3", w.calls)
	}
	if !resp.Truncated || resp.Eligible != 3 {
		t.Fatalf("resp = %+v, want truncated with 3 eligible", resp)
	}
}

func TestExecuteWithNothingEligibleSendsNoWrite(t *testing.T) {
	g := &stubGraph{}
	g.addNoTwin("a")
	w := &recordingWriter{}
	resp, err := newRunner(g, w).Run(context.Background(), Request{Reason: "r"})
	if err != nil || len(w.calls) != 0 || resp.Deleted != 0 {
		t.Fatalf("resp=%+v err=%v calls=%v", resp, err, w.calls)
	}
}

func TestCleanGraphDryRunReportsZeroCandidates(t *testing.T) {
	resp, err := newRunner(&stubGraph{}, &recordingWriter{}).Run(context.Background(), Request{DryRun: true})
	if err != nil || len(resp.Candidates) != 0 || resp.Eligible != 0 || resp.Blocked != 0 || resp.Truncated {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if resp.Candidates == nil {
		t.Fatal("candidates must encode as an empty list, not null")
	}
}

func TestRequestValidation(t *testing.T) {
	r := newRunner(&stubGraph{}, &recordingWriter{})
	cases := map[string]Request{
		"execute without reason":   {},
		"blank reason":             {Reason: "   "},
		"limit above max":          {DryRun: true, Limit: MaxLimit + 1},
		"negative limit":           {DryRun: true, Limit: -1},
		"too many ids":             {DryRun: true, IDs: make([]string, MaxLimit+1)},
		"blank id in list":         {DryRun: true, IDs: []string{"workload:a", " "}},
		"execute reason is blank2": {Reason: "\t"},
	}
	for name, req := range cases {
		if _, err := r.Run(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
	}
}

func TestExecuteRefusedOnUnsupportedBackendButDryRunWorks(t *testing.T) {
	g := &stubGraph{}
	g.addEligible("a")
	r := newRunner(g, &recordingWriter{})
	r.ExecuteSupported = false
	if _, err := r.Run(context.Background(), Request{Reason: "r"}); !errors.Is(err, ErrExecuteUnsupported) {
		t.Fatalf("err = %v, want ErrExecuteUnsupported", err)
	}
	if g.reads != 0 {
		t.Fatalf("a refused execute must not read the graph; reads = %d", g.reads)
	}
	if resp, err := r.Run(context.Background(), Request{DryRun: true}); err != nil || resp.Eligible != 1 {
		t.Fatalf("dry run on an unsupported backend: resp=%+v err=%v", resp, err)
	}
}

func TestWriteFailureSurfacesAndAuditsUnavailable(t *testing.T) {
	g := &stubGraph{}
	g.addEligible("a")
	audit := &recordingAudit{}
	r := newRunner(g, &recordingWriter{err: errors.New("graph exploded")})
	r.Audit = audit
	if _, err := r.Run(context.Background(), Request{Reason: "r"}); err == nil {
		t.Fatal("want the write failure")
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionUnavailable {
		t.Fatalf("audit = %+v", audit.events)
	}
}

func TestAuditRecordsExecuteAndRefusalsButNotDryRun(t *testing.T) {
	g := &stubGraph{}
	g.addEligible("a")
	audit := &recordingAudit{}
	r := newRunner(g, &recordingWriter{deleted: 1})
	r.Audit = audit

	if _, err := r.Run(context.Background(), Request{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if len(audit.events) != 0 {
		t.Fatalf("dry run audited: %+v", audit.events)
	}
	_, _ = r.Run(context.Background(), Request{})
	_, _ = r.Run(context.Background(), Request{Reason: "r"})
	if len(audit.events) != 2 {
		t.Fatalf("events = %+v, want a refusal and an execute", audit.events)
	}
	if audit.events[0].Decision != governanceaudit.DecisionDenied || audit.events[1].Decision != governanceaudit.DecisionAllowed {
		t.Fatalf("decisions = %v, %v", audit.events[0].Decision, audit.events[1].Decision)
	}
	for _, e := range audit.events {
		if _, err := governanceaudit.NormalizeEvent(e); err != nil {
			t.Errorf("event %+v is not a valid governance audit event: %v", e, err)
		}
	}
}

func TestExecuteEmitsOutcomeCountersLogsAndWarnsPerBlockedNode(t *testing.T) {
	reader := metric.NewManualReader()
	inst, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	g := &stubGraph{}
	g.addEligible("a")
	g.addNoTwin("b")
	g.candidates = append(g.candidates, map[string]any{"id": "workload:c", "name": "c", "repo_id": "repository:c"})
	g.twins = append(g.twins, map[string]any{"id": legacyworkload.TwinID("repository:c", "c"), "repo_id": "repository:c"})
	g.rels = append(g.rels, map[string]any{"id": "workload:c", "type": "DOCUMENTS", "outgoing": false, "other_id": "repository:doc", "other_labels": []any{"Repository"}})
	g.edgeInst = []string{"workload:d"}
	g.candidates = append(g.candidates, map[string]any{"id": "workload:d", "name": "d", "repo_id": "repository:d"})

	var logs bytes.Buffer
	r := newRunner(g, &recordingWriter{deleted: 1})
	r.Instruments = inst
	r.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	if _, err := r.Run(context.Background(), Request{Reason: "post-cutover cleanup"}); err != nil {
		t.Fatal(err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_workload_legacy_retirements_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value("outcome")
				got[outcome.AsString()] += dp.Value
			}
		}
	}
	want := map[string]int64{OutcomeDeleted: 1, OutcomeBlockedNoTwin: 1, OutcomeBlockedRelationships: 1, OutcomeBlockedHasInstances: 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("counter outcome %s = %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
	out := logs.String()
	for _, needle := range []string{"legacy workload retirement executed", "reason=\"post-cutover cleanup\"", "workload_id=workload:c", "blocked_reason=relationships", "DOCUMENTS:1"} {
		if !strings.Contains(out, needle) {
			t.Errorf("log output missing %q:\n%s", needle, out)
		}
	}
}

func TestDryRunRecordsNoCounter(t *testing.T) {
	reader := metric.NewManualReader()
	inst, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	g := &stubGraph{}
	g.addEligible("a")
	r := newRunner(g, &recordingWriter{})
	r.Instruments = inst
	if _, err := r.Run(context.Background(), Request{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "eshu_dp_workload_legacy_retirements_total" {
				t.Fatalf("dry run recorded the retirement counter: %+v", m)
			}
		}
	}
}

func TestConcurrentRequestsSerializeWithoutOverlappingWrites(t *testing.T) {
	g := &stubGraph{}
	g.addEligible("a")
	w := &recordingWriter{deleted: 1}
	r := newRunner(g, w)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Run(context.Background(), Request{Reason: "r"})
		}()
	}
	wg.Wait()
	if len(w.calls) != 8 {
		t.Fatalf("write calls = %d, want 8 (each request runs; the statement is idempotent)", len(w.calls))
	}
}
