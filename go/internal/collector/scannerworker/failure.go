// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FailureDisposition identifies whether a failure should retry or dead-letter.
type FailureDisposition string

const (
	// FailureRetryable records a failure that workflow may retry.
	FailureRetryable FailureDisposition = "retryable"
	// FailureDeadLetter records a terminal scanner-worker failure.
	FailureDeadLetter FailureDisposition = "dead_letter"
)

// FailureClass is the bounded scanner-worker failure vocabulary used in retry,
// dead-letter, and telemetry payloads.
type FailureClass string

const (
	// FailureClassAnalyzerFailed covers analyzer process or tool failures.
	FailureClassAnalyzerFailed FailureClass = "analyzer_failed"
	// FailureClassCPULimitExceeded covers CPU budget exhaustion.
	FailureClassCPULimitExceeded FailureClass = "cpu_limit_exceeded"
	// FailureClassMemoryLimitExceeded covers memory budget exhaustion.
	FailureClassMemoryLimitExceeded FailureClass = "memory_limit_exceeded"
	// FailureClassTimeout covers analyzer timeout expiration.
	FailureClassTimeout FailureClass = "timeout"
	// FailureClassInputLimitExceeded covers oversized input payloads.
	FailureClassInputLimitExceeded FailureClass = "input_limit_exceeded"
	// FailureClassFileLimitExceeded covers target file-count overflow.
	FailureClassFileLimitExceeded FailureClass = "file_limit_exceeded"
	// FailureClassFactLimitExceeded covers excessive source fact output.
	FailureClassFactLimitExceeded FailureClass = "fact_limit_exceeded"
	// FailureClassTargetUnavailable covers missing or unreadable bounded targets.
	FailureClassTargetUnavailable FailureClass = "target_unavailable"
	// FailureClassUnsupportedTarget covers target shapes this worker cannot scan.
	FailureClassUnsupportedTarget FailureClass = "unsupported_target"
	// FailureClassSourceUnavailable covers unavailable source dependencies.
	FailureClassSourceUnavailable FailureClass = "source_unavailable"
	// FailureClassCommitFailed covers retryable persistence failures after a
	// scanner worker has produced bounded source facts.
	FailureClassCommitFailed FailureClass = "commit_failed"
)

// FailurePayload is the privacy-safe retry or dead-letter payload for scanner
// workers. It carries hashes and bounded enums, not raw target locators.
type FailurePayload struct {
	WorkItemID        string             `json:"work_item_id"`
	ClaimID           string             `json:"claim_id"`
	FencingToken      int64              `json:"fencing_token"`
	Analyzer          AnalyzerKind       `json:"analyzer"`
	TargetKind        TargetKind         `json:"target_kind"`
	TargetLocatorHash string             `json:"target_locator_hash"`
	FailureClass      FailureClass       `json:"failure_class"`
	Disposition       FailureDisposition `json:"disposition"`
	Retryable         bool               `json:"retryable"`
	Attempt           int                `json:"attempt"`
	CPUSeconds        float64            `json:"cpu_seconds"`
	PeakMemoryBytes   int64              `json:"peak_memory_bytes"`
}

// FailurePayloadFor builds the retry or dead-letter payload for scanner-worker
// failures without exposing raw target scope IDs.
func FailurePayloadFor(
	input ClaimInput,
	disposition FailureDisposition,
	failureClass FailureClass,
	usage ResourceUsage,
) (FailurePayload, error) {
	if err := input.validate(); err != nil {
		return FailurePayload{}, err
	}
	if err := failureClass.validate(); err != nil {
		return FailurePayload{}, err
	}
	retryable, err := disposition.retryable()
	if err != nil {
		return FailurePayload{}, err
	}
	if usage.CPUSeconds < 0 {
		return FailurePayload{}, fmt.Errorf("cpu_seconds must not be negative")
	}
	if usage.PeakMemoryBytes < 0 {
		return FailurePayload{}, fmt.Errorf("peak_memory_bytes must not be negative")
	}

	return FailurePayload{
		WorkItemID:        input.WorkItemID,
		ClaimID:           input.ClaimID,
		FencingToken:      input.FencingToken,
		Analyzer:          input.Analyzer,
		TargetKind:        input.Target.Kind,
		TargetLocatorHash: input.Target.LocatorHash,
		FailureClass:      failureClass,
		Disposition:       disposition,
		Retryable:         retryable,
		Attempt:           input.Attempt,
		CPUSeconds:        usage.CPUSeconds,
		PeakMemoryBytes:   usage.PeakMemoryBytes,
	}, nil
}

// String returns a JSON representation of the privacy-safe failure payload.
func (p FailurePayload) String() string {
	encoded, err := json.Marshal(p)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func (c FailureClass) validate() error {
	switch c {
	case FailureClassAnalyzerFailed,
		FailureClassCPULimitExceeded,
		FailureClassMemoryLimitExceeded,
		FailureClassTimeout,
		FailureClassInputLimitExceeded,
		FailureClassFileLimitExceeded,
		FailureClassFactLimitExceeded,
		FailureClassTargetUnavailable,
		FailureClassUnsupportedTarget,
		FailureClassSourceUnavailable,
		FailureClassCommitFailed:
		return nil
	default:
		if strings.TrimSpace(string(c)) == "" {
			return fmt.Errorf("failure_class must not be blank")
		}
		return fmt.Errorf("failure_class %q is not a scanner-worker failure class", c)
	}
}

func (d FailureDisposition) retryable() (bool, error) {
	switch d {
	case FailureRetryable:
		return true, nil
	case FailureDeadLetter:
		return false, nil
	default:
		return false, fmt.Errorf("unknown failure disposition %q", d)
	}
}
