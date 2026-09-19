// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// TestContentWriterFlagsADeriveOnAnUnfencedSession pins the fence's loud
// signal on the write path: a derive whose connection lacks the derive-aware
// writer setting (a pooler dropped the SET) still succeeds, but counts
// outcome ok_unfenced_session and logs infra_inventory.derive.unfenced_session
// at ERROR, because its content writes were marked and readers stay on the
// graph until the reconcile repairs them.
func TestContentWriterFlagsADeriveOnAnUnfencedSession(t *testing.T) {
	t.Parallel()

	instruments, reader := newDeriveMeter(t)
	var logs bytes.Buffer
	database := withTransactions(&fakeExecQueryer{})
	database.unfenced = true
	writer := NewContentWriter(database).WithInstruments(instruments).
		WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))
	if _, err := writer.Write(context.Background(), content.Materialization{
		RepoID: "repo-1", Records: []content.Record{{Path: "a.tf", Body: "x"}},
	}); err != nil {
		t.Fatalf("Write() error = %v, want the derive to succeed on an unfenced session", err)
	}
	if got := deriveOutcomes(t, reader); got["ok_unfenced_session"] != 1 || got["ok"] != 0 {
		t.Fatalf("derive outcomes = %v, want ok_unfenced_session=1 and no ok", got)
	}
	out := logs.String()
	if !strings.Contains(out, `"event_name":"infra_inventory.derive.unfenced_session"`) ||
		!strings.Contains(out, `"level":"ERROR"`) {
		t.Fatalf("logs = %s, want an ERROR infra_inventory.derive.unfenced_session event", out)
	}
}
