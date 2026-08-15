// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"io"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/extensionconformance"
)

// RunConform runs the extension conformance fixtures declared by a manifest
// and reports the outcome. Under --json the full conformance report is
// written even when the run failed, so a CI gate can archive the findings;
// the run error still decides the exit code.
func RunConform(
	ctx context.Context,
	w io.Writer,
	jsonOutput bool,
	home string,
	manifestPath string,
	fixtures []string,
	mode string,
) error {
	report, runErr := extensionconformance.Run(ctx, extensionconformance.Request{
		ManifestPath:  manifestPath,
		FixturePaths:  fixtures,
		Mode:          extensionconformance.Mode(mode),
		ComponentHome: home,
	})
	if jsonOutput {
		payload := newCLIOutput("conform", conformanceStatus(report))
		payload.Conformance = &report
		if runErr != nil {
			payload.Error = errorPayload(componentcore.WrapError(
				componentcore.ErrorCodeConformanceFailed,
				runErr.Error(),
				runErr,
			))
		}
		if writeErr := writeJSON(w, payload); writeErr != nil {
			return writeErr
		}
	}
	if runErr != nil {
		return runErr //nolint:wrapcheck // the conformance runner's message is the operator-facing failure text; a wrap would double it
	}
	if jsonOutput {
		return nil
	}
	return writef(
		w,
		"conformance passed %s@%s fixtures=%d facts=%d\n",
		report.ComponentID,
		report.ComponentVersion,
		report.Summary.FixtureCount,
		report.Summary.FactCount,
	)
}

// conformanceStatus reduces a conformance report to the pass/fail status
// word the CLI payload carries.
func conformanceStatus(report extensionconformance.Report) string {
	if report.Status == extensionconformance.StatusPassed {
		return "passed"
	}
	return "failed"
}
