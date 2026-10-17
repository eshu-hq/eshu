// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// emptyStateFixture is the canonical legitimately-emptied Terraform state:
// a JSON object root with matching lineage/serial identity, an explicitly
// empty resources array, and no outputs key at all.
const emptyStateFixture = `{
	"format_version": "1.0",
	"terraform_version": "1.9.8",
	"serial": 17,
	"lineage": "lineage-123",
	"resources": []
}`

// TestParseLegitimatelyEmptyStateEmitsSnapshotOnly is the #8329 theory proof:
// a legitimately-emptied state parses as a JSON object root with matching
// lineage/serial identity into exactly one envelope (the unconditional
// snapshotFact) and zero body facts.
func TestParseLegitimatelyEmptyStateEmitsSnapshotOnly(t *testing.T) {
	t.Parallel()

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(emptyStateFixture), parseFixtureOptions(t))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if got, want := len(result.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d (snapshot only)", got, want)
	}
	snapshot := factByKind(t, result.Facts, cloud.TerraformStateSnapshotFactKind)
	if got, want := snapshot.FactKind, cloud.TerraformStateSnapshotFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got := result.ResourceFacts; got != 0 {
		t.Fatalf("ResourceFacts = %d, want 0", got)
	}
	if got := result.OutputFacts; got != 0 {
		t.Fatalf("OutputFacts = %d, want 0", got)
	}
	if got := result.ModuleFacts; got != 0 {
		t.Fatalf("ModuleFacts = %d, want 0", got)
	}
	if !terraformstate.IsLegitimatelyEmptyBody(result.ResourceFacts, result.OutputFacts, result.ModuleFacts) {
		t.Fatal("IsLegitimatelyEmptyBody() = false, want true for resources:[] with outputs absent")
	}
}

// TestParseLegitimatelyEmptyStateRejectsNonObjectRoot proves the empty shape
// still requires a JSON object root: an array root is not legitimate
// emptiness, it is malformed state.
func TestParseLegitimatelyEmptyStateRejectsNonObjectRoot(t *testing.T) {
	t.Parallel()

	_, err := terraformstate.Parse(context.Background(), strings.NewReader(`[]`), parseFixtureOptions(t))
	if err == nil {
		t.Fatal("Parse() error = nil, want non-object root error")
	}
}

// TestLegitimatelyEmptyBodyRejectsContentBearingStates proves the check only
// fires for the empty shape: any resource or output fact means the state
// still carries content worth protecting.
func TestLegitimatelyEmptyBodyRejectsContentBearingStates(t *testing.T) {
	t.Parallel()

	withResource, err := terraformstate.Parse(context.Background(), strings.NewReader(`{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"id":"i-1"}}]}
	]}`), parseFixtureOptions(t))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if terraformstate.IsLegitimatelyEmptyBody(withResource.ResourceFacts, withResource.OutputFacts, withResource.ModuleFacts) {
		t.Fatal("IsLegitimatelyEmptyBody() = true, want false for content-bearing resources")
	}

	withOutput, err := terraformstate.Parse(context.Background(), strings.NewReader(`{"serial":17,"lineage":"lineage-123","resources":[],"outputs":{
		"bucket_name": {"value": "logs", "type": "string"}
	}}`), parseFixtureOptions(t))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if terraformstate.IsLegitimatelyEmptyBody(withOutput.ResourceFacts, withOutput.OutputFacts, withOutput.ModuleFacts) {
		t.Fatal("IsLegitimatelyEmptyBody() = true, want false for content-bearing outputs")
	}
}

// TestFailureShapesNeverAttest proves the negative half of the theory: a
// missing state, an over-ceiling state, and an identity-mismatched state
// never produce an attestable parse, so no attestation may fire for them.
func TestFailureShapesNeverAttest(t *testing.T) {
	t.Parallel()

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
			Bucket: "tfstate-prod",
			Key:    "services/deleted/terraform.tfstate",
			Region: "us-east-1",
			Client: &recordingS3Client{
				err: fmt.Errorf("GET s3://tfstate-prod/services/deleted/terraform.tfstate: %w", terraformstate.ErrStateMissing),
			},
		})
		if err != nil {
			t.Fatalf("NewS3StateSource() error = %v, want nil", err)
		}
		if _, _, err := source.Open(context.Background()); !errors.Is(err, terraformstate.ErrStateMissing) {
			t.Fatalf("Open() error = %v, want ErrStateMissing (no parse to attest)", err)
		}
	})

	t.Run("too_large", func(t *testing.T) {
		t.Parallel()
		source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
			Bucket:   "tfstate-prod",
			Key:      "services/huge/terraform.tfstate",
			Region:   "us-east-1",
			MaxBytes: 8,
			Client: &recordingS3Client{
				output: terraformstate.S3GetObjectOutput{
					Body: io.NopCloser(strings.NewReader(emptyStateFixture)),
					Size: int64(len(emptyStateFixture)),
				},
			},
		})
		if err != nil {
			t.Fatalf("NewS3StateSource() error = %v, want nil", err)
		}
		if _, _, err := source.Open(context.Background()); !errors.Is(err, terraformstate.ErrStateTooLarge) {
			t.Fatalf("Open() error = %v, want ErrStateTooLarge (no parse to attest)", err)
		}
	})

	t.Run("identity_mismatch", func(t *testing.T) {
		t.Parallel()
		options := parseFixtureOptions(t)
		for _, state := range []string{
			`{"serial":16,"lineage":"lineage-123","resources":[]}`,
			`{"serial":17,"lineage":"lineage-456","resources":[]}`,
		} {
			if _, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options); err == nil {
				t.Fatalf("Parse() error = nil, want identity-mismatch error (no parse to attest)")
			}
		}
	})
}
