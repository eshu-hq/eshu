package awscloud

import (
	"regexp"
	"strings"
	"time"
)

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
	// WarningAssumeRoleFailed marks a scan that could not acquire target
	// account credentials.
	WarningAssumeRoleFailed = "assumerole_failed"
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
