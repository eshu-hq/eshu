// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// failingWriter fails on the Nth write so the tests can reach the branches
// where output does not make it to the terminal.
type failingWriter struct {
	failAfter int
	writes    int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes > w.failAfter {
		return 0, errors.New("broken pipe")
	}
	return len(p), nil
}

func impactEnvelope() Envelope {
	return Envelope{
		Data: map[string]any{
			"changed_file_count": float64(2),
			"truncated":          false,
			"code_surface":       map[string]any{"symbol_count": float64(3)},
			"impact_summary":     map[string]any{"direct_count": float64(1), "transitive_count": float64(2)},
			"missing_evidence":   []any{},
			"coverage":           map[string]any{"state": "supported"},
			"recommended_next_calls": []any{
				map[string]any{"tool": "get_entity_context", "reason": "expand the direct callers"},
				"not an object",
			},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}
}

func planEnvelope() Envelope {
	return Envelope{
		Data: map[string]any{
			"changed_file_count": float64(1),
			"blocked":            true,
			"truncated":          false,
			"actions": []any{
				map[string]any{"kind": "rename_safety_check", "risk": "high", "title": "Verify both path endpoints"},
				42,
			},
			"bounded_next_calls": []any{map[string]any{"kind": "api", "target": "POST " + ImpactRoute}},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}
}

func TestFinishImpactRenderings(t *testing.T) {
	t.Parallel()

	t.Run("clean summary", func(t *testing.T) {
		t.Parallel()
		out := &bytes.Buffer{}
		if err := FinishImpact(out, Options{}, impactEnvelope(), nil); err != nil {
			t.Fatalf("FinishImpact() error = %v", err)
		}
		want := "Truth freshness: fresh\n" +
			"Pre-change impact: 2 changed files (coverage=supported truncated=false)\n" +
			"  symbols=3 direct=1 transitive=2 missing_evidence=0\n" +
			"  next=get_entity_context reason=expand the direct callers\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
	})

	t.Run("json mode prints only the envelope and keeps the error", func(t *testing.T) {
		t.Parallel()
		out := &bytes.Buffer{}
		sentinelErr := errors.New("boom")
		if err := FinishImpact(out, Options{JSON: true}, impactEnvelope(), sentinelErr); !errors.Is(err, sentinelErr) {
			t.Fatalf("FinishImpact() error = %v, want the caller's error back", err)
		}
		if strings.Contains(out.String(), "Pre-change impact:") {
			t.Fatalf("JSON output carries the human summary: %q", out.String())
		}
		var decoded Envelope
		if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
			t.Fatalf("JSON output does not parse: %v (%q)", err, out.String())
		}
		if stringValue(mapValue(decoded.Truth, "freshness"), "state") != "fresh" {
			t.Fatalf("decoded truth = %+v", decoded.Truth)
		}
	})

	t.Run("failed call with an envelope error prints one line", func(t *testing.T) {
		t.Parallel()
		out := &bytes.Buffer{}
		envelope := Envelope{Error: &EnvelopeError{Code: "not_found", Message: "no such repo"}}
		cmdErr := EnvelopeFailure(envelope.Error)
		if err := FinishImpact(out, Options{}, envelope, cmdErr); !errors.Is(err, cmdErr) {
			t.Fatalf("FinishImpact() error = %v, want %v", err, cmdErr)
		}
		if got, want := out.String(), "Pre-change impact error (not_found): no such repo\n"; got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	})

	t.Run("failed call that still carried data prints the summary", func(t *testing.T) {
		t.Parallel()
		out := &bytes.Buffer{}
		envelope := impactEnvelope()
		envelope.Data["truncated"] = true
		cmdErr := ClassifyImpact(envelope)
		if err := FinishImpact(out, Options{}, envelope, cmdErr); !errors.Is(err, cmdErr) {
			t.Fatalf("FinishImpact() error = %v, want %v", err, cmdErr)
		}
		if !strings.Contains(out.String(), "truncated=true") {
			t.Fatalf("partial summary missing: %q", out.String())
		}
	})

	t.Run("failed call with neither error nor data prints nothing", func(t *testing.T) {
		t.Parallel()
		out := &bytes.Buffer{}
		cmdErr := errors.New("transport gave up")
		if err := FinishImpact(out, Options{}, Envelope{}, cmdErr); !errors.Is(err, cmdErr) {
			t.Fatalf("FinishImpact() error = %v, want %v", err, cmdErr)
		}
		if out.Len() != 0 {
			t.Fatalf("output = %q, want empty", out.String())
		}
	})

	t.Run("a write failure replaces the command error", func(t *testing.T) {
		t.Parallel()
		cmdErr := errors.New("transport gave up")
		if err := FinishImpact(&failingWriter{}, Options{JSON: true}, impactEnvelope(), cmdErr); err == nil || errors.Is(err, cmdErr) {
			t.Fatalf("FinishImpact() error = %v, want the write error", err)
		}
		// Second summary line: the freshness line writes first.
		if err := FinishImpact(&failingWriter{failAfter: 1}, Options{}, impactEnvelope(), nil); err == nil {
			t.Fatal("FinishImpact() error = nil, want the write error")
		}
		envelope := Envelope{Error: &EnvelopeError{Code: "not_found", Message: "no such repo"}}
		if err := FinishImpact(&failingWriter{}, Options{}, envelope, EnvelopeFailure(envelope.Error)); err == nil {
			t.Fatal("FinishImpact() error = nil, want the write error")
		}
	})
}

func TestFinishPlanRenderings(t *testing.T) {
	t.Parallel()

	t.Run("summary has no freshness line", func(t *testing.T) {
		t.Parallel()
		out := &bytes.Buffer{}
		envelope := planEnvelope()
		cmdErr := ClassifyPlan(envelope)
		if err := FinishPlan(out, Options{}, envelope, cmdErr); !errors.Is(err, cmdErr) {
			t.Fatalf("FinishPlan() error = %v, want %v", err, cmdErr)
		}
		want := "Developer change plan: 2 actions for 1 changed files (blocked=true truncated=false)\n" +
			"  action=rename_safety_check risk=high title=Verify both path endpoints\n" +
			"  next=api target=POST " + ImpactRoute + "\n"
		if out.String() != want {
			t.Fatalf("output = %q, want %q", out.String(), want)
		}
		if strings.Contains(out.String(), "Truth freshness") {
			t.Fatalf("plan summary gained a freshness line: %q", out.String())
		}
	})

	t.Run("a write failure replaces the command error", func(t *testing.T) {
		t.Parallel()
		if err := FinishPlan(&failingWriter{}, Options{}, planEnvelope(), nil); err == nil {
			t.Fatal("FinishPlan() error = nil, want the write error")
		}
		if err := FinishPlan(&failingWriter{failAfter: 1}, Options{}, planEnvelope(), nil); err == nil {
			t.Fatal("FinishPlan() error = nil, want the write error on the action line")
		}
		if err := FinishPlan(&failingWriter{failAfter: 2}, Options{}, planEnvelope(), nil); err == nil {
			t.Fatal("FinishPlan() error = nil, want the write error on the next-call line")
		}
	})
}

// TestRequestBodiesCarryEveryOption pins both request shapes, and that only the
// plan body carries developer_intent.
func TestRequestBodiesCarryEveryOption(t *testing.T) {
	t.Parallel()

	opts := Options{
		DeveloperIntent: "rename helper safely",
		RepoID:          "repo-1",
		BaseRef:         "main",
		HeadRef:         "feature/pre-change",
		ChangedPaths:    []string{"go/a.go"},
		Changes:         []FileChange{{Path: "go/a.go", OldPath: "go/b.go", Status: "renamed"}},
		Target:          "svc:api",
		TargetType:      "service",
		ServiceName:     "api",
		WorkloadID:      "workload-1",
		ResourceID:      "arn:aws:x",
		ModuleID:        "module-1",
		Topic:           "auth",
		Environment:     "prod",
		MaxDepth:        3,
		Limit:           25,
		Offset:          10,
	}

	impact := ImpactRequestBody(opts)
	if _, ok := impact["developer_intent"]; ok {
		t.Fatalf("impact body carries developer_intent: %v", impact)
	}
	for key, want := range map[string]any{
		"repo_id": "repo-1", "base_ref": "main", "head_ref": "feature/pre-change",
		"target": "svc:api", "target_type": "service", "service_name": "api",
		"workload_id": "workload-1", "resource_id": "arn:aws:x", "module_id": "module-1",
		"topic": "auth", "environment": "prod", "max_depth": 3, "limit": 25, "offset": 10,
	} {
		if got := impact[key]; got != want {
			t.Fatalf("impact body[%s] = %#v, want %#v", key, got, want)
		}
	}
	if got := len(impact); got != 16 {
		t.Fatalf("impact body has %d keys, want 16", got)
	}

	plan := PlanRequestBody(opts)
	if got, want := plan["developer_intent"], "rename helper safely"; got != want {
		t.Fatalf("plan body developer_intent = %#v, want %#v", got, want)
	}
	if got := len(plan); got != 17 {
		t.Fatalf("plan body has %d keys, want 17", got)
	}
	// PlanRequestBody builds on the impact body; nothing else may differ.
	delete(plan, "developer_intent")
	for key, want := range impact {
		if got, ok := plan[key]; !ok || !sameJSON(t, got, want) {
			t.Fatalf("plan body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

// sameJSON compares two request-body values through their JSON encoding, which
// is the only comparison that matters for a body about to be marshaled and is
// also the only one that works for the slice-valued keys.
func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal %#v: %v", a, err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal %#v: %v", b, err)
	}
	return bytes.Equal(left, right)
}
