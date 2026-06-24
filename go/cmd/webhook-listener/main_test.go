// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestNewWebhookApplicationStartsDedicatedMetricsServer(t *testing.T) {
	statusReader := &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, time.May, 20, 19, 30, 0, 0, time.UTC),
			Queue: statuspkg.QueueSnapshot{
				Outstanding: 4,
			},
		},
	}
	cfg := runtimecfg.Config{
		ServiceName: "webhook-listener",
		Command:     "webhook-listener",
		ListenAddr:  reserveTCPAddress(t),
		MetricsAddr: reserveTCPAddress(t),
	}
	app, err := newWebhookApplication(
		cfg,
		statusReader,
		http.NewServeMux(),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "custom_prometheus_metric 1\n")
		}),
	)
	if err != nil {
		t.Fatalf("newWebhookApplication() error = %v, want nil", err)
	}

	if err := app.Lifecycle.Start(context.Background()); err != nil {
		t.Fatalf("Lifecycle.Start() error = %v, want nil", err)
	}
	defer func() {
		_ = app.Lifecycle.Stop(context.Background())
	}()

	response, err := http.Get("http://" + cfg.MetricsAddr + "/metrics")
	if err != nil {
		t.Fatalf("GET metrics error = %v, want nil", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}
	got := string(body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET metrics status = %d, want %d body=%q", response.StatusCode, http.StatusOK, got)
	}
	for _, want := range []string{
		`eshu_runtime_queue_outstanding{service_name="webhook-listener"} 4`,
		`custom_prometheus_metric 1`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("GET metrics body = %q, want %q", got, want)
		}
	}
}

type fakeStatusReader struct {
	snapshot statuspkg.RawSnapshot
}

func (r *fakeStatusReader) ReadStatusSnapshot(context.Context, time.Time) (statuspkg.RawSnapshot, error) {
	return r.snapshot, nil
}

func (r *fakeStatusReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	_ statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	return r.ReadStatusSnapshot(ctx, asOf)
}

func reserveTCPAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v, want nil", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() error = %v, want nil", err)
	}
	return addr
}
