// Tests in this file mutate the slog.Default() global to capture log records
// from walkBlockAttributes (which has no logger plumbing — see the
// "Parser-side logging access" decision in
// .claude/plans/multi-element-first-wins-telemetry-185.md). Because slog.SetDefault
// is process-global, NONE of the tests in this file may call t.Parallel(); a
// concurrent neighbor would observe the test logger and either pollute its
// own output or race on handler state. The save/restore pattern below is the
// minimum required scaffolding.
package hcl

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordingHandler captures slog.Records so tests can assert on the structured
// log shape emitted by walkBlockAttributes.
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

// attrMapForRecord pulls all attrs of one record into a key→value map.
func attrMapForRecord(r slog.Record) map[string]slog.Value {
	out := map[string]slog.Value{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value
		return true
	})
	return out
}

// installRecordingDefaultLogger swaps slog.Default() for one wired to the
// recording handler. The returned restore func reinstalls the previous
// default; callers MUST defer it so other tests run with the original logger.
func installRecordingDefaultLogger(level slog.Level) (*recordingHandler, func()) {
	prev := slog.Default()
	handler := newRecordingHandler(level)
	slog.SetDefault(slog.New(handler))
	return handler, func() {
		slog.SetDefault(prev)
	}
}

// NOTE: this test does not call t.Parallel(); see file-level comment above.
func TestParserMultiElementBlockFirstWinsLogs(t *testing.T) {
	handler, restore := installRecordingDefaultLogger(slog.LevelDebug)
	defer restore()

	body := `resource "aws_security_group" "ex" {
  ingress {
    from_port = 80
  }
  ingress {
    from_port = 443
  }
  ingress {
    from_port = 22
  }
}
`
	filePath := writeHCLTestFile(t, "main.tf", body)
	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	resources := bucketForTest(t, got, "terraform_resources")
	sg := namedItemForTest(t, resources, "aws_security_group.ex")
	known, ok := sg["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %T, want map[string]any", sg["attributes"])
	}
	if got, want := known["ingress.from_port"], "80"; got != want {
		t.Fatalf("ingress.from_port = %v, want %q (first-wins must drop 443 and 22)", got, want)
	}

	// Three ingress blocks → block #1 survives; blocks #2 and #3 each hit
	// the seenBlockTypes guard, so the parser emits two debug records.
	records := handler.snapshot()
	if len(records) != 2 {
		for i, r := range records {
			t.Logf("record[%d]: msg=%q attrs=%+v", i, r.Message, attrMapForRecord(r))
		}
		t.Fatalf("captured %d log records, want 2 (one per duplicate ingress block)", len(records))
	}
	for i, r := range records {
		if r.Level != slog.LevelDebug {
			t.Errorf("record[%d] level = %v, want Debug", i, r.Level)
		}
		m := attrMapForRecord(r)
		if got, want := m[telemetry.LogKeyDriftMultiElementSource].String(), "parser_walk"; got != want {
			t.Errorf("record[%d] %s = %q, want %q",
				i, telemetry.LogKeyDriftMultiElementSource, got, want)
		}
		if got, want := m[telemetry.LogKeyDriftMultiElementPrefix].String(), "ingress"; got != want {
			t.Errorf("record[%d] %s = %q, want %q",
				i, telemetry.LogKeyDriftMultiElementPrefix, got, want)
		}
		// Per the plan's open-question 4 resolution, the parser side
		// intentionally omits the count field — duplicates arrive
		// one-at-a-time during recursion. multi_element.source
		// disambiguates from the state-flatten path which can emit count.
		if _, present := m[telemetry.LogKeyDriftMultiElementCount]; present {
			t.Errorf("record[%d] unexpectedly carries %s; parser-side emissions must omit it",
				i, telemetry.LogKeyDriftMultiElementCount)
		}
	}
}

// NOTE: this test does not call t.Parallel(); see file-level comment above.
func TestParserSingletonBlockDoesNotLog(t *testing.T) {
	handler, restore := installRecordingDefaultLogger(slog.LevelDebug)
	defer restore()

	body := `resource "aws_s3_bucket" "logs" {
  versioning {
    enabled = true
  }
}
`
	filePath := writeHCLTestFile(t, "main.tf", body)
	if _, err := Parse(filePath, false, shared.Options{}); err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if records := handler.snapshot(); len(records) != 0 {
		for i, r := range records {
			t.Logf("unexpected record[%d]: msg=%q attrs=%+v", i, r.Message, attrMapForRecord(r))
		}
		t.Fatalf("captured %d log records, want 0 (singleton blocks must not log)", len(records))
	}
}
