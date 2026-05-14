package cloudwatchlogs

import (
	"context"
	"time"
)

// Client lists metadata-only CloudWatch Logs log group observations for one AWS
// claim.
type Client interface {
	ListLogGroups(ctx context.Context) ([]LogGroup, error)
}

// LogGroup is the scanner-owned CloudWatch Logs log group model. It contains
// control-plane metadata only and intentionally excludes log events, log stream
// payloads, Insights query results, export payloads, resource policies,
// subscription payloads, and mutations.
type LogGroup struct {
	ARN                  string
	Name                 string
	CreationTime         time.Time
	RetentionInDays      int32
	StoredBytes          int64
	MetricFilterCount    int32
	LogGroupClass        string
	DataProtectionStatus string
	InheritedProperties  []string
	KMSKeyID             string
	DeletionProtected    bool
	BearerTokenAuth      bool
	Tags                 map[string]string
}
