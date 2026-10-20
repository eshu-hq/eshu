// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsacmpca "github.com/aws/aws-sdk-go-v2/service/acmpca"
	acmpcatypes "github.com/aws/aws-sdk-go-v2/service/acmpca/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesSensitiveAndMutatingOperations is the security
// gate for the ACM Private CA SDK adapter. The scanner contract forbids issuing
// or exporting certificates, reading the CSR body, reading the CA certificate
// chain body, and every CA lifecycle mutation. This test reflects over the
// adapter's internal apiClient interface and FAILS if any forbidden method
// appears so a future SDK refactor cannot quietly broaden the contract.
func TestAPIClientInterfaceExcludesSensitiveAndMutatingOperations(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Sensitive bodies / certificate issuance.
		"IssueCertificate",
		"GetCertificate",
		"GetCertificateAuthorityCsr",
		"GetCertificateAuthorityCertificate",
		"RevokeCertificate",
		// CA lifecycle mutations.
		"CreateCertificateAuthority",
		"DeleteCertificateAuthority",
		"UpdateCertificateAuthority",
		"RestoreCertificateAuthority",
		"ImportCertificateAuthorityCertificate",
		"CreateCertificateAuthorityAuditReport",
		// Permission and policy mutations.
		"CreatePermission",
		"DeletePermission",
		"PutPolicy",
		"DeletePolicy",
		"GetPolicy",
		// Tag mutations.
		"TagCertificateAuthority",
		"UntagCertificateAuthority",
	}
	for _, name := range forbidden {
		if _, ok := apiClientType.MethodByName(name); ok {
			t.Fatalf("apiClient interface exposes %q; ACM Private CA scanner forbids this API", name)
		}
	}
}

func TestClientListReadsDescribeAndTagsAndOmitsSensitiveBodies(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notBefore := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2031, 1, 2, 0, 0, 0, 0, time.UTC)
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"
	api := &fakeACMPCAAPI{
		listPages: []*awsacmpca.ListCertificateAuthoritiesOutput{{
			CertificateAuthorities: []acmpcatypes.CertificateAuthority{{
				Arn: aws.String(caARN),
			}},
		}},
		descriptions: map[string]*acmpcatypes.CertificateAuthority{
			caARN: {
				Arn:                        aws.String(caARN),
				OwnerAccount:               aws.String("123456789012"),
				Type:                       acmpcatypes.CertificateAuthorityTypeRoot,
				Status:                     acmpcatypes.CertificateAuthorityStatusActive,
				Serial:                     aws.String("01"),
				UsageMode:                  acmpcatypes.CertificateAuthorityUsageModeGeneralPurpose,
				KeyStorageSecurityStandard: acmpcatypes.KeyStorageSecurityStandardFips1402Level3OrHigher,
				CreatedAt:                  &createdAt,
				LastStateChangeAt:          &createdAt,
				NotBefore:                  &notBefore,
				NotAfter:                   &notAfter,
				CertificateAuthorityConfiguration: &acmpcatypes.CertificateAuthorityConfiguration{
					KeyAlgorithm:     acmpcatypes.KeyAlgorithmRsa2048,
					SigningAlgorithm: acmpcatypes.SigningAlgorithmSha256withrsa,
					Subject:          &acmpcatypes.ASN1Subject{CommonName: aws.String("Eshu Root CA")},
				},
				RevocationConfiguration: &acmpcatypes.RevocationConfiguration{
					CrlConfiguration: &acmpcatypes.CrlConfiguration{
						Enabled:      aws.Bool(true),
						S3BucketName: aws.String("eshu-crl-bucket"),
					},
					OcspConfiguration: &acmpcatypes.OcspConfiguration{Enabled: aws.Bool(false)},
				},
			},
		},
		tags: map[string][]acmpcatypes.Tag{
			caARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceACMPCA},
	}

	authorities, err := adapter.ListCertificateAuthorities(context.Background())
	if err != nil {
		t.Fatalf("ListCertificateAuthorities() error = %v, want nil", err)
	}
	if got, want := len(authorities), 1; got != want {
		t.Fatalf("len(authorities) = %d, want %d", got, want)
	}
	ca := authorities[0]
	if ca.ARN != caARN {
		t.Fatalf("ARN = %q, want %q", ca.ARN, caARN)
	}
	if ca.Type != string(acmpcatypes.CertificateAuthorityTypeRoot) {
		t.Fatalf("Type = %q, want ROOT", ca.Type)
	}
	if ca.Status != string(acmpcatypes.CertificateAuthorityStatusActive) {
		t.Fatalf("Status = %q, want ACTIVE", ca.Status)
	}
	if ca.UsageMode != string(acmpcatypes.CertificateAuthorityUsageModeGeneralPurpose) {
		t.Fatalf("UsageMode = %q", ca.UsageMode)
	}
	if ca.KeyAlgorithm != string(acmpcatypes.KeyAlgorithmRsa2048) {
		t.Fatalf("KeyAlgorithm = %q", ca.KeyAlgorithm)
	}
	if ca.SigningAlgorithm != string(acmpcatypes.SigningAlgorithmSha256withrsa) {
		t.Fatalf("SigningAlgorithm = %q", ca.SigningAlgorithm)
	}
	if ca.SubjectCommonName != "Eshu Root CA" {
		t.Fatalf("SubjectCommonName = %q", ca.SubjectCommonName)
	}
	if !ca.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", ca.CreatedAt, createdAt)
	}
	if !ca.NotAfter.Equal(notAfter) {
		t.Fatalf("NotAfter = %v, want %v", ca.NotAfter, notAfter)
	}
	if !ca.CRLEnabled || ca.CRLS3BucketName != "eshu-crl-bucket" {
		t.Fatalf("CRL config = (%v, %q)", ca.CRLEnabled, ca.CRLS3BucketName)
	}
	if ca.OCSPEnabled {
		t.Fatalf("OCSPEnabled = true, want false")
	}
	if ca.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v", ca.Tags)
	}
	if ca.KMSKeyARN != "" {
		t.Fatalf("KMSKeyARN = %q; metadata API does not report a KMS key, adapter must not synthesize one", ca.KMSKeyARN)
	}
	if ca.ParentCAARN != "" {
		t.Fatalf("ParentCAARN = %q; metadata API does not report a parent ARN, adapter must not synthesize one", ca.ParentCAARN)
	}
	if api.describeCalls == 0 {
		t.Fatalf("DescribeCertificateAuthority not called; adapter must enrich with metadata")
	}
	if api.listTagsCalls == 0 {
		t.Fatalf("ListTags not called; adapter must read tags")
	}
}

func TestClientPaginatesWithoutSameTokenLoop(t *testing.T) {
	api := &fakeACMPCAAPI{
		listPages: []*awsacmpca.ListCertificateAuthoritiesOutput{
			{
				CertificateAuthorities: []acmpcatypes.CertificateAuthority{{
					Arn: aws.String("arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/a"),
				}},
				NextToken: aws.String("token-1"),
			},
			{
				CertificateAuthorities: []acmpcatypes.CertificateAuthority{{
					Arn: aws.String("arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/b"),
				}},
			},
		},
		descriptions: map[string]*acmpcatypes.CertificateAuthority{
			"arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/a": {
				Arn:    aws.String("arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/a"),
				Status: acmpcatypes.CertificateAuthorityStatusActive,
			},
			"arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/b": {
				Arn:    aws.String("arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/b"),
				Status: acmpcatypes.CertificateAuthorityStatusActive,
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceACMPCA},
	}

	authorities, err := adapter.ListCertificateAuthorities(context.Background())
	if err != nil {
		t.Fatalf("ListCertificateAuthorities() error = %v, want nil", err)
	}
	if got, want := len(authorities), 2; got != want {
		t.Fatalf("len(authorities) = %d, want %d", got, want)
	}
	if got, want := api.listCalls, 2; got != want {
		t.Fatalf("ListCertificateAuthorities calls = %d, want %d", got, want)
	}
	if api.lastToken != "token-1" {
		t.Fatalf("last NextToken passed = %q, want %q", api.lastToken, "token-1")
	}
}

func TestClientListBreaksOnRepeatedNextToken(t *testing.T) {
	api := &fakeACMPCAAPI{
		repeatListToken: "stuck-token",
		descriptions: map[string]*acmpcatypes.CertificateAuthority{
			"arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/a": {
				Arn:    aws.String("arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/a"),
				Status: acmpcatypes.CertificateAuthorityStatusActive,
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceACMPCA},
	}

	_, err := adapter.ListCertificateAuthorities(context.Background())
	if err != nil {
		t.Fatalf("ListCertificateAuthorities() error = %v, want nil", err)
	}
	// The same non-empty token must not loop forever: the first page advances
	// from no token to "stuck-token"; the second page echoes the same token,
	// which the guard treats as no-advance and terminates pagination at two
	// calls (well under the fake's loop cap).
	if got, want := api.listCalls, 2; got != want {
		t.Fatalf("ListCertificateAuthorities calls = %d, want %d (repeated token must break the loop)", got, want)
	}
}

func TestClientListTagsBreaksOnRepeatedNextToken(t *testing.T) {
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/a"
	api := &fakeACMPCAAPI{
		listPages: []*awsacmpca.ListCertificateAuthoritiesOutput{{
			CertificateAuthorities: []acmpcatypes.CertificateAuthority{{Arn: aws.String(caARN)}},
		}},
		descriptions: map[string]*acmpcatypes.CertificateAuthority{
			caARN: {Arn: aws.String(caARN), Status: acmpcatypes.CertificateAuthorityStatusActive},
		},
		repeatTagsToken: "stuck-tags-token",
		tags: map[string][]acmpcatypes.Tag{
			caARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceACMPCA},
	}

	authorities, err := adapter.ListCertificateAuthorities(context.Background())
	if err != nil {
		t.Fatalf("ListCertificateAuthorities() error = %v, want nil", err)
	}
	if got, want := len(authorities), 1; got != want {
		t.Fatalf("len(authorities) = %d, want %d", got, want)
	}
	if got, want := api.listTagsCalls, 2; got != want {
		t.Fatalf("ListTags calls = %d, want %d (repeated token must break the loop)", got, want)
	}
}

func TestClientSkipsBlankARNSummaries(t *testing.T) {
	api := &fakeACMPCAAPI{
		listPages: []*awsacmpca.ListCertificateAuthoritiesOutput{{
			CertificateAuthorities: []acmpcatypes.CertificateAuthority{
				{Arn: aws.String("  ")},
				{Arn: nil},
			},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceACMPCA},
	}

	authorities, err := adapter.ListCertificateAuthorities(context.Background())
	if err != nil {
		t.Fatalf("ListCertificateAuthorities() error = %v, want nil", err)
	}
	if len(authorities) != 0 {
		t.Fatalf("len(authorities) = %d, want 0", len(authorities))
	}
	if api.describeCalls != 0 {
		t.Fatalf("DescribeCertificateAuthority called %d times for blank ARNs, want 0", api.describeCalls)
	}
}

type fakeACMPCAAPI struct {
	listPages     []*awsacmpca.ListCertificateAuthoritiesOutput
	listCalls     int
	describeCalls int
	listTagsCalls int
	lastToken     string
	descriptions  map[string]*acmpcatypes.CertificateAuthority
	tags          map[string][]acmpcatypes.Tag
	// repeatListToken, when set, makes ListCertificateAuthorities echo a single
	// CA plus the same non-empty NextToken forever, exercising the same-token
	// break guard. A call cap converts a missing guard into a fast test failure
	// instead of an infinite loop.
	repeatListToken string
	// repeatTagsToken does the same for the ListTags pagination loop.
	repeatTagsToken string
}

// fakePaginationCallCap bounds a fake's repeated-token output so a missing
// same-token guard fails the test quickly instead of hanging.
const fakePaginationCallCap = 100

func (f *fakeACMPCAAPI) ListCertificateAuthorities(
	_ context.Context,
	input *awsacmpca.ListCertificateAuthoritiesInput,
	_ ...func(*awsacmpca.Options),
) (*awsacmpca.ListCertificateAuthoritiesOutput, error) {
	f.lastToken = aws.ToString(input.NextToken)
	if f.repeatListToken != "" {
		f.listCalls++
		if f.listCalls > fakePaginationCallCap {
			return nil, errRepeatedTokenLoop
		}
		return &awsacmpca.ListCertificateAuthoritiesOutput{
			CertificateAuthorities: []acmpcatypes.CertificateAuthority{{
				Arn: aws.String("arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/a"),
			}},
			NextToken: aws.String(f.repeatListToken),
		}, nil
	}
	if f.listCalls >= len(f.listPages) {
		return &awsacmpca.ListCertificateAuthoritiesOutput{}, nil
	}
	page := f.listPages[f.listCalls]
	f.listCalls++
	return page, nil
}

// errRepeatedTokenLoop signals that a fake exceeded its pagination call cap,
// which means production code lacks a same-token break guard.
var errRepeatedTokenLoop = errors.New("pagination did not break on repeated NextToken")

func (f *fakeACMPCAAPI) DescribeCertificateAuthority(
	_ context.Context,
	input *awsacmpca.DescribeCertificateAuthorityInput,
	_ ...func(*awsacmpca.Options),
) (*awsacmpca.DescribeCertificateAuthorityOutput, error) {
	f.describeCalls++
	arn := aws.ToString(input.CertificateAuthorityArn)
	detail, ok := f.descriptions[arn]
	if !ok {
		return &awsacmpca.DescribeCertificateAuthorityOutput{}, nil
	}
	return &awsacmpca.DescribeCertificateAuthorityOutput{CertificateAuthority: detail}, nil
}

func (f *fakeACMPCAAPI) ListTags(
	_ context.Context,
	input *awsacmpca.ListTagsInput,
	_ ...func(*awsacmpca.Options),
) (*awsacmpca.ListTagsOutput, error) {
	f.listTagsCalls++
	arn := aws.ToString(input.CertificateAuthorityArn)
	if f.repeatTagsToken != "" {
		if f.listTagsCalls > fakePaginationCallCap {
			return nil, errRepeatedTokenLoop
		}
		return &awsacmpca.ListTagsOutput{
			Tags:      f.tags[arn],
			NextToken: aws.String(f.repeatTagsToken),
		}, nil
	}
	return &awsacmpca.ListTagsOutput{Tags: f.tags[arn]}, nil
}
