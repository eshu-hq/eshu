// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// TestAPIClientInterfaceExcludesGetCertificateAndExportCertificate is the
// security gate for the ACM SDK adapter. The scanner contract forbids
// GetCertificate (returns the PEM body) and ExportCertificate (returns the
// private key). This test reflects over the adapter's internal apiClient
// interface and FAILS if a future SDK refactor adds either method.
func TestAPIClientInterfaceExcludesGetCertificateAndExportCertificate(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{"GetCertificate", "ExportCertificate"}
	for _, name := range forbidden {
		if _, ok := apiClientType.MethodByName(name); ok {
			t.Fatalf("apiClient interface exposes %q; ACM scanner forbids this API", name)
		}
	}
}

func TestClientListCertificatesReadsOnlyDescribeAndTagsAndOmitsCertificateBody(t *testing.T) {
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/abc"
	api := &fakeACMAPI{
		listPages: []*awsacm.ListCertificatesOutput{{
			CertificateSummaryList: []acmtypes.CertificateSummary{{
				CertificateArn: awsv2.String(certificateARN),
				DomainName:     awsv2.String("example.com"),
			}},
		}},
		descriptions: map[string]*acmtypes.CertificateDetail{
			certificateARN: {
				CertificateArn:          awsv2.String(certificateARN),
				DomainName:              awsv2.String("example.com"),
				SubjectAlternativeNames: []string{"example.com", "www.example.com"},
				Status:                  acmtypes.CertificateStatusIssued,
				Type:                    acmtypes.CertificateTypeAmazonIssued,
				Issuer:                  awsv2.String("Amazon"),
				NotBefore:               &notBefore,
				NotAfter:                &notAfter,
				KeyAlgorithm:            acmtypes.KeyAlgorithmRsa2048,
				SignatureAlgorithm:      awsv2.String("SHA256WITHRSA"),
				InUseBy: []string{
					"arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc",
				},
			},
		},
		tags: map[string][]acmtypes.Tag{
			certificateARN: {
				{Key: awsv2.String("Environment"), Value: awsv2.String("prod")},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceACM},
	}

	certificates, err := adapter.ListCertificates(context.Background())
	if err != nil {
		t.Fatalf("ListCertificates() error = %v, want nil", err)
	}
	if got, want := len(certificates), 1; got != want {
		t.Fatalf("len(certificates) = %d, want %d", got, want)
	}
	certificate := certificates[0]
	if certificate.ARN != certificateARN {
		t.Fatalf("ARN = %q, want %q", certificate.ARN, certificateARN)
	}
	if certificate.DomainName != "example.com" {
		t.Fatalf("DomainName = %q", certificate.DomainName)
	}
	if !equalStrings(certificate.SubjectAlternativeNames, []string{"example.com", "www.example.com"}) {
		t.Fatalf("SubjectAlternativeNames = %#v", certificate.SubjectAlternativeNames)
	}
	if certificate.Status != string(acmtypes.CertificateStatusIssued) {
		t.Fatalf("Status = %q, want ISSUED", certificate.Status)
	}
	if certificate.Type != string(acmtypes.CertificateTypeAmazonIssued) {
		t.Fatalf("Type = %q, want AMAZON_ISSUED", certificate.Type)
	}
	if certificate.Issuer != "Amazon" {
		t.Fatalf("Issuer = %q", certificate.Issuer)
	}
	if !certificate.NotBefore.Equal(notBefore) {
		t.Fatalf("NotBefore = %v, want %v", certificate.NotBefore, notBefore)
	}
	if !certificate.NotAfter.Equal(notAfter) {
		t.Fatalf("NotAfter = %v, want %v", certificate.NotAfter, notAfter)
	}
	if certificate.KeyAlgorithm != string(acmtypes.KeyAlgorithmRsa2048) {
		t.Fatalf("KeyAlgorithm = %q", certificate.KeyAlgorithm)
	}
	if certificate.SignatureAlgorithm != "SHA256WITHRSA" {
		t.Fatalf("SignatureAlgorithm = %q", certificate.SignatureAlgorithm)
	}
	if certificate.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v", certificate.Tags)
	}
	if api.getCertificateCalls > 0 {
		t.Fatalf("GetCertificate called %d times; metadata-only adapter must not call GetCertificate", api.getCertificateCalls)
	}
	if api.exportCertificateCalls > 0 {
		t.Fatalf("ExportCertificate called %d times; metadata-only adapter must not call ExportCertificate", api.exportCertificateCalls)
	}
	if api.describeCalls == 0 {
		t.Fatalf("DescribeCertificate not called; adapter must enrich with metadata")
	}
	if api.listTagsCalls == 0 {
		t.Fatalf("ListTagsForCertificate not called; adapter must read tags")
	}
}

func TestClientListCertificatesIncludesACMPCAIssuedCertificatesWithoutCallingPCAAPIs(t *testing.T) {
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/private"
	api := &fakeACMAPI{
		listPages: []*awsacm.ListCertificatesOutput{{
			CertificateSummaryList: []acmtypes.CertificateSummary{{
				CertificateArn: awsv2.String(certificateARN),
				DomainName:     awsv2.String("internal.example"),
			}},
		}},
		descriptions: map[string]*acmtypes.CertificateDetail{
			certificateARN: {
				CertificateArn: awsv2.String(certificateARN),
				DomainName:     awsv2.String("internal.example"),
				Status:         acmtypes.CertificateStatusIssued,
				Type:           acmtypes.CertificateTypePrivate,
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceACM},
	}

	certificates, err := adapter.ListCertificates(context.Background())
	if err != nil {
		t.Fatalf("ListCertificates() error = %v, want nil", err)
	}
	if got, want := len(certificates), 1; got != want {
		t.Fatalf("len(certificates) = %d, want %d", got, want)
	}
	if certificates[0].Type != string(acmtypes.CertificateTypePrivate) {
		t.Fatalf("Type = %q, want PRIVATE", certificates[0].Type)
	}
}

func TestClientListCertificatesPaginates(t *testing.T) {
	api := &fakeACMAPI{
		listPages: []*awsacm.ListCertificatesOutput{
			{
				CertificateSummaryList: []acmtypes.CertificateSummary{{
					CertificateArn: awsv2.String("arn:aws:acm:us-east-1:123456789012:certificate/a"),
					DomainName:     awsv2.String("a.example"),
				}},
				NextToken: awsv2.String("token-1"),
			},
			{
				CertificateSummaryList: []acmtypes.CertificateSummary{{
					CertificateArn: awsv2.String("arn:aws:acm:us-east-1:123456789012:certificate/b"),
					DomainName:     awsv2.String("b.example"),
				}},
			},
		},
		descriptions: map[string]*acmtypes.CertificateDetail{
			"arn:aws:acm:us-east-1:123456789012:certificate/a": {
				CertificateArn: awsv2.String("arn:aws:acm:us-east-1:123456789012:certificate/a"),
				Status:         acmtypes.CertificateStatusIssued,
			},
			"arn:aws:acm:us-east-1:123456789012:certificate/b": {
				CertificateArn: awsv2.String("arn:aws:acm:us-east-1:123456789012:certificate/b"),
				Status:         acmtypes.CertificateStatusIssued,
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceACM},
	}

	certificates, err := adapter.ListCertificates(context.Background())
	if err != nil {
		t.Fatalf("ListCertificates() error = %v, want nil", err)
	}
	if got, want := len(certificates), 2; got != want {
		t.Fatalf("len(certificates) = %d, want %d", got, want)
	}
	if got, want := api.listCalls, 2; got != want {
		t.Fatalf("ListCertificates calls = %d, want %d", got, want)
	}
}

type fakeACMAPI struct {
	listPages              []*awsacm.ListCertificatesOutput
	listCalls              int
	describeCalls          int
	listTagsCalls          int
	getCertificateCalls    int
	exportCertificateCalls int
	descriptions           map[string]*acmtypes.CertificateDetail
	tags                   map[string][]acmtypes.Tag
}

func (f *fakeACMAPI) ListCertificates(
	_ context.Context,
	_ *awsacm.ListCertificatesInput,
	_ ...func(*awsacm.Options),
) (*awsacm.ListCertificatesOutput, error) {
	if f.listCalls >= len(f.listPages) {
		return &awsacm.ListCertificatesOutput{}, nil
	}
	page := f.listPages[f.listCalls]
	f.listCalls++
	return page, nil
}

func (f *fakeACMAPI) DescribeCertificate(
	_ context.Context,
	input *awsacm.DescribeCertificateInput,
	_ ...func(*awsacm.Options),
) (*awsacm.DescribeCertificateOutput, error) {
	f.describeCalls++
	arn := awsv2.ToString(input.CertificateArn)
	detail, ok := f.descriptions[arn]
	if !ok {
		return &awsacm.DescribeCertificateOutput{}, nil
	}
	return &awsacm.DescribeCertificateOutput{Certificate: detail}, nil
}

func (f *fakeACMAPI) ListTagsForCertificate(
	_ context.Context,
	input *awsacm.ListTagsForCertificateInput,
	_ ...func(*awsacm.Options),
) (*awsacm.ListTagsForCertificateOutput, error) {
	f.listTagsCalls++
	arn := awsv2.ToString(input.CertificateArn)
	return &awsacm.ListTagsForCertificateOutput{Tags: f.tags[arn]}, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}
