// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticClientReportRequiresEveryConcurrentIndex(t *testing.T) {
	for _, count := range []int{19, 20} {
		c := newDiagnosticClient(20)
		for index := range count {
			c.record("concurrent", index, strconv.Itoa(index+1), time.Unix(1, 0), time.Millisecond, 200)
		}
		path := filepath.Join(t.TempDir(), "client.json")
		err := writeDiagnosticClientReport(path, c, 20)
		if (err == nil) != (count == 20) {
			t.Fatalf("count=%d err=%v", count, err)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var report struct {
			Complete bool `json:"complete"`
		}
		if err := json.Unmarshal(data, &report); err != nil || report.Complete != (count == 20) {
			t.Fatalf("invalid completion artifact: %s", data)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private artifact permissions: info=%v err=%v", info, err)
		}
	}
}

func TestDiagnosticClientCapturesSelectedHTTPResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-only" {
			t.Error("request authentication changed")
		}
		w.Header().Set(diagnosticResponseHeader, "7")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c := newDiagnosticClient(1)
	op := Operation{Method: http.MethodGet, Path: "/api/v0/status/ingesters/repository"}
	duration, status, _, err := sweepOperationObserved(server.Client(), server.URL+op.Path, "fixture-only", op,
		&diagnosticRequest{client: c, phase: "concurrent", index: 3})
	if err != nil || status != 200 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	records, err := c.snapshot()
	if err != nil || len(records) != 1 || records[0].Index != 3 || records[0].ServerID != "7" || records[0].ElapsedMS != float64(duration)/float64(time.Millisecond) {
		t.Fatalf("correlation/measurement changed: records=%+v err=%v", records, err)
	}
}

func TestDiagnosticClientBindsDispatchIndexes(t *testing.T) {
	c := newDiagnosticClient(20)
	var workers sync.WaitGroup
	for index := range 20 {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			c.record("concurrent", index, strconv.Itoa(index+1), time.Unix(1, 0), time.Millisecond, 200)
		}(index)
	}
	workers.Wait()
	records, err := c.snapshot()
	if err != nil || len(records) != 20 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	for index, record := range records {
		if record.Index != index || record.ServerID != strconv.Itoa(index+1) || record.Status != 200 {
			t.Fatalf("dispatch %d lost correlation: %+v", index, record)
		}
	}
}

func TestDiagnosticClientRejectsIncompleteEvidence(t *testing.T) {
	for _, id := range []string{"", "secret-token", "0", "0001", "123456789012345678901"} {
		t.Run(id, func(t *testing.T) {
			c := newDiagnosticClient(1)
			c.record("concurrent", 0, id, time.Unix(1, 0), time.Millisecond, 200)
			if _, err := c.snapshot(); err == nil {
				t.Fatal("invalid server ID accepted")
			}
		})
	}
	for _, mode := range []string{"overflow", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			limit, secondID := 1, "2"
			if mode == "duplicate" {
				limit, secondID = 2, "1"
			}
			c := newDiagnosticClient(limit)
			c.record("concurrent", 0, "1", time.Unix(1, 0), time.Millisecond, 200)
			c.record("concurrent", 1, secondID, time.Unix(1, 0), time.Millisecond, 200)
			if _, err := c.snapshot(); err == nil {
				t.Fatal("incomplete diagnostic evidence accepted")
			}
		})
	}
}
