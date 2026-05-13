package ecr

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS ECR repository, lifecycle policy, and image-reference facts
// for one claimed account and region.
type Scanner struct {
	Client Client
}

// Scan observes ECR repositories, images, and lifecycle policies through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ecr scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceECR
	case awscloud.ServiceECR:
	default:
		return nil, fmt.Errorf("ecr scanner received service_kind %q", boundary.ServiceKind)
	}

	repositories, err := s.Client.ListRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ECR repositories: %w", err)
	}
	var envelopes []facts.Envelope
	for _, repository := range repositories {
		resource, err := awscloud.NewResourceEnvelope(repositoryObservation(boundary, repository))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		images, err := s.Client.ListImages(ctx, repository)
		if err != nil {
			return nil, fmt.Errorf("list ECR images for repository %q: %w", repository.Name, err)
		}
		for _, image := range images {
			imageRefs, err := imageReferenceEnvelopes(boundary, image)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, imageRefs...)
		}

		policy, err := s.Client.GetLifecyclePolicy(ctx, repository)
		if err != nil {
			return nil, fmt.Errorf("get ECR lifecycle policy for repository %q: %w", repository.Name, err)
		}
		if policy != nil {
			resource, err := awscloud.NewResourceEnvelope(lifecyclePolicyObservation(boundary, *policy))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, resource)
		}
	}
	return envelopes, nil
}

func repositoryObservation(boundary awscloud.Boundary, repository Repository) awscloud.ResourceObservation {
	repositoryARN := strings.TrimSpace(repository.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          repositoryARN,
		ResourceID:   firstNonEmpty(repositoryARN, repository.URI, repository.Name),
		ResourceType: awscloud.ResourceTypeECRRepository,
		Name:         repository.Name,
		Tags:         repository.Tags,
		Attributes: map[string]any{
			"created_at":           timeOrNil(repository.CreatedAt),
			"encryption_type":      strings.TrimSpace(repository.EncryptionType),
			"image_tag_mutability": strings.TrimSpace(repository.ImageTagMutability),
			"kms_key":              strings.TrimSpace(repository.KMSKey),
			"repository_uri":       strings.TrimSpace(repository.URI),
			"scan_on_push":         repository.ScanOnPush,
		},
		CorrelationAnchors: []string{repositoryARN, repository.URI, repository.Name},
		SourceRecordID:     firstNonEmpty(repositoryARN, repository.Name),
	}
}

func lifecyclePolicyObservation(boundary awscloud.Boundary, policy LifecyclePolicy) awscloud.ResourceObservation {
	repositoryARN := strings.TrimSpace(policy.RepositoryARN)
	resourceID := repositoryARN + "#lifecycle-policy"
	if strings.TrimSpace(repositoryARN) == "" {
		resourceID = strings.TrimSpace(policy.RepositoryName) + "#lifecycle-policy"
	}
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeECRLifecyclePolicy,
		Name:         strings.TrimSpace(policy.RepositoryName) + ":lifecycle-policy",
		Attributes: map[string]any{
			"last_evaluated_at":      timeOrNil(policy.LastEvaluatedAt),
			"parent_repository_arn":  repositoryARN,
			"parent_repository_name": strings.TrimSpace(policy.RepositoryName),
			"policy_text":            strings.TrimSpace(policy.PolicyText),
			"registry_id":            strings.TrimSpace(policy.RegistryID),
		},
		CorrelationAnchors: []string{resourceID, repositoryARN, policy.RepositoryName},
		SourceRecordID:     resourceID,
	}
}

func imageReferenceEnvelopes(boundary awscloud.Boundary, image Image) ([]facts.Envelope, error) {
	tags := image.Tags
	if len(tags) == 0 {
		tags = []string{""}
	}
	envelopes := make([]facts.Envelope, 0, len(tags))
	for _, tag := range tags {
		envelope, err := awscloud.NewImageReferenceEnvelope(awscloud.ImageReferenceObservation{
			Boundary:          boundary,
			RepositoryARN:     image.RepositoryARN,
			RepositoryName:    image.RepositoryName,
			RegistryID:        image.RegistryID,
			ImageDigest:       image.ImageDigest,
			ManifestDigest:    image.ManifestDigest,
			Tag:               tag,
			PushedAt:          image.PushedAt,
			ImageSizeInBytes:  image.ImageSizeInBytes,
			ManifestMediaType: image.ManifestMediaType,
			ArtifactMediaType: image.ArtifactMediaType,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
