// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func validResourceSample(now time.Time) map[string]any {
	role := func(uid string) map[string]any {
		return map[string]any{
			"pod_name": "postgres-reader", "namespace": "eshu", "pod_uid": uid,
			"node_name": "node-a", "phase": "Running", "timestamp": now.Add(-time.Second).Format(time.RFC3339Nano),
			"window": "60s", "metric_age_seconds": 1.0, "window_seconds": 60.0,
			"containers": map[string]any{"postgres": map[string]any{
				"cpu_cores": 1.0, "cpu_limit_cores": 4.0,
				"memory_bytes": int64(1024), "memory_limit_bytes": int64(4096),
			}},
		}
	}
	return map[string]any{
		"sample_at": now.Add(-time.Second).Format(time.RFC3339Nano), "sample_seq": 3,
		"breaches": []string{},
		"ceilings": map[string]any{
			"cpu_ratio": 0.8, "memory_ratio": 0.8,
			"metric_age_seconds": 5.0, "metric_window_seconds": 120.0,
		},
		"roles": map[string]any{"reader": role("reader-uid"), "writer": role("writer-uid")},
	}
}

func TestReaderResourceSampleRejectsMissingStaleAndBreachedData(t *testing.T) {
	now := time.Now().UTC()
	config := readerResourceConfig{readerUID: "reader-uid", writerUID: "writer-uid"}
	good := validResourceSample(now)
	if err := validateReaderResourceSample(good, config, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing sample", func(s map[string]any) { delete(s, "sample_at") }},
		{"stale sample", func(s map[string]any) { s["sample_at"] = now.Add(-3 * time.Second).Format(time.RFC3339Nano) }},
		{"future sample", func(s map[string]any) { s["sample_at"] = now.Add(time.Second).Format(time.RFC3339Nano) }},
		{"missing sequence", func(s map[string]any) { delete(s, "sample_seq") }},
		{"missing breaches", func(s map[string]any) { delete(s, "breaches") }},
		{"breach", func(s map[string]any) { s["breaches"] = []string{"reader memory"} }},
		{"wrong UID", func(s map[string]any) {
			s["roles"].(map[string]any)["reader"].(map[string]any)["pod_uid"] = "other"
		}},
		{"missing container", func(s map[string]any) {
			s["roles"].(map[string]any)["reader"].(map[string]any)["containers"] = map[string]any{}
		}},
		{"missing role metric", func(s map[string]any) {
			delete(s["roles"].(map[string]any)["writer"].(map[string]any), "metric_age_seconds")
		}},
		{"usage over ceiling", func(s map[string]any) {
			s["roles"].(map[string]any)["reader"].(map[string]any)["containers"].(map[string]any)["postgres"].(map[string]any)["cpu_cores"] = 4.0
		}},
		{"invalid ceiling", func(s map[string]any) { s["ceilings"].(map[string]any)["cpu_ratio"] = 0.0 }},
		{"unsafe ceiling", func(s map[string]any) { s["ceilings"].(map[string]any)["cpu_ratio"] = 1.5 }},
		{"fractional memory", func(s map[string]any) {
			s["roles"].(map[string]any)["reader"].(map[string]any)["containers"].(map[string]any)["postgres"].(map[string]any)["memory_bytes"] = 1024.5
		}},
		{"window mismatch", func(s map[string]any) {
			s["roles"].(map[string]any)["reader"].(map[string]any)["window"] = "30s"
		}},
		{"fractional sequence", func(s map[string]any) { s["sample_seq"] = 3.5 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sample := validResourceSample(now)
			tc.change(sample)
			if err := validateReaderResourceSample(sample, config, now); err == nil {
				t.Fatal("unsafe resource sample accepted")
			}
		})
	}
}

func TestReaderResourceFileFailClosed(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "watch.json")
	config := readerResourceConfig{file: path, readerUID: "reader-uid", writerUID: "writer-uid"}
	if err := checkReaderResourceFile(context.Background(), config); err == nil {
		t.Fatal("missing watcher file accepted")
	}
	if err := os.WriteFile(path, []byte(`{"sample_at":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkReaderResourceFile(context.Background(), config); err == nil {
		t.Fatal("truncated JSON accepted")
	}
	encoded, err := json.Marshal(validResourceSample(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkReaderResourceFile(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", 65537)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkReaderResourceFile(context.Background(), config); err == nil {
		t.Fatal("oversized watcher file accepted")
	}
}

func TestReaderResourceFIFOWithoutWriterReturnsPromptly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	config := readerResourceConfig{file: path, readerUID: "reader-uid", writerUID: "writer-uid"}
	result := make(chan error, 1)
	go func() {
		result <- checkReaderResourceFile(context.Background(), config)
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("FIFO accepted as a watcher sample")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("watcher FIFO blocked despite no writer")
	}
}

func TestReaderResourceRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "watch.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	config := readerResourceConfig{file: link, readerUID: "reader-uid", writerUID: "writer-uid"}
	if err := checkReaderResourceFile(context.Background(), config); err == nil {
		t.Fatal("symlink watcher accepted")
	}
}

func TestReaderResourceConfigRequiresAbsolutePathAndUIDs(t *testing.T) {
	t.Setenv("ESHU7033_RESOURCE_GATE_FILE", "/tmp/reader-watch.json")
	t.Setenv("ESHU7033_READER_POD_UID", "reader-uid")
	t.Setenv("ESHU7033_WRITER_POD_UID", "writer-uid")
	if _, err := loadReaderResourceConfig(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"ESHU7033_RESOURCE_GATE_FILE", "relative.json"},
		{"ESHU7033_READER_POD_UID", ""},
		{"ESHU7033_WRITER_POD_UID", ""},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := loadReaderResourceConfig(); err == nil {
				t.Fatal("unsafe resource configuration accepted")
			}
		})
	}
}
