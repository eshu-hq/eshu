package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// ErrStaleFence reports that a checkpoint write came from an older workflow
// fencing token than the row already stored for that operation.
var ErrStaleFence = errors.New("aws pagination checkpoint stale fence")

// Scope identifies the workflow claim boundary that owns checkpoint state.
// Resource parents and operations are deliberately outside Scope so one claim
// can expire all stale operation checkpoints when the generation changes.
type Scope struct {
	CollectorInstanceID string
	AccountID           string
	Region              string
	ServiceKind         string
	GenerationID        string
	FencingToken        int64
}

// Key identifies one paginated AWS API operation under a claim. ResourceParent
// is empty for claim-wide operations and should be an ARN or other stable
// parent identifier for child-resource scans such as ECR repository images.
type Key struct {
	Scope          Scope
	ResourceParent string
	Operation      string
}

// Checkpoint is the persisted resume marker for one paginated operation.
// PageToken is the token to retry next; PageNumber is advisory operator context.
type Checkpoint struct {
	Key        Key
	PageToken  string
	PageNumber int
	Payload    map[string]any
	UpdatedAt  time.Time
}

// Store persists claim-fenced pagination progress for AWS scanners.
type Store interface {
	Load(context.Context, Key) (Checkpoint, bool, error)
	Save(context.Context, Checkpoint) error
	Complete(context.Context, Key) error
	ExpireStale(context.Context, Scope) (int64, error)
}

// ScopeFromBoundary copies the durable claim identity out of an AWS fact
// boundary so service adapters cannot accidentally omit generation fencing.
func ScopeFromBoundary(boundary awscloud.Boundary) Scope {
	return Scope{
		CollectorInstanceID: boundary.CollectorInstanceID,
		AccountID:           boundary.AccountID,
		Region:              boundary.Region,
		ServiceKind:         boundary.ServiceKind,
		GenerationID:        boundary.GenerationID,
		FencingToken:        boundary.FencingToken,
	}
}

// Identity returns a stable low-level key for maps and logs. It must not be
// used as a metric label because ResourceParent can contain ARNs.
func (k Key) Identity() string {
	parts := []string{
		k.Scope.CollectorInstanceID,
		k.Scope.AccountID,
		k.Scope.Region,
		k.Scope.ServiceKind,
		k.Scope.GenerationID,
		k.ResourceParent,
		k.Operation,
	}
	return strings.Join(parts, "\x00")
}

// Validate rejects incomplete checkpoint keys before they reach storage.
func (k Key) Validate() error {
	if err := k.Scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(k.Operation) == "" {
		return fmt.Errorf("aws pagination checkpoint operation is required")
	}
	return nil
}

// Validate rejects incomplete checkpoint scopes before storage mutation.
func (s Scope) Validate() error {
	switch {
	case strings.TrimSpace(s.CollectorInstanceID) == "":
		return fmt.Errorf("aws pagination checkpoint collector_instance_id is required")
	case strings.TrimSpace(s.AccountID) == "":
		return fmt.Errorf("aws pagination checkpoint account_id is required")
	case strings.TrimSpace(s.Region) == "":
		return fmt.Errorf("aws pagination checkpoint region is required")
	case strings.TrimSpace(s.ServiceKind) == "":
		return fmt.Errorf("aws pagination checkpoint service_kind is required")
	case strings.TrimSpace(s.GenerationID) == "":
		return fmt.Errorf("aws pagination checkpoint generation_id is required")
	case s.FencingToken <= 0:
		return fmt.Errorf("aws pagination checkpoint fencing_token must be positive")
	default:
		return nil
	}
}

// Validate rejects incomplete persisted checkpoints before storage mutation.
func (c Checkpoint) Validate() error {
	if err := c.Key.Validate(); err != nil {
		return err
	}
	if c.PageNumber < 0 {
		return fmt.Errorf("aws pagination checkpoint page_number must be non-negative")
	}
	return nil
}
