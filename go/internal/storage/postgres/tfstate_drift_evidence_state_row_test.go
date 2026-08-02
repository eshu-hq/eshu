// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"strings"
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

// TestFlattenStateAttributesTreatsARedactionMarkerAsAbsent covers the sibling
// drift surface #5859 does not otherwise touch.
//
// A redaction marker is a map, and flattenStateAttributes recurses into maps, so
// a redacted `ami` becomes three dotted leaves -- `ami.marker`, `ami.reason`,
// `ami.source` -- and the literal `ami` key vanishes. `aws_instance.ami` is in
// tfconfigstate.attributeAllowlist, the same attribute this branch's cloudruntime
// regression targets.
//
// Today that is ACCIDENTALLY safe: classifyAttributeDrift skips the attribute
// because the literal key is missing, and nothing reads the dotted leaves. But
// nothing proves it, and the leaves are one allowlist addition away from being
// compared as real declared values. Recognising the marker keeps the shape
// explicit rather than relying on a downstream lookup happening to miss.
func TestFlattenStateAttributesTreatsARedactionMarkerAsAbsent(t *testing.T) {
	t.Parallel()

	attrs := map[string]any{
		"ami": map[string]any{
			"marker": "redacted:hmac-sha256:0000000000000000000000000000000000000000000000000000000000000000",
			"reason": "unknown_provider_schema",
			"source": "resources.*.attributes.ami",
		},
		"instance_type": "t3.micro",
	}
	out := map[string]string{}
	flattenStateAttributes(context.Background(), nil, attrs, "", out)

	for key := range out {
		if strings.HasPrefix(key, "ami") {
			t.Fatalf("flattenStateAttributes emitted %q = %q for a redacted attribute; "+
				"a redaction marker must not be flattened into comparable leaves", key, out[key])
		}
	}
	if out["instance_type"] != "t3.micro" {
		t.Fatalf("instance_type = %q, want t3.micro; unredacted attributes must survive", out["instance_type"])
	}
}

// benchStateAttributes builds a Terraform-state-shaped attributes tree with a
// realistic mix of scalars, nested maps, and the singleton-array repeated-block
// shape the flattener is written around.
func benchStateAttributes() map[string]any {
	block := func(i int) any {
		return []any{map[string]any{
			"from_port": i,
			"to_port":   i + 1,
			"protocol":  "tcp",
			"cidr_blocks": []any{map[string]any{
				"cidr": "10.0.0.0/16",
				"desc": "internal",
			}},
		}}
	}
	attrs := map[string]any{
		"arn":           "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		"ami":           "ami-0123456789abcdef0",
		"instance_type": "t3.micro",
		"monitoring":    false,
		"tags": map[string]any{
			"Name": "demo", "Env": "prod", "Team": "platform", "Cost": "cc-1",
		},
	}
	for i := range 8 {
		attrs["ingress_"+strings.Repeat("x", i%3)+string(rune('a'+i))] = block(i)
	}
	return attrs
}

// BenchmarkFlattenStateAttributes measures the widest of the three paths this
// branch added a redaction check to. Unlike the value-drift decoders, which
// read at most two allowlisted keys per row, flattenStateAttributes calls
// redact.IsRedactedValue once per node VISITED -- every map, every array, and
// every scalar leaf of a whole state resource's attribute tree. It backs the
// No-Regression Evidence in go/internal/storage/postgres/README.md, which would
// otherwise be asserting the cost of the one path it did not measure.
func BenchmarkFlattenStateAttributes(b *testing.B) {
	attrs := benchStateAttributes()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		out := make(map[string]string, 64)
		flattenStateAttributes(ctx, nil, attrs, "", out)
		if len(out) == 0 {
			b.Fatalf("flattenStateAttributes() produced no leaves")
		}
	}
}
