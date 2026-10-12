// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

const diagnosticResponseHeader = "X-Eshu-Diagnostic-ID"

type diagnosticRequest struct {
	client *diagnosticClient
	phase  string
	index  int
}

func diagnosticRequestFor(client *diagnosticClient, phase string, index int) *diagnosticRequest {
	if client == nil {
		return nil
	}
	return &diagnosticRequest{client: client, phase: phase, index: index}
}

// diagnosticClientSample binds a dispatch index to the server's response ID.
// It preserves the sweep's measured interval, which excludes validation work.
// No URL, credentials, response payload or transport error is retained.
type diagnosticClientSample struct {
	Phase           string  `json:"phase"`
	Index           int     `json:"index"`
	ServerID        string  `json:"server_id"`
	StartUnixNano   int64   `json:"start_unix_nano"`
	MeasuredEndNano int64   `json:"measured_end_unix_nano"`
	ElapsedMS       float64 `json:"elapsed_ms"`
	Status          int     `json:"status_code"`
}

// diagnosticClient retains a bounded set of client observations. Errors are
// closed codes; malformed response IDs never enter the retained artifact.
type diagnosticClient struct {
	mu      sync.Mutex
	limit   int
	samples []diagnosticClientSample
	ids     map[string]bool
	failure string
}

func newDiagnosticClient(limit int) *diagnosticClient {
	return &diagnosticClient{limit: limit, ids: make(map[string]bool)}
}

func (c *diagnosticClient) record(phase string, index int, id string, start time.Time, elapsed time.Duration, status int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failure != "" {
		return
	}
	if !validDiagnosticID(id) {
		c.failure = "missing_or_invalid_server_id"
		return
	}
	if phase != "warmup" && phase != "serial" && phase != "concurrent" || index < 0 || index >= 4096 || elapsed < 0 {
		c.failure = "invalid_client_boundary"
		return
	}
	if c.ids[id] {
		c.failure = "duplicate_server_id"
		return
	}
	if len(c.samples) >= c.limit {
		c.failure = "client_record_overflow"
		return
	}
	c.ids[id] = true
	c.samples = append(c.samples, diagnosticClientSample{
		Phase: phase, Index: index, ServerID: id, StartUnixNano: start.UnixNano(),
		MeasuredEndNano: start.Add(elapsed).UnixNano(), ElapsedMS: float64(elapsed) / float64(time.Millisecond), Status: status,
	})
}

func validDiagnosticID(id string) bool {
	if len(id) == 0 || len(id) > 20 || id[0] == '0' {
		return false
	}
	for _, char := range id {
		if char < '0' || char > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(id, 10, 64)
	return err == nil
}

func (c *diagnosticClient) snapshot() ([]diagnosticClientSample, error) {
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	records := append([]diagnosticClientSample(nil), c.samples...)
	sort.Slice(records, func(i, j int) bool {
		if records[i].Phase != records[j].Phase {
			return records[i].Phase < records[j].Phase
		}
		return records[i].Index < records[j].Index
	})
	if c.failure != "" {
		return records, fmt.Errorf("diagnostic client: %s", c.failure)
	}
	return records, nil
}
