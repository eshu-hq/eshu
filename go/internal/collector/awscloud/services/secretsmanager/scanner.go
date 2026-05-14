package secretsmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Secrets Manager metadata facts for one claimed account and
// region. It never reads secret values, secret versions, resource policy JSON,
// partner rotation metadata, or mutates Secrets Manager resources.
type Scanner struct {
	Client Client
}

// Scan observes Secrets Manager metadata and direct KMS/Lambda dependency
// metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("secretsmanager scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceSecretsManager
	case awscloud.ServiceSecretsManager:
	default:
		return nil, fmt.Errorf("secretsmanager scanner received service_kind %q", boundary.ServiceKind)
	}

	secrets, err := s.Client.ListSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Secrets Manager secrets: %w", err)
	}
	var envelopes []facts.Envelope
	for _, secret := range secrets {
		secretEnvelopes, err := secretEnvelopes(boundary, secret)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, secretEnvelopes...)
	}
	return envelopes, nil
}

func secretEnvelopes(boundary awscloud.Boundary, secret Secret) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(secretObservation(boundary, secret))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		kmsRelationship(boundary, secret),
		rotationLambdaRelationship(boundary, secret),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func secretObservation(boundary awscloud.Boundary, secret Secret) awscloud.ResourceObservation {
	secretARN := strings.TrimSpace(secret.ARN)
	name := strings.TrimSpace(secret.Name)
	resourceID := secretResourceID(secret)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          secretARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSecretsManagerSecret,
		Name:         name,
		Tags:         cloneStringMap(secret.Tags),
		Attributes: map[string]any{
			"description_present": secret.DescriptionPresent,
			"kms_key_id":          strings.TrimSpace(secret.KMSKeyID),
			"rotation_enabled":    secret.RotationEnabled,
			"created_at":          timeOrNil(secret.CreatedAt),
			"deleted_at":          timeOrNil(secret.DeletedAt),
			"last_changed_at":     timeOrNil(secret.LastChangedAt),
			"last_rotated_at":     timeOrNil(secret.LastRotatedAt),
			"next_rotation_at":    timeOrNil(secret.NextRotationAt),
			"primary_region":      strings.TrimSpace(secret.PrimaryRegion),
			"owning_service":      strings.TrimSpace(secret.OwningService),
			"secret_type":         strings.TrimSpace(secret.SecretType),
			"rotation_every_days": secret.RotationEveryDays,
			"rotation_duration":   strings.TrimSpace(secret.RotationDuration),
			"rotation_schedule":   strings.TrimSpace(secret.RotationSchedule),
		},
		CorrelationAnchors: []string{secretARN, name},
		SourceRecordID:     resourceID,
	}
}
