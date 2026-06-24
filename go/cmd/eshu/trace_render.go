// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"strings"
)

func renderTraceCodeToRuntime(w io.Writer, data map[string]any) error {
	trace := traceMap(data, "code_to_runtime_trace")
	if len(trace) == 0 {
		return nil
	}
	segments := traceSlice(trace, "segments")
	if len(segments) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Code to runtime:"); err != nil {
		return err
	}
	if status := traceString(trace, "status"); status != "" {
		if _, err := fmt.Fprintf(w, "Trace status: %s\n", status); err != nil {
			return err
		}
	}
	for _, item := range segments {
		segment, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := traceString(segment, "name")
		status := traceString(segment, "status")
		if name == "" || status == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "- %s: %s", name, status); err != nil {
			return err
		}
		if count := traceInt(segment, "evidence_count"); count > 0 {
			if _, err := fmt.Fprintf(w, " (%d evidence)", count); err != nil {
				return err
			}
		}
		if basis := traceString(segment, "basis"); basis != "" {
			if _, err := fmt.Fprintf(w, " via %s", basis); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if missing := traceStrings(trace["missing_segments"]); len(missing) > 0 {
		if _, err := fmt.Fprintf(w, "Missing evidence: %s\n", strings.Join(missing, ", ")); err != nil {
			return err
		}
	}
	return nil
}
