// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Flush writes a completed or explicitly invalid report after HTTP shutdown.
func (d *Collector) Flush(ctx context.Context) error {
	if d == nil {
		return nil
	}
	flushErr := d.provider.ForceFlush(ctx)
	d.mu.Lock()
	report := statusDiagnosticReport{
		SchemaVersion: 1,
		Overflow:      d.overflow,
		Requests:      append([]statusDiagnosticRequest(nil), d.requests...),
		Spans:         append([]statusDiagnosticSpan(nil), d.spans...),
		Errors:        []string{},
	}
	if d.closed {
		report.Errors = append(report.Errors, "duplicate_flush")
	}
	if flushErr != nil {
		report.Errors = append(report.Errors, "provider_flush_failed")
	}
	if len(report.Requests) == 0 {
		report.Errors = append(report.Errors, "no_target_requests")
	}
	d.closed = true
	if report.Overflow {
		report.Errors = append(report.Errors, "capacity_exceeded")
	}
	if len(d.active) != 0 {
		report.Errors = append(report.Errors, "active_requests")
	}
	if len(d.pending) != 0 {
		report.Errors = append(report.Errors, "unfinished_spans")
	}
	spanNames := make(map[string]map[string]bool, len(report.Requests))
	for _, span := range report.Spans {
		if spanNames[span.RequestID] == nil {
			spanNames[span.RequestID] = make(map[string]bool)
		}
		spanNames[span.RequestID][span.Name] = true
		if span.Name == "postgres.reader_access" && span.Role == "writer" && span.Stage == "writer_checkpoint" {
			spanNames[span.RequestID]["writer_checkpoint"] = true
		}
		if span.Name == "postgres.reader_access" && span.Role == "reader" {
			spanNames[span.RequestID]["reader_access"] = true
		}
	}
	for _, request := range report.Requests {
		if !spanNames[request.ID][statusDiagnosticRoot] || !spanNames[request.ID]["eshu-api"] ||
			(request.StatusCode == 200 && (!spanNames[request.ID][statusDiagnosticDispatch] ||
				!spanNames[request.ID]["writer_checkpoint"] || !spanNames[request.ID]["postgres.status_snapshot"] ||
				!spanNames[request.ID]["reader_access"])) {
			report.Errors = append(report.Errors, "missing_required_span")
			break
		}
	}
	for _, request := range report.Requests {
		if request.TraceID == "" || request.EndUnixNano == 0 || request.StatusCode == 0 {
			report.Errors = append(report.Errors, "incomplete_request")
			break
		}
	}
	report.Complete = len(report.Errors) == 0
	d.mu.Unlock()
	if err := writeStatusDiagnosticReport(d.path, report); err != nil {
		return err
	}
	if !report.Complete {
		return errors.New("status diagnostic report incomplete")
	}
	return nil
}

func writeStatusDiagnosticReport(path string, report statusDiagnosticReport) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".status-diagnostic-*")
	if err != nil {
		return fmt.Errorf("create diagnostic report: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure diagnostic report: %w", err)
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(report); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode diagnostic report: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync diagnostic report: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close diagnostic report: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("publish diagnostic report: %w", err)
	}
	return nil
}
