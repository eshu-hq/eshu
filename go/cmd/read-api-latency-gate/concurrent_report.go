// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type concurrentOperationReport struct {
	ID         string    `json:"id"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	MCP        bool      `json:"mcp"`
	SamplesMS  []float64 `json:"samples_ms"`
	P95MS      float64   `json:"p95_ms"`
	Status     int       `json:"status"`
	Exercised  bool      `json:"exercised"`
	HardFailed bool      `json:"hard_failed"`
}

type concurrentReport struct {
	Identity   LatencyReportIdentity       `json:"identity"`
	Operations []concurrentOperationReport `json:"operations"`
}

func writeConcurrentReport(path string, opts runOptions, results []RouteLatency) error {
	if path == "" {
		return nil
	}
	report := concurrentReport{Identity: latencyReportIdentityFrom(opts), Operations: make([]concurrentOperationReport, 0, len(results))}
	for _, result := range results {
		row := concurrentOperationReport{ID: result.Route, Method: result.Method, Path: result.Path, MCP: result.MCP, P95MS: millis(result.P95), Status: result.Status, Exercised: result.Exercised, HardFailed: result.HardFailed, SamplesMS: make([]float64, len(result.Samples))}
		for i, sample := range result.Samples {
			row.SamplesMS[i] = millis(sample)
		}
		report.Operations = append(report.Operations, row)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode concurrent report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write concurrent report %s: %w", path, err)
	}
	return nil
}
