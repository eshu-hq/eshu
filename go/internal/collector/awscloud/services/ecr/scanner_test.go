package ecr

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsECRRepositoriesImagesAndLifecyclePolicies(t *testing.T) {
	client := fakeClient{
		repositories: []Repository{{
			ARN:                "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
			Name:               "team/api",
			URI:                "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api",
			RegistryID:         "123456789012",
			ImageTagMutability: "IMMUTABLE",
			EncryptionType:     "KMS",
			Tags:               map[string]string{"Environment": "Prod"},
		}},
		images: map[string][]Image{
			"team/api": {
				{
					RepositoryARN:     "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
					RepositoryName:    "team/api",
					RegistryID:        "123456789012",
					ImageDigest:       "sha256:image",
					ManifestDigest:    "sha256:image",
					Tags:              []string{"v1", "latest"},
					PushedAt:          time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC),
					ImageSizeInBytes:  1234,
					ManifestMediaType: "application/vnd.oci.image.manifest.v1+json",
				},
			},
		},
		lifecyclePolicies: map[string]*LifecyclePolicy{
			"team/api": {
				RepositoryARN:   "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
				RepositoryName:  "team/api",
				RegistryID:      "123456789012",
				PolicyText:      `{"rules":[]}`,
				LastEvaluatedAt: time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC),
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 2 {
		t.Fatalf("aws_resource count = %d, want 2", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSImageReferenceFactKind] != 2 {
		t.Fatalf("aws_image_reference count = %d, want 2", counts[facts.AWSImageReferenceFactKind])
	}
	assertResourceType(t, envelopes, awscloud.ResourceTypeECRRepository)
	assertResourceType(t, envelopes, awscloud.ResourceTypeECRLifecyclePolicy)
	assertImageReference(t, envelopes, "team/api", "sha256:image", "latest")
	assertImageReference(t, envelopes, "team/api", "sha256:image", "v1")
}

func TestScannerEmitsUntaggedImageReference(t *testing.T) {
	client := fakeClient{
		repositories: []Repository{{
			ARN:        "arn:aws:ecr:us-east-1:123456789012:repository/team/worker",
			Name:       "team/worker",
			RegistryID: "123456789012",
		}},
		images: map[string][]Image{
			"team/worker": {
				{
					RepositoryARN:  "arn:aws:ecr:us-east-1:123456789012:repository/team/worker",
					RepositoryName: "team/worker",
					RegistryID:     "123456789012",
					ImageDigest:    "sha256:untagged",
					ManifestDigest: "sha256:untagged",
				},
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	assertImageReference(t, envelopes, "team/worker", "sha256:untagged", "")
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err := Scanner{Client: fakeClient{}}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceECR,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ecr:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	repositories      []Repository
	images            map[string][]Image
	lifecyclePolicies map[string]*LifecyclePolicy
}

func (c fakeClient) ListRepositories(context.Context) ([]Repository, error) {
	return c.repositories, nil
}

func (c fakeClient) ListImages(_ context.Context, repository Repository) ([]Image, error) {
	return c.images[repository.Name], nil
}

func (c fakeClient) GetLifecyclePolicy(_ context.Context, repository Repository) (*LifecyclePolicy, error) {
	if c.lifecyclePolicies == nil {
		return nil, nil
	}
	return c.lifecyclePolicies[repository.Name], nil
}

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
}

func assertImageReference(t *testing.T, envelopes []facts.Envelope, repository string, digest string, tag string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSImageReferenceFactKind {
			continue
		}
		if envelope.Payload["repository_name"] == repository &&
			envelope.Payload["image_digest"] == digest &&
			envelope.Payload["tag"] == tag {
			return
		}
	}
	t.Fatalf("missing image reference repository=%q digest=%q tag=%q in %#v", repository, digest, tag, envelopes)
}
