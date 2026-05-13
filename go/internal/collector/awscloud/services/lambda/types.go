package lambda

import (
	"context"
	"time"
)

// Client is the Lambda read surface consumed by Scanner. Runtime adapters
// should translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListFunctions(context.Context) ([]Function, error)
	ListAliases(context.Context, Function) ([]Alias, error)
	ListEventSourceMappings(context.Context, Function) ([]EventSourceMapping, error)
}

// Function is the scanner-owned representation of a Lambda function.
type Function struct {
	ARN              string
	Name             string
	Runtime          string
	RoleARN          string
	Handler          string
	Description      string
	State            string
	LastUpdateStatus string
	PackageType      string
	Version          string
	CodeSHA256       string
	CodeSize         int64
	ImageURI         string
	ResolvedImageURI string
	KMSKeyARN        string
	SourceKMSKeyARN  string
	MemorySize       int32
	TimeoutSeconds   int32
	LastModified     time.Time
	Architectures    []string
	Environment      map[string]string
	VPCConfig        VPCConfig
	LoggingConfig    LoggingConfig
	Tags             map[string]string
}

// VPCConfig carries Lambda networking placement for EC2 topology joins.
type VPCConfig struct {
	VPCID            string
	SubnetIDs        []string
	SecurityGroupIDs []string
	IPv6AllowedForDS bool
}

// LoggingConfig carries non-secret Lambda CloudWatch Logs settings.
type LoggingConfig struct {
	LogGroup            string
	LogFormat           string
	ApplicationLogLevel string
	SystemLogLevel      string
}

// Alias is the scanner-owned representation of a Lambda alias.
type Alias struct {
	ARN             string
	Name            string
	FunctionARN     string
	FunctionVersion string
	Description     string
	RevisionID      string
	RoutingWeights  map[string]float64
}

// EventSourceMapping is the scanner-owned representation of a Lambda event
// source mapping.
type EventSourceMapping struct {
	ARN                   string
	UUID                  string
	FunctionARN           string
	EventSourceARN        string
	State                 string
	LastProcessingResult  string
	StartingPosition      string
	BatchSize             int32
	MaximumRetryAttempts  int32
	ParallelizationFactor int32
}
