// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"fmt"
)

// AnalyzerResult is the output and measured resource usage from one analyzer
// execution.
type AnalyzerResult struct {
	Output FactOutput
	Usage  ResourceUsage
}

// Analyzer executes one bounded scanner-worker claim.
type Analyzer interface {
	Analyze(context.Context, ClaimInput) (AnalyzerResult, error)
}

// AnalyzerFailure is a bounded analyzer failure suitable for workflow retry or
// dead-letter handling.
type AnalyzerFailure struct {
	class     FailureClass
	retryable bool
	usage     ResourceUsage
}

// NewRetryableAnalyzerFailure records a retryable analyzer failure class.
func NewRetryableAnalyzerFailure(class FailureClass, usage ResourceUsage, cause error) error {
	// The raw cause can contain repository paths, image names, or package
	// coordinates. Keep workflow payloads bounded to failure_class and usage.
	_ = cause
	return AnalyzerFailure{class: class, retryable: true, usage: usage}
}

// NewTerminalAnalyzerFailure records a terminal analyzer failure class.
func NewTerminalAnalyzerFailure(class FailureClass, usage ResourceUsage, cause error) error {
	// The raw cause can contain repository paths, image names, or package
	// coordinates. Keep workflow payloads bounded to failure_class and usage.
	_ = cause
	return AnalyzerFailure{class: class, retryable: false, usage: usage}
}

func (f AnalyzerFailure) Error() string {
	if f.retryable {
		return fmt.Sprintf("retryable scanner analyzer failure: %s", f.class)
	}
	return fmt.Sprintf("terminal scanner analyzer failure: %s", f.class)
}

// FailureClass returns the bounded failure class this analyzer reported.
func (f AnalyzerFailure) FailureClass() FailureClass {
	return f.class
}

// Disposition returns the workflow disposition (retryable or dead-letter).
func (f AnalyzerFailure) Disposition() FailureDisposition {
	if f.retryable {
		return FailureRetryable
	}
	return FailureDeadLetter
}

// Retryable reports whether the analyzer failure should be retried by workflow.
func (f AnalyzerFailure) Retryable() bool {
	return f.retryable
}

// ResourceUsage returns the measured CPU/memory usage at the time of failure.
func (f AnalyzerFailure) ResourceUsage() ResourceUsage {
	return f.usage
}
