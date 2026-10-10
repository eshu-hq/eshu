// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticConcurrentCaptureJoinsReusedAndReconnectedPorts(t *testing.T) {
	const route = "GET /api/v0/status/ingesters/{ingester}"
	const path = "/api/v0/status/ingesters/repository"
	var mu sync.Mutex
	serverRequests := make([]diagnosticServerRequest, 0, 20)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now().UTC()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ingester":"repository","runtime_family":"ingester","queue":{},"health":{}}`))
		mu.Lock()
		serverRequests = append(serverRequests, diagnosticServerRequest{Peer: r.RemoteAddr, Path: r.URL.Path, Status: http.StatusOK, StartUTC: start, EndUTC: time.Now().UTC()})
		mu.Unlock()
	}))
	server.Start()
	defer server.Close()
	tracePath := filepath.Join(t.TempDir(), "trace.json")
	t.Setenv("ESHU_DIAG_HTTPTRACE_REPORT", tracePath)
	got, err := SweepConcurrentOperations(SweepOptions{BaseURL: server.URL, APIKey: "key", Timeout: time.Second}, map[string]Operation{
		route: {Method: http.MethodGet, Path: path, Expect: "ingester_status"},
	}, 4, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Requested != 20 || got[0].Succeeded != 20 {
		t.Fatalf("unexpected concurrent result: %+v", got)
	}
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Records []diagnosticHTTPRecord `json:"records"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if len(capture.Records) != 20 {
		t.Fatalf("got %d records, want 20", len(capture.Records))
	}
	for index, record := range capture.Records {
		if record.RequestIndex != index || record.LocalIP == "" || record.LocalPort == 0 || record.EndUTC.Before(record.StartUTC) || record.Status != 200 {
			t.Fatalf("record %d is incomplete: %+v", index, record)
		}
		if net.ParseIP(record.LocalIP) == nil {
			t.Fatalf("record %d has invalid local IP %q", index, record.LocalIP)
		}
	}
	mu.Lock()
	serverCopy := append([]diagnosticServerRequest(nil), serverRequests...)
	mu.Unlock()
	if err := validateDiagnosticJoin(capture.Records, serverCopy); err != nil {
		t.Fatalf("valid 20-request join: %v", err)
	}
	duplicate := append([]diagnosticServerRequest(nil), serverCopy...)
	duplicate[1] = duplicate[0]
	if err := validateDiagnosticJoin(capture.Records, duplicate); err == nil {
		t.Fatal("ambiguous duplicate server root must be rejected")
	}
}

func TestDiagnosticConcurrentCaptureReconnectsWithNewPorts(t *testing.T) {
	const path = "/api/v0/status/ingesters/repository"
	var mu sync.Mutex
	roots := make([]diagnosticServerRequest, 0, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now().UTC()
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ingester":"repository","runtime_family":"ingester","queue":{},"health":{}}`))
		mu.Lock()
		roots = append(roots, diagnosticServerRequest{Peer: r.RemoteAddr, Path: r.URL.Path, Status: 200, StartUTC: start, EndUTC: time.Now().UTC()})
		mu.Unlock()
	}))
	defer server.Close()
	report := filepath.Join(t.TempDir(), "reconnect.json")
	t.Setenv("ESHU_DIAG_HTTPTRACE_REPORT", report)
	_, err := SweepConcurrentOperations(SweepOptions{BaseURL: server.URL, APIKey: "key", Timeout: time.Second}, map[string]Operation{
		"GET /api/v0/status/ingesters/{ingester}": {Method: http.MethodGet, Path: path, Expect: "ingester_status"},
	}, 4, 20)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Records []diagnosticHTTPRecord `json:"records"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	ports := make(map[int]bool)
	for _, record := range capture.Records {
		if record.GotConnReused || ports[record.LocalPort] {
			t.Fatalf("expected new connection, got %+v", record)
		}
		ports[record.LocalPort] = true
	}
	if len(ports) != 20 {
		t.Fatalf("ports=%d, want 20", len(ports))
	}
	mu.Lock()
	serverCopy := append([]diagnosticServerRequest(nil), roots...)
	mu.Unlock()
	if err := validateDiagnosticJoin(capture.Records, serverCopy); err != nil {
		t.Fatal(err)
	}
}

// diagnosticServerRequest is a bounded server-root projection used to check a
// client/server attribution before interpreting timing evidence.
type diagnosticServerRequest struct {
	Peer     string
	Path     string
	Status   int
	StartUTC time.Time
	EndUTC   time.Time
}

// validateDiagnosticJoin rejects missing or ambiguous client/server matches.
func validateDiagnosticJoin(records []diagnosticHTTPRecord, server []diagnosticServerRequest) error {
	if len(records) != len(server) {
		return fmt.Errorf("client/server count mismatch: %d/%d", len(records), len(server))
	}
	matched := make(map[int]bool, len(server))
	for _, record := range records {
		if record.LocalPort <= 0 || net.ParseIP(record.LocalIP) == nil || record.EndUTC.Before(record.StartUTC) || record.Status != http.StatusOK {
			return fmt.Errorf("incomplete client request %d", record.RequestIndex)
		}
		candidates := 0
		selected := -1
		connAt, firstByteAt := record.StartUTC, record.EndUTC
		haveConn, haveFirstByte := false, false
		for _, event := range record.Events {
			at := record.StartUTC.Add(time.Duration(event.AtMS * float64(time.Millisecond)))
			if event.Name == "got_conn" {
				connAt, haveConn = at, true
			}
			if event.Name == "first_response_byte" {
				firstByteAt, haveFirstByte = at, true
			}
		}
		if !haveConn || !haveFirstByte || firstByteAt.Before(connAt) {
			return fmt.Errorf("request %d missing connection timing", record.RequestIndex)
		}
		for i, root := range server {
			host, port, err := net.SplitHostPort(root.Peer)
			if err != nil {
				return fmt.Errorf("invalid server peer: %w", err)
			}
			var serverPort int
			if _, err := fmt.Sscanf(port, "%d", &serverPort); err != nil {
				return err
			}
			if net.ParseIP(host).Equal(net.ParseIP(record.LocalIP)) && serverPort == record.LocalPort && root.Path == "/api/v0/status/ingesters/repository" && root.Status == http.StatusOK && !root.StartUTC.Before(connAt) && !root.StartUTC.After(firstByteAt) && !root.EndUTC.Before(record.StartUTC) && !record.EndUTC.Before(root.StartUTC) {
				candidates++
				selected = i
			}
		}
		if candidates != 1 || matched[selected] {
			return fmt.Errorf("request %d port %d interval %s..%s has %d server matches", record.RequestIndex, record.LocalPort, record.StartUTC.Format(time.RFC3339Nano), record.EndUTC.Format(time.RFC3339Nano), candidates)
		}
		matched[selected] = true
	}
	if len(matched) != len(server) {
		return fmt.Errorf("unmatched server roots")
	}
	return nil
}
