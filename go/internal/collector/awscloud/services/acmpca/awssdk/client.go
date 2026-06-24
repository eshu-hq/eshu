// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsacmpca "github.com/aws/aws-sdk-go-v2/service/acmpca"
	acmpcatypes "github.com/aws/aws-sdk-go-v2/service/acmpca/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	acmpcaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/acmpca"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the subset of the AWS SDK ACM Private CA client this adapter
// consumes. The interface deliberately excludes IssueCertificate,
// GetCertificate, GetCertificateAuthorityCsr (CSR body),
// GetCertificateAuthorityCertificate (certificate chain body),
// RevokeCertificate, and every Create/Delete/Update/Restore/Import lifecycle
// API. A dedicated test reflects over the interface and FAILS if any forbidden
// method appears so a future SDK refactor cannot quietly broaden the contract.
type apiClient interface {
	ListCertificateAuthorities(context.Context, *awsacmpca.ListCertificateAuthoritiesInput, ...func(*awsacmpca.Options)) (*awsacmpca.ListCertificateAuthoritiesOutput, error)
	DescribeCertificateAuthority(context.Context, *awsacmpca.DescribeCertificateAuthorityInput, ...func(*awsacmpca.Options)) (*awsacmpca.DescribeCertificateAuthorityOutput, error)
	ListTags(context.Context, *awsacmpca.ListTagsInput, ...func(*awsacmpca.Options)) (*awsacmpca.ListTagsOutput, error)
}

// Client adapts AWS SDK ACM Private CA control-plane calls into scanner-owned
// certificate authority metadata. It pages ListCertificateAuthorities, fans out
// to DescribeCertificateAuthority plus ListTags per authority, and never issues
// or exports certificates, reads the CSR body, or reads the certificate chain
// body.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an ACM Private CA SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsacmpca.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListCertificateAuthorities returns ACM Private CA metadata visible to the
// configured AWS credentials. It pages through ListCertificateAuthorities,
// enriches each entry with DescribeCertificateAuthority and ListTags, and
// surfaces only safe metadata. The certificate chain body, CSR, and private key
// material are never read.
func (c *Client) ListCertificateAuthorities(ctx context.Context) ([]acmpcaservice.CertificateAuthority, error) {
	arns, err := c.listCertificateAuthorityARNs(ctx)
	if err != nil {
		return nil, err
	}
	authorities := make([]acmpcaservice.CertificateAuthority, 0, len(arns))
	for _, arn := range arns {
		authority, err := c.certificateAuthorityMetadata(ctx, arn)
		if err != nil {
			return nil, err
		}
		authorities = append(authorities, authority)
	}
	return authorities, nil
}

func (c *Client) listCertificateAuthorityARNs(ctx context.Context) ([]string, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awsacmpca.ListCertificateAuthoritiesOutput
		err := c.recordAPICall(ctx, "ListCertificateAuthorities", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListCertificateAuthorities(callCtx, &awsacmpca.ListCertificateAuthoritiesInput{
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
		for _, summary := range page.CertificateAuthorities {
			if arn := strings.TrimSpace(aws.ToString(summary.Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return arns, nil
		}
		nextToken = page.NextToken
	}
}

func (c *Client) certificateAuthorityMetadata(ctx context.Context, arn string) (acmpcaservice.CertificateAuthority, error) {
	detail, err := c.describeCertificateAuthority(ctx, arn)
	if err != nil {
		return acmpcaservice.CertificateAuthority{}, err
	}
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return acmpcaservice.CertificateAuthority{}, err
	}
	return mapCertificateAuthority(arn, detail, tags), nil
}

func (c *Client) describeCertificateAuthority(ctx context.Context, arn string) (*acmpcatypes.CertificateAuthority, error) {
	var output *awsacmpca.DescribeCertificateAuthorityOutput
	err := c.recordAPICall(ctx, "DescribeCertificateAuthority", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeCertificateAuthority(callCtx, &awsacmpca.DescribeCertificateAuthorityInput{
			CertificateAuthorityArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.CertificateAuthority, nil
}

func (c *Client) listTags(ctx context.Context, arn string) ([]acmpcatypes.Tag, error) {
	var tags []acmpcatypes.Tag
	var nextToken *string
	for {
		var output *awsacmpca.ListTagsOutput
		err := c.recordAPICall(ctx, "ListTags", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTags(callCtx, &awsacmpca.ListTagsInput{
				CertificateAuthorityArn: aws.String(arn),
				NextToken:               nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return tags, nil
		}
		tags = append(tags, output.Tags...)
		if !hasNextPage(nextToken, output.NextToken) {
			return tags, nil
		}
		nextToken = output.NextToken
	}
}

// hasNextPage reports whether a paginated response advanced to a new token,
// guarding against a service that echoes the same non-empty token forever. An
// empty next token, or one equal to the token just sent, ends pagination.
func hasNextPage(previous, next *string) bool {
	token := strings.TrimSpace(aws.ToString(next))
	if token == "" {
		return false
	}
	return token != strings.TrimSpace(aws.ToString(previous))
}

func mapCertificateAuthority(arn string, detail *acmpcatypes.CertificateAuthority, tags []acmpcatypes.Tag) acmpcaservice.CertificateAuthority {
	authority := acmpcaservice.CertificateAuthority{
		ARN:  strings.TrimSpace(arn),
		Tags: tagsToMap(tags),
	}
	if detail == nil {
		return authority
	}
	authority.OwnerAccount = strings.TrimSpace(aws.ToString(detail.OwnerAccount))
	authority.Type = string(detail.Type)
	authority.Status = string(detail.Status)
	authority.Serial = strings.TrimSpace(aws.ToString(detail.Serial))
	authority.FailureReason = string(detail.FailureReason)
	authority.UsageMode = string(detail.UsageMode)
	authority.KeyStorageSecurityStandard = string(detail.KeyStorageSecurityStandard)
	authority.CreatedAt = timeOrZero(detail.CreatedAt)
	authority.LastStateChangeAt = timeOrZero(detail.LastStateChangeAt)
	authority.NotBefore = timeOrZero(detail.NotBefore)
	authority.NotAfter = timeOrZero(detail.NotAfter)
	if config := detail.CertificateAuthorityConfiguration; config != nil {
		authority.KeyAlgorithm = string(config.KeyAlgorithm)
		authority.SigningAlgorithm = string(config.SigningAlgorithm)
		if config.Subject != nil {
			authority.SubjectCommonName = strings.TrimSpace(aws.ToString(config.Subject.CommonName))
		}
	}
	applyRevocationConfiguration(&authority, detail.RevocationConfiguration)
	return authority
}

// applyRevocationConfiguration copies the non-secret CRL and OCSP revocation
// metadata onto the scanner-owned record. Only the CRL S3 bucket name is
// carried; the CRL/OCSP custom CNAME, custom path, and object ACL are operator
// configuration the scanner does not need for inventory or relationships.
func applyRevocationConfiguration(authority *acmpcaservice.CertificateAuthority, revocation *acmpcatypes.RevocationConfiguration) {
	if revocation == nil {
		return
	}
	if crl := revocation.CrlConfiguration; crl != nil {
		authority.CRLEnabled = aws.ToBool(crl.Enabled)
		authority.CRLS3BucketName = strings.TrimSpace(aws.ToString(crl.S3BucketName))
	}
	if ocsp := revocation.OcspConfiguration; ocsp != nil {
		authority.OCSPEnabled = aws.ToBool(ocsp.Enabled)
	}
}

func tagsToMap(tags []acmpcatypes.Tag) map[string]string {
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

func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

// Compile-time assertions.
var (
	_ acmpcaservice.Client = (*Client)(nil)
	_ apiClient            = (*awsacmpca.Client)(nil)
)
