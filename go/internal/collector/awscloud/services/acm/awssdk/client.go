package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	acmservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/acm"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the subset of the AWS SDK ACM client this adapter consumes.
// The interface deliberately excludes GetCertificate (returns the PEM body)
// and ExportCertificate (returns private key material). A dedicated test
// reflects over the interface and FAILS if either method appears here so a
// future SDK refactor cannot quietly broaden the contract.
type apiClient interface {
	ListCertificates(context.Context, *awsacm.ListCertificatesInput, ...func(*awsacm.Options)) (*awsacm.ListCertificatesOutput, error)
	DescribeCertificate(context.Context, *awsacm.DescribeCertificateInput, ...func(*awsacm.Options)) (*awsacm.DescribeCertificateOutput, error)
	ListTagsForCertificate(context.Context, *awsacm.ListTagsForCertificateInput, ...func(*awsacm.Options)) (*awsacm.ListTagsForCertificateOutput, error)
}

// Client adapts AWS SDK ACM control-plane calls into scanner-owned certificate
// metadata. It pages ListCertificates, fans out to DescribeCertificate plus
// ListTagsForCertificate per certificate, and never calls GetCertificate or
// ExportCertificate.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an ACM SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsacm.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListCertificates returns ACM certificate metadata visible to the configured
// AWS credentials. It pages through ListCertificates, enriches each entry with
// DescribeCertificate and ListTagsForCertificate, and surfaces only safe
// metadata. The PEM body and private key material are never read.
func (c *Client) ListCertificates(ctx context.Context) ([]acmservice.Certificate, error) {
	arns, err := c.listCertificateARNs(ctx)
	if err != nil {
		return nil, err
	}
	certificates := make([]acmservice.Certificate, 0, len(arns))
	for _, arn := range arns {
		certificate, err := c.certificateMetadata(ctx, arn)
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, certificate)
	}
	return certificates, nil
}

func (c *Client) listCertificateARNs(ctx context.Context) ([]string, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awsacm.ListCertificatesOutput
		err := c.recordAPICall(ctx, "ListCertificates", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListCertificates(callCtx, &awsacm.ListCertificatesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return arns, nil
		}
		for _, summary := range page.CertificateSummaryList {
			if arn := strings.TrimSpace(aws.ToString(summary.CertificateArn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		if aws.ToString(page.NextToken) == "" {
			return arns, nil
		}
		nextToken = page.NextToken
	}
}

func (c *Client) certificateMetadata(ctx context.Context, arn string) (acmservice.Certificate, error) {
	detail, err := c.describeCertificate(ctx, arn)
	if err != nil {
		return acmservice.Certificate{}, err
	}
	tags, err := c.listCertificateTags(ctx, arn)
	if err != nil {
		return acmservice.Certificate{}, err
	}
	return mapCertificate(arn, detail, tags), nil
}

func (c *Client) describeCertificate(ctx context.Context, arn string) (*acmtypes.CertificateDetail, error) {
	var output *awsacm.DescribeCertificateOutput
	err := c.recordAPICall(ctx, "DescribeCertificate", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeCertificate(callCtx, &awsacm.DescribeCertificateInput{
			CertificateArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.Certificate, nil
}

func (c *Client) listCertificateTags(ctx context.Context, arn string) ([]acmtypes.Tag, error) {
	var output *awsacm.ListTagsForCertificateOutput
	err := c.recordAPICall(ctx, "ListTagsForCertificate", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForCertificate(callCtx, &awsacm.ListTagsForCertificateInput{
			CertificateArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.Tags, nil
}

func mapCertificate(arn string, detail *acmtypes.CertificateDetail, tags []acmtypes.Tag) acmservice.Certificate {
	certificate := acmservice.Certificate{
		ARN:  strings.TrimSpace(arn),
		Tags: tagsToMap(tags),
	}
	if detail == nil {
		return certificate
	}
	certificate.DomainName = strings.TrimSpace(aws.ToString(detail.DomainName))
	certificate.SubjectAlternativeNames = cloneStrings(detail.SubjectAlternativeNames)
	certificate.Status = string(detail.Status)
	certificate.Type = string(detail.Type)
	certificate.Issuer = strings.TrimSpace(aws.ToString(detail.Issuer))
	if detail.NotBefore != nil {
		certificate.NotBefore = detail.NotBefore.UTC()
	}
	if detail.NotAfter != nil {
		certificate.NotAfter = detail.NotAfter.UTC()
	}
	certificate.KeyAlgorithm = string(detail.KeyAlgorithm)
	certificate.SignatureAlgorithm = strings.TrimSpace(aws.ToString(detail.SignatureAlgorithm))
	certificate.InUseBy = cloneStrings(detail.InUseBy)
	return certificate
}

func tagsToMap(tags []acmtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// Compile-time assertions.
var (
	_ acmservice.Client = (*Client)(nil)
	_ apiClient         = (*awsacm.Client)(nil)
)
