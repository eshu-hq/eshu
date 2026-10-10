// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type diagnosticHTTPEvent struct {
	Name string  `json:"name"`
	AtMS float64 `json:"at_ms"`
}

type diagnosticHTTPRecord struct {
	Route          string                `json:"route"`
	RequestIndex   int                   `json:"request_index"`
	StartUTC       time.Time             `json:"start_utc"`
	EndUTC         time.Time             `json:"end_utc"`
	LocalIP        string                `json:"local_ip"`
	LocalPort      int                   `json:"local_port"`
	SampleMS       float64               `json:"sample_ms"`
	Status         int                   `json:"status"`
	HeadersMS      float64               `json:"headers_ms"`
	BodyMS         float64               `json:"body_ms"`
	GotConnReused  bool                  `json:"got_conn_reused"`
	GotConnWasIdle bool                  `json:"got_conn_was_idle"`
	GotConnIdleMS  float64               `json:"got_conn_idle_ms"`
	Events         []diagnosticHTTPEvent `json:"events"`
}

type diagnosticHTTPTrace struct {
	mu     sync.Mutex
	start  time.Time
	record diagnosticHTTPRecord
}

func (d *diagnosticHTTPTrace) attach(req *http.Request) *http.Request {
	hooks := &httptrace.ClientTrace{
		GetConn:      func(string) { d.mark("get_conn") },
		DNSStart:     func(httptrace.DNSStartInfo) { d.mark("dns_start") },
		DNSDone:      func(httptrace.DNSDoneInfo) { d.mark("dns_done") },
		ConnectStart: func(string, string) { d.mark("connect_start") },
		ConnectDone:  func(string, string, error) { d.mark("connect_done") },
		GotConn: func(info httptrace.GotConnInfo) {
			d.mu.Lock()
			if info.Conn != nil {
				if host, port, err := net.SplitHostPort(info.Conn.LocalAddr().String()); err == nil {
					d.record.LocalIP = host
					d.record.LocalPort, _ = strconv.Atoi(port)
				}
			}
			d.record.GotConnReused = info.Reused
			d.record.GotConnWasIdle = info.WasIdle
			d.record.GotConnIdleMS = float64(info.IdleTime) / float64(time.Millisecond)
			d.mu.Unlock()
			d.mark("got_conn")
		},
		WroteRequest:         func(httptrace.WroteRequestInfo) { d.mark("wrote_request") },
		GotFirstResponseByte: func() { d.mark("first_response_byte") },
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), hooks))
}

func (d *diagnosticHTTPTrace) setStart(start time.Time) {
	d.mu.Lock()
	d.start = start
	d.record.StartUTC = start.UTC()
	d.mu.Unlock()
}

func (d *diagnosticHTTPTrace) setHeaders(elapsed time.Duration) {
	d.mu.Lock()
	d.record.HeadersMS = float64(elapsed) / float64(time.Millisecond)
	d.mu.Unlock()
}

func (d *diagnosticHTTPTrace) setBody(elapsed time.Duration) {
	d.mu.Lock()
	d.record.BodyMS = float64(elapsed) / float64(time.Millisecond)
	d.mu.Unlock()
}

func (d *diagnosticHTTPTrace) mark(name string) {
	now := time.Now()
	d.mu.Lock()
	d.record.Events = append(d.record.Events, diagnosticHTTPEvent{
		Name: name,
		AtMS: float64(now.Sub(d.start)) / float64(time.Millisecond),
	})
	d.mu.Unlock()
}

func (d *diagnosticHTTPTrace) snapshot(route string, index int, sample time.Duration, status int) diagnosticHTTPRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.record.Route = route
	d.record.RequestIndex = index
	d.record.SampleMS = float64(sample) / float64(time.Millisecond)
	d.record.EndUTC = d.record.StartUTC.Add(sample)
	if d.record.BodyMS > 0 {
		d.record.EndUTC = d.record.StartUTC.Add(time.Duration(d.record.BodyMS * float64(time.Millisecond)))
	}
	d.record.Status = status
	result := d.record
	result.Events = append([]diagnosticHTTPEvent(nil), result.Events...)
	return result
}

func writeDiagnosticHTTPTrace(path string, records []diagnosticHTTPRecord) error {
	payload := struct {
		Version    int                    `json:"version"`
		Provenance string                 `json:"provenance"`
		Records    []diagnosticHTTPRecord `json:"records"`
	}{
		Version:    1,
		Provenance: "H1 diagnostic concurrent client httptrace",
		Records:    records,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func writeDiagnosticStageMarker(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, name+".txt"), []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
}
