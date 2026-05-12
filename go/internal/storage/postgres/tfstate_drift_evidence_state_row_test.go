package postgres

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordingHandler captures slog.Records so tests can assert on the structured
// log shape without depending on a concrete formatter (JSON/text). The
// handler honors the configured Level so debug-level lines flow through.
type recordingHandler struct {
	mu      sync.Mutex
	level   slog.Level
	records []slog.Record
}

func newRecordingHandler(level slog.Level) *recordingHandler {
	return &recordingHandler{level: level}
}

func (h *recordingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *recordingHandler) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

// attrMap collects all attrs from one record into a map keyed on attr key.
// The tests use this to assert on field values without iterating manually.
func attrMap(r slog.Record) map[string]slog.Value {
	out := map[string]slog.Value{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value
		return true
	})
	return out
}

func TestFlattenStateAttributesFirstWinsLogsMultiElement(t *testing.T) {
	t.Parallel()

	handler := newRecordingHandler(slog.LevelDebug)
	logger := slog.New(handler)

	attrs := map[string]any{
		// Singleton repeated block — must NOT log; len(typed) == 1 path.
		"versioning": []any{
			map[string]any{"enabled": "true"},
		},
		// Multi-element repeated block at the root prefix — should log
		// once with count=2 and prefix=ingress.
		"ingress": []any{
			map[string]any{"from_port": float64(80), "to_port": float64(80)},
			map[string]any{"from_port": float64(443), "to_port": float64(443)},
		},
		// Multi-element nested under a parent map — should log once with
		// count=3 and the dotted prefix aws_x.rule.
		"aws_x": map[string]any{
			"rule": []any{
				map[string]any{"k": "a"},
				map[string]any{"k": "b"},
				map[string]any{"k": "c"},
			},
		},
	}

	out := map[string]string{}
	flattenStateAttributes(context.Background(), logger, attrs, "", out)

	if got, want := out["versioning.enabled"], "true"; got != want {
		t.Errorf("versioning.enabled = %q, want %q", got, want)
	}
	if got, want := out["ingress.from_port"], "80"; got != want {
		t.Errorf("ingress.from_port = %q, want %q (first-wins must drop 443)", got, want)
	}
	if _, present := out["ingress.from_port"]; present {
		if out["ingress.from_port"] == "443" {
			t.Errorf("ingress.from_port = 443, want 80 (first-wins violated)")
		}
	}
	if got, want := out["aws_x.rule.k"], "a"; got != want {
		t.Errorf("aws_x.rule.k = %q, want %q (first-wins must drop b and c)", got, want)
	}

	records := handler.snapshot()
	if len(records) != 2 {
		for i, r := range records {
			t.Logf("record[%d]: msg=%q attrs=%+v", i, r.Message, attrMap(r))
		}
		t.Fatalf("captured %d log records, want 2 (one per multi-element prefix)", len(records))
	}

	got := map[string]struct {
		count  int64
		source string
	}{}
	for _, r := range records {
		if r.Level != slog.LevelDebug {
			t.Errorf("record level = %v, want Debug", r.Level)
		}
		m := attrMap(r)
		prefix := m[telemetry.LogKeyDriftMultiElementPrefix].String()
		count := m[telemetry.LogKeyDriftMultiElementCount].Int64()
		source := m[telemetry.LogKeyDriftMultiElementSource].String()
		got[prefix] = struct {
			count  int64
			source string
		}{count: count, source: source}
	}
	wantRecords := map[string]struct {
		count  int64
		source string
	}{
		"ingress":    {count: 2, source: "state_flatten"},
		"aws_x.rule": {count: 3, source: "state_flatten"},
	}
	for prefix, want := range wantRecords {
		g, ok := got[prefix]
		if !ok {
			t.Errorf("missing log record for prefix %q (got %v)", prefix, got)
			continue
		}
		if g != want {
			t.Errorf("record for %q = %+v, want %+v", prefix, g, want)
		}
	}
}

func TestFlattenStateAttributesNilLoggerDoesNotPanic(t *testing.T) {
	t.Parallel()

	attrs := map[string]any{
		"ingress": []any{
			map[string]any{"from_port": float64(80)},
			map[string]any{"from_port": float64(443)},
		},
	}
	out := map[string]string{}
	// Must not panic with a nil logger and must still produce the
	// first-wins flat map.
	flattenStateAttributes(context.Background(), nil, attrs, "", out)
	if got, want := out["ingress.from_port"], "80"; got != want {
		t.Errorf("ingress.from_port = %q, want %q", got, want)
	}
}
