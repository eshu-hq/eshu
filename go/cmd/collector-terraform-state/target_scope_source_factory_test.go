package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestTargetScopeSourceFactorySelectsCandidateCredentials(t *testing.T) {
	t.Parallel()

	factory := newTargetScopeSourceFactory(targetScopeSourceFactoryConfig{
		DefaultCredentials: awsCredentialConfig{Mode: awsCredentialModeDefault},
		TargetScopes: []awsTargetScopeConfig{
			{
				TargetScopeID: "aws-prod-a",
				Credentials: awsCredentialConfig{
					Mode:    awsCredentialModeCentralAssumeRole,
					RoleARN: "arn:aws:iam::111111111111:role/eshu-tfstate-reader",
				},
				AllowedRegions:  []string{"us-east-1"},
				AllowedBackends: []string{"s3"},
			},
			{
				TargetScopeID: "aws-prod-b",
				Credentials: awsCredentialConfig{
					Mode:    awsCredentialModeCentralAssumeRole,
					RoleARN: "arn:aws:iam::222222222222:role/eshu-tfstate-reader",
				},
				AllowedRegions:  []string{"us-west-2"},
				AllowedBackends: []string{"s3"},
			},
		},
	})

	credentials, _, err := factory.credentialsForCandidate(terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
		},
		Source:        terraformstate.DiscoveryCandidateSourceSeed,
		TargetScopeID: "aws-prod-b",
		Region:        "us-west-2",
	})
	if err != nil {
		t.Fatalf("credentialsForCandidate() error = %v, want nil", err)
	}
	if got, want := credentials.RoleARN, "arn:aws:iam::222222222222:role/eshu-tfstate-reader"; got != want {
		t.Fatalf("RoleARN = %q, want %q", got, want)
	}
}

func TestTargetScopeSourceFactoryRejectsAmbiguousImplicitScope(t *testing.T) {
	t.Parallel()

	const locator = "s3://tfstate-prod/services/api/terraform.tfstate"
	factory := newTargetScopeSourceFactory(targetScopeSourceFactoryConfig{
		TargetScopes: []awsTargetScopeConfig{
			{
				TargetScopeID:   "aws-prod-a",
				Credentials:     awsCredentialConfig{Mode: awsCredentialModeDefault},
				AllowedBackends: []string{"s3"},
			},
			{
				TargetScopeID:   "aws-prod-b",
				Credentials:     awsCredentialConfig{Mode: awsCredentialModeDefault},
				AllowedBackends: []string{"s3"},
			},
		},
	})

	_, _, err := factory.credentialsForCandidate(terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     locator,
		},
		Source: terraformstate.DiscoveryCandidateSourceGraph,
		Region: "us-east-1",
	})
	if err == nil {
		t.Fatal("credentialsForCandidate() error = nil, want ambiguous target scope rejection")
	}
	if !strings.Contains(err.Error(), "ambiguous target_scope_id") {
		t.Fatalf("credentialsForCandidate() error = %v, want ambiguous target scope context", err)
	}
	assertNoRawS3LocatorInError(t, err, locator)
}

func TestTargetScopeSourceFactoryRoutingErrorsDoNotLeakS3Locator(t *testing.T) {
	t.Parallel()

	const locator = "s3://tfstate-prod/services/api/terraform.tfstate"
	tests := []struct {
		name      string
		candidate terraformstate.DiscoveryCandidate
		scopes    []awsTargetScopeConfig
	}{
		{
			name: "unknown explicit target scope",
			candidate: terraformstate.DiscoveryCandidate{
				State:         terraformstate.StateKey{BackendKind: terraformstate.BackendS3, Locator: locator},
				Source:        terraformstate.DiscoveryCandidateSourceSeed,
				TargetScopeID: "aws-missing",
				Region:        "us-east-1",
			},
			scopes: []awsTargetScopeConfig{{
				TargetScopeID: "aws-prod",
			}},
		},
		{
			name: "no matching implicit target scope",
			candidate: terraformstate.DiscoveryCandidate{
				State:  terraformstate.StateKey{BackendKind: terraformstate.BackendS3, Locator: locator},
				Source: terraformstate.DiscoveryCandidateSourceGraph,
				Region: "us-east-1",
			},
			scopes: []awsTargetScopeConfig{{
				TargetScopeID:  "aws-prod",
				AllowedRegions: []string{"us-west-2"},
			}},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := newTargetScopeSourceFactory(targetScopeSourceFactoryConfig{
				TargetScopes: tt.scopes,
			})
			_, _, err := factory.credentialsForCandidate(tt.candidate)
			if err == nil {
				t.Fatal("credentialsForCandidate() error = nil, want routing error")
			}
			assertNoRawS3LocatorInError(t, err, locator)
		})
	}
}

func TestTargetScopeSourceFactoryOpensS3WithoutLiveAWSCall(t *testing.T) {
	t.Parallel()

	factory := newTargetScopeSourceFactory(targetScopeSourceFactoryConfig{
		TargetScopes: []awsTargetScopeConfig{{
			TargetScopeID:   "aws-prod",
			Credentials:     awsCredentialConfig{Mode: awsCredentialModeDefault},
			AllowedRegions:  []string{"us-east-1"},
			AllowedBackends: []string{"s3"},
		}},
	})

	source, err := factory.OpenSource(context.Background(), terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
		},
		Source:        terraformstate.DiscoveryCandidateSourceSeed,
		TargetScopeID: "aws-prod",
		Region:        "us-east-1",
	})
	if err != nil {
		t.Fatalf("OpenSource() error = %v, want nil", err)
	}
	if source == nil {
		t.Fatal("OpenSource() source = nil, want S3 state source")
	}
}

func TestTargetScopeSourceFactoryOpensLocalCandidateWithoutTargetScope(t *testing.T) {
	t.Parallel()

	factory := newTargetScopeSourceFactory(targetScopeSourceFactoryConfig{
		TargetScopes: []awsTargetScopeConfig{
			{
				TargetScopeID:   "aws-prod-a",
				Credentials:     awsCredentialConfig{Mode: awsCredentialModeDefault},
				AllowedBackends: []string{"local"},
			},
			{
				TargetScopeID:   "aws-prod-b",
				Credentials:     awsCredentialConfig{Mode: awsCredentialModeDefault},
				AllowedBackends: []string{"local"},
			},
		},
	})
	path := filepath.Join(t.TempDir(), "prod.tfstate")
	if err := os.WriteFile(path, []byte(`{"serial":17,"lineage":"lineage-123","resources":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	source, err := factory.OpenSource(context.Background(), terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendLocal,
			Locator:     path,
		},
		Source: terraformstate.DiscoveryCandidateSourceSeed,
	})
	if err != nil {
		t.Fatalf("OpenSource() error = %v, want nil", err)
	}
	if source == nil {
		t.Fatal("OpenSource() source = nil, want local state source")
	}
}

func assertNoRawS3LocatorInError(t *testing.T, err error, locator string) {
	t.Helper()
	message := err.Error()
	if strings.Contains(message, locator) ||
		strings.Contains(message, "tfstate-prod") ||
		strings.Contains(message, "services/api/terraform.tfstate") {
		t.Fatalf("error leaked raw S3 locator material: %q", message)
	}
}
