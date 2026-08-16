// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"io"
	"os"

	"gopkg.in/yaml.v3"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/componentindex"
)

// RunIndexVerify validates a component extension index file for local or CI
// publication gates and reports the verdict. An invalid index renders the
// issue list and returns the verification error the command exits with.
func RunIndexVerify(w io.Writer, jsonOutput bool, indexPath string) error {
	index, err := loadComponentIndex(indexPath)
	if err != nil {
		return renderError(w, jsonOutput, "index verify", err)
	}
	report := componentindex.Validate(index)
	if !report.Valid {
		return renderComponentIndexReport(w, jsonOutput, index, report, componentIndexVerificationFailure(report))
	}
	return renderComponentIndexReport(w, jsonOutput, index, report, nil)
}

// loadComponentIndex reads and decodes an index file. The read error is
// deliberately replaced with a fixed message so a missing-file failure never
// echoes the operator's local path into output that may be archived.
func loadComponentIndex(path string) (componentindex.Index, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is an operator-supplied CLI flag pointing to a local component index file, not an HTTP request param
	if err != nil {
		return componentindex.Index{}, componentcore.NewError(componentcore.ErrorCodeInvalidManifest, "read component index failed") //nolint:wrapcheck // the fixed message is the point: the read error would leak the local path
	}
	var index componentindex.Index
	if err := yaml.Unmarshal(raw, &index); err != nil {
		return componentindex.Index{}, componentcore.Errorf(componentcore.ErrorCodeInvalidManifest, "decode component index: %v", err)
	}
	return index, nil
}

// renderComponentIndexReport writes the verification verdict in the shape
// the operator asked for and returns err unchanged. In JSON mode the report
// is always written; in text mode a failure lists every issue row.
func renderComponentIndexReport(
	w io.Writer,
	jsonOutput bool,
	index componentindex.Index,
	report componentindex.Report,
	err error,
) error {
	if jsonOutput {
		status := "verified"
		if err != nil {
			status = "failed"
		}
		payload := newCLIOutput("index verify", status)
		payload.IndexReport = &report
		if err != nil {
			payload.Error = errorPayload(err)
		}
		if writeErr := writeJSON(w, payload); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if writeErr := writef(w, "component index verification failed with %d issues\n", len(report.Issues)); writeErr != nil {
			return writeErr
		}
		for _, issue := range report.Issues {
			if writeErr := writef(
				w,
				"%s\t%s\t%s\t%s\n",
				issue.Code,
				issue.ComponentID,
				issue.Field,
				issue.Message,
			); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	return writef(w, "verified component index with %d entries\n", len(index.Entries))
}

// componentIndexVerificationFailure turns an invalid index report into the
// error the command exits with, carrying the issue count only.
func componentIndexVerificationFailure(report componentindex.Report) error {
	return componentcore.Errorf(componentcore.ErrorCodeInvalidManifest, "component index verification failed: %d issue(s)", len(report.Issues))
}
