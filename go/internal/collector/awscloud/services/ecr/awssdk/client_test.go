package awssdk

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

func TestMapRepositoryPreservesTagsAndEncryption(t *testing.T) {
	createdAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	repository := mapRepository(awsecrtypes.Repository{
		CreatedAt: aws.Time(createdAt),
		EncryptionConfiguration: &awsecrtypes.EncryptionConfiguration{
			EncryptionType: awsecrtypes.EncryptionTypeKms,
			KmsKey:         aws.String("arn:aws:kms:us-east-1:123456789012:key/key-1"),
		},
		ImageScanningConfiguration: &awsecrtypes.ImageScanningConfiguration{ScanOnPush: true},
		ImageTagMutability:         awsecrtypes.ImageTagMutabilityImmutable,
		RegistryId:                 aws.String("123456789012"),
		RepositoryArn:              aws.String("arn:aws:ecr:us-east-1:123456789012:repository/team/api"),
		RepositoryName:             aws.String("team/api"),
		RepositoryUri:              aws.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api"),
	}, []awsecrtypes.Tag{{
		Key:   aws.String("Environment"),
		Value: aws.String("Prod"),
	}})

	if repository.Name != "team/api" {
		t.Fatalf("Name = %q, want team/api", repository.Name)
	}
	if repository.EncryptionType != "KMS" {
		t.Fatalf("EncryptionType = %q, want KMS", repository.EncryptionType)
	}
	if !repository.ScanOnPush {
		t.Fatalf("ScanOnPush = false, want true")
	}
	if repository.Tags["Environment"] != "Prod" {
		t.Fatalf("Tags = %#v, want Environment=Prod", repository.Tags)
	}
}

func TestMapImageDetailDefaultsManifestDigest(t *testing.T) {
	pushedAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	image := mapImageDetail("arn:aws:ecr:us-east-1:123456789012:repository/team/api", awsecrtypes.ImageDetail{
		ImageDigest:            aws.String("sha256:image"),
		ImageManifestMediaType: aws.String("application/vnd.oci.image.manifest.v1+json"),
		ImagePushedAt:          aws.Time(pushedAt),
		ImageSizeInBytes:       aws.Int64(1234),
		ImageTags:              []string{"latest"},
		RegistryId:             aws.String("123456789012"),
		RepositoryName:         aws.String("team/api"),
	})

	if image.ImageDigest != "sha256:image" {
		t.Fatalf("ImageDigest = %q, want sha256:image", image.ImageDigest)
	}
	if image.ManifestDigest != "sha256:image" {
		t.Fatalf("ManifestDigest = %q, want sha256:image", image.ManifestDigest)
	}
	if len(image.Tags) != 1 || image.Tags[0] != "latest" {
		t.Fatalf("Tags = %#v, want latest", image.Tags)
	}
}

func TestIsLifecyclePolicyNotFound(t *testing.T) {
	if !isLifecyclePolicyNotFound(&awsecrtypes.LifecyclePolicyNotFoundException{}) {
		t.Fatalf("isLifecyclePolicyNotFound() = false, want true")
	}
	if isLifecyclePolicyNotFound(errors.New("LifecyclePolicyNotFoundException")) {
		t.Fatalf("plain error was classified as lifecycle policy not found")
	}
}
