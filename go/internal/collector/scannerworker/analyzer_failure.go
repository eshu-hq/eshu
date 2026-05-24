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

func (f AnalyzerFailure) failureClass() FailureClass {
	return f.class
}

func (f AnalyzerFailure) disposition() FailureDisposition {
	if f.retryable {
		return FailureRetryable
	}
	return FailureDeadLetter
}

func (f AnalyzerFailure) resourceUsage() ResourceUsage {
	return f.usage
}
