package ssm

import (
	"context"
	"time"
)

// Client lists metadata-only SSM Parameter Store observations for one AWS
// claim.
type Client interface {
	ListParameters(ctx context.Context) ([]Parameter, error)
}

// Parameter is the scanner-owned SSM Parameter Store metadata model. It
// intentionally excludes parameter values, history values, raw descriptions,
// raw allowed patterns, and raw policy JSON.
type Parameter struct {
	ARN                   string
	Name                  string
	Type                  string
	Tier                  string
	DataType              string
	KeyID                 string
	LastModifiedAt        time.Time
	DescriptionPresent    bool
	AllowedPatternPresent bool
	Policies              []PolicyMetadata
	Tags                  map[string]string
}

// PolicyMetadata carries the safe shape of an SSM parameter policy without the
// policy JSON text.
type PolicyMetadata struct {
	Type   string
	Status string
}
