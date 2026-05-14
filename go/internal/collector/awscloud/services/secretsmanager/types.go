package secretsmanager

import (
	"context"
	"time"
)

// Client lists metadata-only Secrets Manager observations for one AWS claim.
type Client interface {
	ListSecrets(ctx context.Context) ([]Secret, error)
}

// Secret is the scanner-owned Secrets Manager metadata model. It intentionally
// excludes secret values, version payloads, resource policy JSON, external
// rotation partner metadata, and mutation state.
type Secret struct {
	ARN                string
	Name               string
	DescriptionPresent bool
	KMSKeyID           string
	RotationEnabled    bool
	RotationLambdaARN  string
	CreatedAt          time.Time
	DeletedAt          time.Time
	LastChangedAt      time.Time
	LastRotatedAt      time.Time
	NextRotationAt     time.Time
	PrimaryRegion      string
	OwningService      string
	SecretType         string
	RotationEveryDays  int64
	RotationDuration   string
	RotationSchedule   string
	Tags               map[string]string
}
