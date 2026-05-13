package ecr

import (
	"context"
	"time"
)

// Client is the ECR read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListRepositories(context.Context) ([]Repository, error)
	ListImages(context.Context, Repository) ([]Image, error)
	GetLifecyclePolicy(context.Context, Repository) (*LifecyclePolicy, error)
}

// Repository is the scanner-owned representation of an ECR repository.
type Repository struct {
	ARN                string
	Name               string
	URI                string
	RegistryID         string
	ImageTagMutability string
	EncryptionType     string
	KMSKey             string
	ScanOnPush         bool
	CreatedAt          time.Time
	Tags               map[string]string
}

// Image is the scanner-owned representation of an ECR image digest and its
// tag set.
type Image struct {
	RepositoryARN     string
	RepositoryName    string
	RegistryID        string
	ImageDigest       string
	ManifestDigest    string
	Tags              []string
	PushedAt          time.Time
	ImageSizeInBytes  int64
	ManifestMediaType string
	ArtifactMediaType string
}

// LifecyclePolicy is the scanner-owned representation of an ECR repository
// lifecycle policy.
type LifecyclePolicy struct {
	RepositoryARN   string
	RepositoryName  string
	RegistryID      string
	PolicyText      string
	LastEvaluatedAt time.Time
}
