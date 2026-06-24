// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// ErrScanStatusStaleFence reports that an AWS scan status start/observe/commit
// row affected zero rows because a newer (generation_id, fencing_token) pair
// already owns the per-target slot. Storage adapters return this sentinel so
// the AWS claimed source can classify it as terminal and stop the orphaned-row
// retry loop described in issue #612.
var ErrScanStatusStaleFence = errors.New("AWS scan status mutation rejected by stale fence")

const (
	// MaxScanStatusMessageLength bounds persisted operator-facing AWS scan
	// failure details so raw SDK payloads cannot dominate status rows.
	MaxScanStatusMessageLength = 240
)

const (
	// ScanStatusRunning means a worker has started the AWS service claim.
	ScanStatusRunning = "running"
	// ScanStatusSucceeded means the scanner read the configured service without
	// outstanding warnings that affect completeness.
	ScanStatusSucceeded = "succeeded"
	// ScanStatusPartial means the scanner produced durable warning evidence for
	// an incomplete but resumable scan.
	ScanStatusPartial = "partial"
	// ScanStatusFailed means the scanner failed before producing a complete
	// service observation.
	ScanStatusFailed = "failed"
	// ScanStatusCredentialFailed means credential acquisition failed for the
	// target account.
	ScanStatusCredentialFailed = "credential_failed"

	// ScanCommitPending means scanner output has not yet reached the durable
	// fact commit boundary.
	ScanCommitPending = "pending"
	// ScanCommitCommitted means the fenced fact transaction committed.
	ScanCommitCommitted = "committed"
	// ScanCommitFailed means the scanner produced output but the durable fact
	// commit failed or was rejected.
	ScanCommitFailed = "failed"

	// WarningBudgetExhausted marks a scan that yielded before completing its
	// configured API budget.
	WarningBudgetExhausted = "budget_exhausted"
	// WarningThrottleSustained marks a scan that omitted optional metadata after
	// an AWS API stayed throttled past the SDK retry budget.
	WarningThrottleSustained = "throttle_sustained"
	// WarningAssumeRoleFailed marks a scan that could not acquire target
	// account credentials.
	WarningAssumeRoleFailed = "assumerole_failed"
	// WarningOrganizationsOrgAccessSkipped marks an Organizations scan skipped
	// because the caller was not using management or delegated-admin
	// credentials.
	WarningOrganizationsOrgAccessSkipped = "organizations_org_access_skipped"
)

var (
	scanStatusARNPattern       = regexp.MustCompile(`arn:aws[a-zA-Z-]*:[^\s]+`)
	scanStatusAccountPattern   = regexp.MustCompile(`\b\d{12}\b`)
	scanStatusAccessKeyPattern = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	scanStatusUUIDPattern      = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	scanStatusWhitespace       = regexp.MustCompile(`\s+`)
)

// SanitizeScanStatusMessage redacts common AWS identifiers and bounds failure
// detail before it is persisted to scan-status rows.
func SanitizeScanStatusMessage(message string) string {
	sanitized := strings.TrimSpace(message)
	if sanitized == "" {
		return ""
	}
	sanitized = scanStatusARNPattern.ReplaceAllString(sanitized, "[redacted-aws-arn]")
	sanitized = scanStatusAccountPattern.ReplaceAllString(sanitized, "[redacted-account]")
	sanitized = scanStatusAccessKeyPattern.ReplaceAllString(sanitized, "[redacted-access-key]")
	sanitized = scanStatusUUIDPattern.ReplaceAllString(sanitized, "[redacted-request-id]")
	sanitized = scanStatusWhitespace.ReplaceAllString(sanitized, " ")
	if len(sanitized) <= MaxScanStatusMessageLength {
		return sanitized
	}
	return sanitized[:MaxScanStatusMessageLength-3] + "..."
}

// ScanStatusStart marks the start of one claim-scoped AWS service scan.
type ScanStatusStart struct {
	Boundary  Boundary
	StartedAt time.Time
}

// ScanStatusObservation records scanner-side completion evidence before the
// shared fact commit boundary runs.
type ScanStatusObservation struct {
	Boundary            Boundary
	Status              string
	FailureClass        string
	FailureMessage      string
	APICallCount        int
	ThrottleCount       int
	WarningCount        int
	ResourceCount       int
	RelationshipCount   int
	TagObservationCount int
	BudgetExhausted     bool
	CredentialFailed    bool
	ObservedAt          time.Time
}

// ScanStatusCommit records the outcome of the fenced fact transaction for one
// AWS claim.
type ScanStatusCommit struct {
	Boundary       Boundary
	CommitStatus   string
	FailureClass   string
	FailureMessage string
	CompletedAt    time.Time
}
