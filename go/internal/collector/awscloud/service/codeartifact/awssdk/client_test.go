// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsca "github.com/aws/aws-sdk-go-v2/service/codeartifact"
	awscatypes "github.com/aws/aws-sdk-go-v2/service/codeartifact/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListDomainsReadsSafeDomainMetadata(t *testing.T) {
	client := &fakeCodeArtifactAPI{
		domainPages: []*awsca.ListDomainsOutput{{
			Domains: []awscatypes.DomainSummary{{
				Name:          aws.String("acme"),
				Arn:           aws.String("arn:aws:codeartifact:us-east-1:123456789012:domain/acme"),
				Owner:         aws.String("123456789012"),
				EncryptionKey: aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
				Status:        awscatypes.DomainStatusActive,
				CreatedTime:   aws.Time(time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)),
			}},
		}},
		domainDescribe: map[string]*awsca.DescribeDomainOutput{
			"acme": {
				Domain: &awscatypes.DomainDescription{
					Name:            aws.String("acme"),
					Arn:             aws.String("arn:aws:codeartifact:us-east-1:123456789012:domain/acme"),
					Owner:           aws.String("123456789012"),
					EncryptionKey:   aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
					S3BucketArn:     aws.String("arn:aws:s3:::assets-acme"),
					RepositoryCount: 3,
					AssetSizeBytes:  8192,
					Status:          awscatypes.DomainStatusActive,
					CreatedTime:     aws.Time(time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)),
				},
			},
		},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	domains, err := adapter.ListDomains(context.Background())
	if err != nil {
		t.Fatalf("ListDomains() error = %v, want nil", err)
	}
	if got, want := len(domains), 1; got != want {
		t.Fatalf("len(domains) = %d, want %d", got, want)
	}
	domain := domains[0]
	if domain.Name != "acme" {
		t.Fatalf("domain.Name = %q, want acme", domain.Name)
	}
	if domain.EncryptionKey != "arn:aws:kms:us-east-1:123456789012:key/abc" {
		t.Fatalf("domain.EncryptionKey = %q", domain.EncryptionKey)
	}
	if domain.S3BucketARN != "arn:aws:s3:::assets-acme" {
		t.Fatalf("domain.S3BucketARN = %q", domain.S3BucketARN)
	}
	if domain.RepositoryCount != 3 {
		t.Fatalf("domain.RepositoryCount = %d, want 3", domain.RepositoryCount)
	}
	if domain.AssetSizeBytes != 8192 {
		t.Fatalf("domain.AssetSizeBytes = %d, want 8192", domain.AssetSizeBytes)
	}
}

func TestClientListRepositoriesReadsExternalConnectionsAndUpstreams(t *testing.T) {
	client := &fakeCodeArtifactAPI{
		repositoryPages: []*awsca.ListRepositoriesOutput{{
			Repositories: []awscatypes.RepositorySummary{{
				Name:        aws.String("team-npm"),
				Arn:         aws.String("arn:aws:codeartifact:us-east-1:123456789012:repository/acme/team-npm"),
				DomainName:  aws.String("acme"),
				DomainOwner: aws.String("123456789012"),
				Description: aws.String("team npm proxy"),
				CreatedTime: aws.Time(time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)),
			}},
		}},
		repositoryDescribe: map[string]*awsca.DescribeRepositoryOutput{
			"acme/team-npm": {
				Repository: &awscatypes.RepositoryDescription{
					Name:       aws.String("team-npm"),
					Arn:        aws.String("arn:aws:codeartifact:us-east-1:123456789012:repository/acme/team-npm"),
					DomainName: aws.String("acme"),
					ExternalConnections: []awscatypes.RepositoryExternalConnectionInfo{{
						ExternalConnectionName: aws.String("public:npmjs"),
						PackageFormat:          awscatypes.PackageFormatNpm,
						Status:                 awscatypes.ExternalConnectionStatusAvailable,
					}},
					Upstreams: []awscatypes.UpstreamRepositoryInfo{
						{RepositoryName: aws.String("shared-npm")},
						{RepositoryName: aws.String("vendor-npm")},
					},
				},
			},
		},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	repositories, err := adapter.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v, want nil", err)
	}
	if got, want := len(repositories), 1; got != want {
		t.Fatalf("len(repositories) = %d, want %d", got, want)
	}
	repository := repositories[0]
	if repository.DomainName != "acme" {
		t.Fatalf("repository.DomainName = %q, want acme", repository.DomainName)
	}
	if got, want := len(repository.ExternalConnections), 1; got != want {
		t.Fatalf("len(repository.ExternalConnections) = %d, want %d", got, want)
	}
	connection := repository.ExternalConnections[0]
	if connection.Name != "public:npmjs" || connection.PackageFormat != "npm" {
		t.Fatalf("connection = %#v", connection)
	}
	if got, want := repository.Upstreams, []string{"shared-npm", "vendor-npm"}; !equalStrings(got, want) {
		t.Fatalf("repository.Upstreams = %#v, want %#v", got, want)
	}
}

// TestAdapterAPIClientForbidsPackagePayloadAndMutation is the metadata-only
// acceptance gate: the CodeArtifact SDK adapter must never read a package
// version or asset, and must never create, update, delete, publish, copy, or
// dispose a CodeArtifact resource. We reflect over the adapter-local apiClient
// interface and fail the build if any forbidden operation becomes reachable.
func TestAdapterAPIClientForbidsPackagePayloadAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		"package", "asset", "version", "readme", "dependencies",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put",
		"Publish", "Copy", "Dispose",
		"Associate", "Disassociate",
		"Tag", "Untag",
		"Enable", "Disable",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the CodeArtifact read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		lower := strings.ToLower(name)
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(lower, banned) {
				t.Fatalf("apiClient exposes package-payload method %q (token %q); the CodeArtifact adapter is metadata-only", name, banned)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the CodeArtifact adapter is metadata-only", name, prefix)
			}
		}
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is neither a List nor Describe read", name)
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCodeArtifact,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:codeartifact:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type fakeCodeArtifactAPI struct {
	domainPages        []*awsca.ListDomainsOutput
	domainCalls        int
	domainDescribe     map[string]*awsca.DescribeDomainOutput
	repositoryPages    []*awsca.ListRepositoriesOutput
	repositoryCalls    int
	repositoryDescribe map[string]*awsca.DescribeRepositoryOutput
}

func (f *fakeCodeArtifactAPI) ListDomains(
	_ context.Context,
	_ *awsca.ListDomainsInput,
	_ ...func(*awsca.Options),
) (*awsca.ListDomainsOutput, error) {
	if f.domainCalls >= len(f.domainPages) {
		return &awsca.ListDomainsOutput{}, nil
	}
	page := f.domainPages[f.domainCalls]
	f.domainCalls++
	return page, nil
}

func (f *fakeCodeArtifactAPI) DescribeDomain(
	_ context.Context,
	input *awsca.DescribeDomainInput,
	_ ...func(*awsca.Options),
) (*awsca.DescribeDomainOutput, error) {
	if f.domainDescribe == nil {
		return &awsca.DescribeDomainOutput{}, nil
	}
	if output, ok := f.domainDescribe[aws.ToString(input.Domain)]; ok {
		return output, nil
	}
	return &awsca.DescribeDomainOutput{}, nil
}

func (f *fakeCodeArtifactAPI) ListRepositories(
	_ context.Context,
	_ *awsca.ListRepositoriesInput,
	_ ...func(*awsca.Options),
) (*awsca.ListRepositoriesOutput, error) {
	if f.repositoryCalls >= len(f.repositoryPages) {
		return &awsca.ListRepositoriesOutput{}, nil
	}
	page := f.repositoryPages[f.repositoryCalls]
	f.repositoryCalls++
	return page, nil
}

func (f *fakeCodeArtifactAPI) DescribeRepository(
	_ context.Context,
	input *awsca.DescribeRepositoryInput,
	_ ...func(*awsca.Options),
) (*awsca.DescribeRepositoryOutput, error) {
	if f.repositoryDescribe == nil {
		return &awsca.DescribeRepositoryOutput{}, nil
	}
	key := aws.ToString(input.Domain) + "/" + aws.ToString(input.Repository)
	if output, ok := f.repositoryDescribe[key]; ok {
		return output, nil
	}
	return &awsca.DescribeRepositoryOutput{}, nil
}

var _ apiClient = (*fakeCodeArtifactAPI)(nil)
