// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

func TestVerifierComparesTerraformAddressClaims(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		TerraformResolver: func(_ doctruth.DocumentInput, address string) doctruth.TerraformAddressResolution {
			switch address {
			case "aws_s3_bucket.logs", "data.aws_iam_policy_document.reader", "module.network":
				return doctruth.TerraformAddressResolution{Supported: true, Exists: true}
			case "aws_sqs_queue.missing":
				return doctruth.TerraformAddressResolution{Supported: true, Exists: false}
			default:
				return doctruth.TerraformAddressResolution{}
			}
		},
	})
	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{
		{
			Path:       "README.md",
			SourceURI:  "file:///README.md",
			RevisionID: "rev-1",
			Content: strings.Join([]string{
				"Bucket: `terraform/aws_s3_bucket.logs`.",
				"Policy: `terraform/data.aws_iam_policy_document.reader`.",
				"Module: `terraform/module.network`.",
				"Queue: `terraform/aws_sqs_queue.missing`.",
			}, "\n"),
		},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	assertFindingStatusForClaim(t, result.Findings, "terraform_address", "aws_s3_bucket.logs", "valid")
	assertFindingStatusForClaim(t, result.Findings, "terraform_address", "data.aws_iam_policy_document.reader", "valid")
	assertFindingStatusForClaim(t, result.Findings, "terraform_address", "module.network", "valid")
	assertFindingStatusForClaim(t, result.Findings, "terraform_address", "aws_sqs_queue.missing", "contradicted")
}

func TestVerifierMarksTerraformAddressUnsupportedWithoutResolver(t *testing.T) {
	t.Parallel()

	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{})
	result, err := verifier.Verify(context.Background(), []doctruth.DocumentInput{
		{
			Path:       "README.md",
			SourceURI:  "file:///README.md",
			RevisionID: "rev-1",
			Content:    "`terraform/aws_s3_bucket.logs`",
		},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}

	assertFindingStatusForClaim(t, result.Findings, "terraform_address", "aws_s3_bucket.logs", "unsupported_claim_type")
}

func TestNormalizeTerraformAddressClaimIsConservative(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "aws_s3_bucket.logs", want: "aws_s3_bucket.logs"},
		{raw: "data.aws_iam_policy_document.reader", want: "data.aws_iam_policy_document.reader"},
		{raw: "terraform/module.network", want: "module.network"},
		{raw: "module.network,", want: "module.network"},
		{raw: ".module.network", want: ""},
		{raw: ",aws_s3_bucket.logs", want: ""},
		{raw: "aws s3 ls", want: ""},
		{raw: "aws_s3_bucket", want: ""},
		{raw: "terraform/prod/main.tf", want: ""},
		{raw: "example.com:8080", want: ""},
	} {
		if got := doctruth.NormalizeTerraformAddressClaim(tc.raw); got != tc.want {
			t.Fatalf("NormalizeTerraformAddressClaim(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
