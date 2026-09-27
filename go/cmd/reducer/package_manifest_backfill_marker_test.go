// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPackageManifestBackfillCursorSampleUsesStorageMarker(t *testing.T) {
	queries := make(chan []driver.NamedValue, 1)
	sql.Register("package_manifest_backfill_marker_probe", packageManifestMarkerProbeDriver{queries: queries})
	database, err := sql.Open("package_manifest_backfill_marker_probe", "")
	if err != nil {
		t.Fatalf("open marker probe database: %v", err)
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runPackageManifestConsumptionKeyBackfill(ctx, database, nil, nil)
	}()

	select {
	case args := <-queries:
		if len(args) != 1 || args[0].Value != postgres.PackageManifestConsumptionKeyBackfillMarker {
			t.Errorf("cursor sample arguments = %v, want storage marker %q", args, postgres.PackageManifestConsumptionKeyBackfillMarker)
		}
	case err := <-done:
		t.Fatalf("backfill exited before cursor sample: %v", err)
	case <-ctx.Done():
		t.Fatal("backfill did not sample cursor")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("backfill exit = %v, want context canceled", err)
	}
}

type packageManifestMarkerProbeDriver struct {
	queries chan []driver.NamedValue
}

func (d packageManifestMarkerProbeDriver) Open(string) (driver.Conn, error) {
	return &packageManifestMarkerProbeConn{queries: d.queries}, nil
}

type packageManifestMarkerProbeConn struct {
	queries chan []driver.NamedValue
}

func (c *packageManifestMarkerProbeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *packageManifestMarkerProbeConn) Close() error { return nil }

func (c *packageManifestMarkerProbeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transaction not supported")
}

func (c *packageManifestMarkerProbeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(query, "pg_try_advisory_lock"), strings.Contains(query, "pg_advisory_unlock"):
		return &packageManifestMarkerProbeRows{value: true}, nil
	case strings.Contains(query, "FROM package_manifest_consumption_key_backfill_markers"):
		return &packageManifestMarkerProbeRows{value: true}, nil
	case strings.Contains(query, "SELECT count(*) FROM ("):
		return &packageManifestMarkerProbeRows{value: int64(0)}, nil
	case strings.Contains(query, "EXTRACT(EPOCH FROM updated_at)"):
		c.queries <- append([]driver.NamedValue(nil), args...)
		return &packageManifestMarkerProbeRows{value: int64(123)}, nil
	default:
		return nil, fmt.Errorf("unexpected marker probe query: %s", query)
	}
}

var _ driver.QueryerContext = (*packageManifestMarkerProbeConn)(nil)

type packageManifestMarkerProbeRows struct {
	value driver.Value
	done  bool
}

func (r *packageManifestMarkerProbeRows) Columns() []string { return []string{"value"} }
func (r *packageManifestMarkerProbeRows) Close() error      { return nil }
func (r *packageManifestMarkerProbeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	dest[0] = r.value
	r.done = true
	return nil
}
