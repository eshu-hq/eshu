// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func writeDiagnosticClientReport(path string, client *diagnosticClient, expected int) error {
	if path == "" {
		return nil
	}
	records, sampleErr := client.snapshot()
	errors := []string{}
	if sampleErr != nil {
		errors = append(errors, "invalid_client_samples")
	}
	seen := make(map[int]bool)
	for _, record := range records {
		if record.Phase != "concurrent" {
			continue
		}
		if seen[record.Index] || record.Index >= expected {
			errors = append(errors, "invalid_concurrent_indexes")
			break
		}
		seen[record.Index] = true
	}
	if expected <= 0 || len(seen) != expected {
		errors = append(errors, "missing_concurrent_samples")
	}
	report := struct {
		SchemaVersion int                      `json:"schema_version"`
		Complete      bool                     `json:"complete"`
		Samples       []diagnosticClientSample `json:"samples"`
		Errors        []string                 `json:"errors"`
	}{1, len(errors) == 0, records, errors}
	file, err := os.CreateTemp(filepath.Dir(path), ".diagnostic-client-*")
	if err != nil {
		return fmt.Errorf("create diagnostic client report: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := json.NewEncoder(file).Encode(report); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode diagnostic client report: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close diagnostic client report: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("publish diagnostic client report: %w", err)
	}
	if !report.Complete {
		return fmt.Errorf("diagnostic client report is incomplete")
	}
	return nil
}
