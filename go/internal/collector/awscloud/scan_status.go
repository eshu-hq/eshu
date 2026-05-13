package awscloud

import "time"

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
