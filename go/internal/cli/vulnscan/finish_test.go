// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRunRepoOutputSelectionByOutcome pins which document each output mode
// writes for a scanner verdict versus an unclassified error: an export and
// the human summary are written for each of the three verdict codes the run
// produced a report for (3, 4 and 5 all appear below), and skipped for an
// error where it did not, while the JSON envelope is written either way.
func TestRunRepoOutputSelectionByOutcome(t *testing.T) {
	tests := []struct {
		name      string
		export    string
		jsonOut   bool
		findings  string
		statusErr error
		// wantSuccess marks the rows where RunRepo returns nil. Without it the
		// loop's unconditional Fatal below made the success + --export path
		// impossible to express, which is why it went unpinned: the table's
		// whole purpose is which document each mode writes, and ready-zero was
		// the one outcome it could not say anything about.
		wantSuccess  bool
		wantWritten  bool
		wantContains string
	}{
		{name: "sarif written for findings verdict", export: ExportFormatSARIF, findings: repoRunFindings, wantWritten: true, wantContains: `"$schema"`},
		{name: "sarif skipped for preflight failure", export: ExportFormatSARIF, findings: repoRunFindings, statusErr: errors.New("down"), wantWritten: false},
		{name: "vex written for not-configured verdict", export: ExportFormatVEX, findings: repoRunNotConfig, wantWritten: true, wantContains: `"statements"`},
		{name: "vex skipped for preflight failure", export: ExportFormatVEX, findings: repoRunNotConfig, statusErr: errors.New("down"), wantWritten: false},
		{name: "sarif written for unsupported verdict", export: ExportFormatSARIF, findings: repoRunUnsupported, wantWritten: true, wantContains: `"$schema"`},
		{name: "vex written for unsupported verdict", export: ExportFormatVEX, findings: repoRunUnsupported, wantWritten: true, wantContains: `"statements"`},
		{name: "summary rendered before unsupported exit", findings: repoRunUnsupported, wantWritten: true, wantContains: "Exit: code=5 reason=unsupported"},
		{name: "summary rendered before findings exit", findings: repoRunFindings, wantWritten: true, wantContains: "Exit: code=3 reason=findings_present"},
		{name: "summary skipped for preflight failure", findings: repoRunFindings, statusErr: errors.New("down"), wantWritten: false},
		{name: "json envelope written for preflight failure", jsonOut: true, findings: repoRunFindings, statusErr: errors.New("down"), wantWritten: true, wantContains: `"error"`},
		// Ready-zero success: no verdict, so the isScannerExit guard is not what
		// lets these through -- the err == nil branch is. finish.go's doc says an
		// export is written "for a scanner verdict ... or for success", and this
		// is the "or for success" half.
		{name: "sarif written for ready-zero success", export: ExportFormatSARIF, findings: repoRunReadyZero, wantSuccess: true, wantWritten: true, wantContains: `"$schema"`},
		{name: "vex written for ready-zero success", export: ExportFormatVEX, findings: repoRunReadyZero, wantSuccess: true, wantWritten: true, wantContains: `"statements"`},
		{name: "summary rendered for ready-zero success", findings: repoRunReadyZero, wantSuccess: true, wantWritten: true, wantContains: "Exit: code=0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeRepoClient{repositories: repoRunRepositories, findings: tt.findings}
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			opts := repoRunOptions(tt.jsonOut)
			opts.ExportFormat = tt.export
			deps := RepoDeps{
				Client:      client,
				ServiceURL:  "http://api.test",
				ScanRuntime: fakeScanRuntime(client, tt.statusErr),
				Stdout:      stdout,
				Stderr:      stderr,
			}

			err := RunRepo(context.Background(), deps, opts)
			switch {
			case tt.wantSuccess && err != nil:
				t.Fatalf("RunRepo() error = %v, want nil (exit 0)", err)
			case !tt.wantSuccess && err == nil:
				t.Fatal("RunRepo() error = nil, want a verdict or an error")
			}
			// The scan banner goes to stdout only in text mode with no export;
			// strip it so "written" means the document, not the banner.
			doc := strings.TrimPrefix(stdout.String(), "Scanning /work/repo...\n")
			if got := doc != ""; got != tt.wantWritten {
				t.Fatalf("document written = %v, want %v; stdout=%q", got, tt.wantWritten, stdout.String())
			}
			if tt.wantContains != "" && !strings.Contains(doc, tt.wantContains) {
				t.Fatalf("stdout = %q, want it to contain %q", doc, tt.wantContains)
			}
		})
	}
}

// TestRunRepoCleanupFailureIsAWarningNotAVerdict pins that a local runtime
// shutdown failure reaches the envelope and stderr as a warning and leaves
// the exit outcome alone, and that shutdown runs before the document is
// written on both a clean and a failing path.
func TestRunRepoCleanupFailureIsAWarningNotAVerdict(t *testing.T) {
	for _, tc := range []struct {
		name     string
		findings string
		wantCode int
	}{
		{name: "clean run", findings: repoRunReadyZero, wantCode: 0},
		{name: "findings run", findings: repoRunFindings, wantCode: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeRepoClient{repositories: repoRunRepositories, findings: tc.findings}
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			closed := 0
			deps := RepoDeps{
				Client:      client,
				ServiceURL:  "http://api.test",
				ScanRuntime: fakeScanRuntime(client, nil),
				Stdout:      stdout,
				Stderr:      stderr,
				CloseLocalRuntime: func() error {
					closed++
					if stdout.Len() != 0 {
						t.Fatal("CloseLocalRuntime ran after the document was written")
					}
					return errors.New("cleanup boom")
				},
			}

			err := RunRepo(context.Background(), deps, repoRunOptions(true))
			var failure *Failure
			switch {
			case tc.wantCode == 0 && err != nil:
				t.Fatalf("RunRepo() error = %v, want nil despite cleanup failure", err)
			case tc.wantCode != 0 && (!errors.As(err, &failure) || failure.Code != tc.wantCode):
				t.Fatalf("RunRepo() error = %v, want *Failure code %d", err, tc.wantCode)
			}
			if closed != 1 {
				t.Fatalf("CloseLocalRuntime calls = %d, want 1", closed)
			}
			if !strings.Contains(stderr.String(), "Warning: local runtime cleanup failed: cleanup boom\n") {
				t.Fatalf("stderr = %q, want the cleanup warning", stderr.String())
			}
			payload := decodeRunEnvelope(t, stdout)
			data := payload["data"].(map[string]any)
			warnings, _ := data["warnings"].([]any)
			if len(warnings) == 0 || warnings[len(warnings)-1] != "local runtime cleanup failed: cleanup boom" {
				t.Fatalf("data[warnings] = %#v, want the cleanup warning last", data["warnings"])
			}
		})
	}
}
