// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

const concurrentReportSchemaVersion = 1

type concurrentOperationReport struct {
	ID                string    `json:"id"`
	Method            string    `json:"method"`
	Path              string    `json:"path"`
	MCP               bool      `json:"mcp"`
	SamplesMS         []float64 `json:"samples_ms"`
	P95MS             float64   `json:"p95_ms"`
	Status            int       `json:"status"`
	Exercised         bool      `json:"exercised"`
	HardFailed        bool      `json:"hard_failed"`
	Requested         int       `json:"requested"`
	Succeeded         int       `json:"succeeded"`
	Workers           int       `json:"workers"`
	PeakInFlight      int       `json:"peak_in_flight"`
	WallMS            float64   `json:"wall_ms"`
	RequestsPerSecond float64   `json:"requests_per_second"`
	Statuses          []int     `json:"statuses"`
}

type concurrentReport struct {
	Version    int                         `json:"version"`
	Identity   LatencyReportIdentity       `json:"identity"`
	Operations []concurrentOperationReport `json:"operations"`
}

func writeConcurrentReport(path string, opts runOptions, results []RouteLatency) error {
	if path == "" {
		return nil
	}
	report := concurrentReport{Version: concurrentReportSchemaVersion, Identity: latencyReportIdentityFrom(opts), Operations: make([]concurrentOperationReport, 0, len(results))}
	for _, result := range results {
		row := concurrentOperationReport{ID: result.Route, Method: result.Method, Path: result.Path, MCP: result.MCP, P95MS: millis(result.P95), Status: result.Status, Exercised: result.Exercised, HardFailed: result.HardFailed, SamplesMS: make([]float64, len(result.Samples)), Requested: result.Requested, Succeeded: result.Succeeded, Workers: result.Workers, PeakInFlight: result.PeakInFlight, WallMS: millis(result.Wall), Statuses: result.Statuses}
		if result.Wall > 0 {
			row.RequestsPerSecond = float64(result.Requested) / result.Wall.Seconds()
		}
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
