// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

var prePRWholeModuleGateIDs = [...]string{"go-fmt", "go-lint", "go-build", "go-vet"}

type prePRWholeModuleRun struct {
	gate    cigates.Gate
	command localGateCommand
	key     sharedGateCommandKey
	result  sharedGateCommandResult
	output  string
}

// executePrePRWholeModulePrelude runs the whole-module Go checks before
// normal path-selected dispatch. Formatting and linting share one serial lane;
// build and vet run alongside it. Results seed the normal command-reuse map, so
// selected hygiene rows observe the exact same pass or failure without running
// the command again.
func executePrePRWholeModulePrelude(
	w io.Writer,
	sels []cigates.Selection,
	repoRoot string,
	report *gateRunReport,
	sharedResults map[sharedGateCommandKey]sharedGateCommandResult,
) (bool, error) {
	runs, err := resolvePrePRWholeModuleRuns(sels)
	if err != nil {
		return false, err
	}
	return executePrePRWholeModuleRuns(w, runs, repoRoot, report, sharedResults), nil
}

func executePrePRWholeModuleRuns(
	w io.Writer,
	runs []prePRWholeModuleRun,
	repoRoot string,
	report *gateRunReport,
	sharedResults map[sharedGateCommandKey]sharedGateCommandResult,
) bool {
	_, _ = fmt.Fprintln(w, "WHOLE   pre-pr: fmt then lint; build and vet run in parallel")

	var wg sync.WaitGroup
	run := func(index int) {
		var output bytes.Buffer
		started := time.Now()
		runs[index].result = sharedGateCommandResult{
			gateID: runs[index].gate.ID,
			err: runShellCommandWithOutput(
				runs[index].command.command, repoRoot, &output, &output,
			),
			durationMS: time.Since(started).Milliseconds(),
		}
		runs[index].output = output.String()
	}

	// The precommit helper uses worktree-local mutable state and stays
	// single-writer: lint begins only after formatting returns.
	serialCount := min(2, len(runs))
	wg.Add(1)
	go func() {
		defer wg.Done()
		for index := range serialCount {
			run(index)
		}
	}()
	for index := serialCount; index < len(runs); index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run(index)
		}()
	}
	wg.Wait()

	failed := false
	for i := range runs {
		entry := &runs[i]
		_, _ = fmt.Fprintf(w, "WHOLE   %s: %s\n", entry.gate.ID, entry.command.command)
		_, _ = io.WriteString(w, entry.output)
		report.addCommand(entry.gate, entry.command, entry.result, false)
		sharedResults[entry.key] = entry.result
		if entry.result.err != nil {
			failed = true
			printGateFailure(w, entry.gate, entry.command.label, entry.result.err)
		}
	}
	return failed
}

func resolvePrePRWholeModuleRuns(sels []cigates.Selection) ([]prePRWholeModuleRun, error) {
	byID := make(map[string]cigates.Gate, len(sels))
	for _, selection := range sels {
		if _, duplicate := byID[selection.Gate.ID]; duplicate {
			return nil, fmt.Errorf("pre-pr whole-module gate %q appears more than once", selection.Gate.ID)
		}
		byID[selection.Gate.ID] = selection.Gate
	}

	runs := make([]prePRWholeModuleRun, 0, len(prePRWholeModuleGateIDs))
	for _, id := range prePRWholeModuleGateIDs {
		gate, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("pre-pr whole-module gate %q is missing from the registry", id)
		}
		if !gate.Blocking {
			return nil, fmt.Errorf("pre-pr whole-module gate %q must be blocking", id)
		}
		if gate.CIOnlyReason != "" {
			return nil, fmt.Errorf("pre-pr whole-module gate %q must be locally runnable", id)
		}
		if gate.Local == nil {
			return nil, fmt.Errorf("pre-pr whole-module gate %q must declare a local command", id)
		}
		commands := localGateCommands(gate.Local, false)
		if len(commands) != 1 || commands[0].label != "command" {
			return nil, fmt.Errorf("pre-pr whole-module gate %q must declare one primary local command", id)
		}
		key, reusable := sharedCommandKey(gate, commands[0].label, commands[0].command)
		if !reusable {
			return nil, fmt.Errorf("pre-pr whole-module gate %q must declare a complete CI owner", id)
		}
		runs = append(runs, prePRWholeModuleRun{
			gate:    gate,
			command: commands[0],
			key:     key,
		})
	}
	if len(runs) < 2 || runs[0].gate.ID != "go-fmt" || runs[1].gate.ID != "go-lint" {
		return nil, fmt.Errorf("pre-pr whole-module gate order must start with go-fmt then go-lint")
	}
	return runs, nil
}
