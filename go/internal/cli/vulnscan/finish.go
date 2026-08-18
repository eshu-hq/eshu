// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// finishRepoAfterCleanup stops the local runtime, if one was started, and
// then writes the output document. Cleanup runs first so a shutdown failure
// reaches the envelope as a warning as well as the terminal; it never changes
// the exit outcome, because the scan's verdict was already reached.
func finishRepoAfterCleanup(
	deps RepoDeps,
	opts RepoOptions,
	result Result,
	truth map[string]any,
	err error,
) error {
	if deps.CloseLocalRuntime != nil {
		if cleanupErr := deps.CloseLocalRuntime(); cleanupErr != nil {
			warning := fmt.Sprintf("local runtime cleanup failed: %v", cleanupErr)
			result.Warnings = append(result.Warnings, warning)
			_ = writef(deps.Stderr, "Warning: %s\n", warning)
		}
	}
	return finishRepo(deps.Stdout, opts, result, truth, err)
}

// finishRepo writes the output document for a finished run and returns the
// outcome the caller should exit with.
//
// Which document, and whether it is written at all, depends on the outcome:
//
//   - An export (--export sarif|vex) is written only for a scanner verdict --
//     findings present (3), evidence not established (4), or unsupported
//     target evidence (5) -- or for success. Any other error skips the
//     document, because the run never produced a report to export.
//   - The JSON envelope is written for every outcome. Its error member is set
//     for every failure except the findings-present verdict, which is a
//     successful scan that found something rather than a failure.
//   - The human summary is rendered for success and for the three scanner
//     verdicts; any other error is returned without a summary.
//
// A write failure replaces the outcome, because a truncated document must not
// be reported as the verdict it was meant to carry.
func finishRepo(
	stdout io.Writer,
	opts RepoOptions,
	result Result,
	truth map[string]any,
	err error,
) error {
	if truth == nil {
		truth = scan.Truth("stale", "partial", opts.Scan.Profile, scan.CurrentGraphBackend())
	}
	report := BuildReport(result, Now())
	result.Report = &report
	if opts.ExportFormat == ExportFormatSARIF {
		if err != nil && !isScannerExit(err) {
			return err
		}
		if writeErr := WriteSARIF(stdout, result, report); writeErr != nil {
			return writeErr
		}
		return err
	}
	if opts.ExportFormat == ExportFormatVEX {
		if err != nil && !isScannerExit(err) {
			return err
		}
		if writeErr := WriteVEX(stdout, result, report); writeErr != nil {
			return writeErr
		}
		return err
	}
	envelope := repoEnvelope{
		Data:  result,
		Truth: truth,
	}
	if err != nil && !isFindingsExit(err) {
		envelope.Error = &RepoError{Message: err.Error()}
	}
	if opts.Scan.JSON {
		if writeErr := writeJSONEnvelope(stdout, envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if isScannerExit(err) {
			if renderErr := RenderSummary(stdout, result); renderErr != nil {
				return renderErr
			}
		}
		return err
	}
	return RenderSummary(stdout, result)
}

// isFindingsExit reports whether err is the findings-present verdict (code
// 3), which is a successful scan that found something rather than a failure,
// so the JSON envelope carries no error member.
func isFindingsExit(err error) bool {
	var failure *Failure
	if !errors.As(err, &failure) {
		return false
	}
	return failure.Code == 3
}

// isScannerExit reports whether err is one of the scanner verdicts the report
// is still written for: findings present, evidence incomplete, or unsupported
// target evidence. Any other error skips the report, because the run never
// produced one.
func isScannerExit(err error) bool {
	var failure *Failure
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.Code {
	case 3, 4, 5:
		return true
	default:
		return false
	}
}

// writeJSONEnvelope writes an indented JSON document without HTML escaping.
// It must stay byte-identical to go/cmd/eshu's writeScanJSON, which every other
// canonical-envelope command still uses; the wrapper's tests compare the
// vuln-scan envelope against a committed golden.
//
//nolint:wrapcheck // Deliberate: the encoder's error text is what the operator sees for a failed write.
func writeJSONEnvelope(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
