// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

type gateRunSummary struct {
	CommandsRun      int `json:"commands_run"`
	CommandsReused   int `json:"commands_reused"`
	SelfTestsSkipped int `json:"self_tests_skipped"`
	AdvisorySkipped  int `json:"advisory_skipped"`
	BlockingFailures int `json:"blocking_failures"`
}

type gateCommandReport struct {
	GateID         string `json:"gate_id"`
	Kind           string `json:"kind"`
	CommandSHA256  string `json:"command_sha256,omitempty"`
	Outcome        string `json:"outcome"`
	DurationMS     int64  `json:"duration_ms"`
	SourceDuration int64  `json:"source_duration_ms,omitempty"`
	ReusedFrom     string `json:"reused_from,omitempty"`
	SkipReason     string `json:"skip_reason,omitempty"`
	Blocking       bool   `json:"blocking"`
}

type gateRunReport struct {
	SchemaVersion string              `json:"schema_version"`
	SelfTests     string              `json:"self_tests"`
	BlockingOnly  bool                `json:"blocking_only"`
	DurationMS    int64               `json:"duration_ms"`
	Summary       gateRunSummary      `json:"summary"`
	Commands      []gateCommandReport `json:"commands"`
}

func newGateRunReport(options executeOptions) gateRunReport {
	return gateRunReport{
		SchemaVersion: "v1",
		SelfTests:     string(options.selfTests),
		BlockingOnly:  options.blockingOnly,
	}
}

func (r *gateRunReport) addCommand(
	gate cigates.Gate,
	command localGateCommand,
	result sharedGateCommandResult,
	reused bool,
) {
	outcome := "pass"
	if result.err != nil {
		outcome = "fail"
		if gate.Blocking {
			r.Summary.BlockingFailures++
		}
	}
	record := gateCommandReport{
		GateID:        gate.ID,
		Kind:          command.label,
		CommandSHA256: commandSHA256(command.command),
		Outcome:       outcome,
		Blocking:      gate.Blocking,
	}
	if reused {
		record.ReusedFrom = result.gateID
		record.SourceDuration = result.durationMS
		r.Summary.CommandsReused++
	} else {
		record.DurationMS = result.durationMS
		r.Summary.CommandsRun++
	}
	r.Commands = append(r.Commands, record)
}

func (r *gateRunReport) addSkipped(gate cigates.Gate, kind, reason string) {
	r.Commands = append(r.Commands, gateCommandReport{
		GateID:     gate.ID,
		Kind:       kind,
		Outcome:    "skipped",
		SkipReason: reason,
		Blocking:   gate.Blocking,
	})
	if kind == "test_command" {
		r.Summary.SelfTestsSkipped++
	}
	if kind == "gate" && !gate.Blocking {
		r.Summary.AdvisorySkipped++
	}
}

func writeGateRunReport(path string, report gateRunReport) error {
	if path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode gate run report: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create gate run report directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".gate-run-report-*")
	if err != nil {
		return fmt.Errorf("create gate run report temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod gate run report: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write gate run report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close gate run report: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publish gate run report: %w", err)
	}
	return nil
}
