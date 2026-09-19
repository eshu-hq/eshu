// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestVerifyWriterSessionLiveFlagsAnUnfencedConnection proves the startup
// check a content-writer binary runs after opening Postgres: a connection
// opened through OpenWriterDB reports fenced and logs nothing, and a plain
// connection (what a pooler that drops the SET hands back) reports unfenced
// and logs postgres.session_unfenced at ERROR with the pooler hint.
func TestVerifyWriterSessionLiveFlagsAnUnfencedConnection(t *testing.T) {
	awareDB, ctx := liveDB(t)
	legacyDB := plainDB(t)

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	if !inventory.VerifyWriterSession(ctx, postgres.SQLDB{DB: awareDB}, logger) {
		t.Fatal("VerifyWriterSession(writer db) = false, want true")
	}
	if logs.Len() != 0 {
		t.Fatalf("a fenced session logged: %s", logs.String())
	}
	if inventory.VerifyWriterSession(ctx, postgres.SQLDB{DB: legacyDB}, logger) {
		t.Fatal("VerifyWriterSession(plain db) = true, want false")
	}
	out := logs.String()
	if !strings.Contains(out, `"event_name":"postgres.session_unfenced"`) || !strings.Contains(out, `"level":"ERROR"`) ||
		!strings.Contains(out, "eshu.infra_inventory_writer") {
		t.Fatalf("logs = %s, want an ERROR postgres.session_unfenced event naming the setting", out)
	}
}
