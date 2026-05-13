package awsruntime

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	// CredentialModeCentralAssumeRole acquires target-account credentials with
	// STS AssumeRole from a central collector deployment.
	CredentialModeCentralAssumeRole CredentialMode = "central_assume_role"
	// CredentialModeLocalWorkloadIdentity uses the collector pod or process
	// identity in the same account boundary as the claimed target.
	CredentialModeLocalWorkloadIdentity CredentialMode = "local_workload_identity"

	// WarningAssumeRoleFailed is emitted when claim-scoped credential
	// acquisition fails before a service scan can start.
	WarningAssumeRoleFailed = "assumerole_failed"
)

// CredentialMode identifies how the runtime obtains AWS credentials for one
// claimed target.
type CredentialMode string

// Config carries the AWS runtime claim and target authorization contract.
type Config struct {
	CollectorInstanceID string
	Targets             []TargetScope
}

// TargetScope defines which AWS claims this collector instance may process.
type TargetScope struct {
	AccountID           string
	AllowedRegions      []string
	AllowedServices     []string
	MaxConcurrentClaims int
	Credentials         CredentialConfig
}

// CredentialConfig carries non-secret credential routing data for one target
// scope. RoleARN and ExternalID are configuration, not credential material.
type CredentialConfig struct {
	Mode       CredentialMode
	RoleARN    string
	ExternalID string
}

// Target is one authorized `(account, region, service_kind)` claim.
type Target struct {
	AccountID   string
	Region      string
	ServiceKind string
	Credentials CredentialConfig
}

// CredentialProvider acquires claim-scoped credentials for an authorized AWS
// target. Implementations must avoid static credentials and release temporary
// material when the returned lease is released.
type CredentialProvider interface {
	Acquire(context.Context, Target, time.Time) (CredentialLease, error)
}

// CredentialLease releases claim-scoped credential state after a scan.
type CredentialLease interface {
	Release() error
}

// ScannerFactory builds a service scanner for one authorized target and
// credential lease.
type ScannerFactory interface {
	Scanner(context.Context, Target, awscloud.Boundary, CredentialLease) (ServiceScanner, error)
}

// ServiceScanner scans one AWS service claim into durable fact envelopes.
type ServiceScanner interface {
	Scan(context.Context, awscloud.Boundary) ([]facts.Envelope, error)
}
