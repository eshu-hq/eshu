package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awsiamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	eksservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks"
)

type oidcProviderRecord struct {
	ARN         string
	IssuerURL   string
	Thumbprints []string
	ClientIDs   []string
}

func (c *Client) listOIDCProviderRecords(ctx context.Context) ([]oidcProviderRecord, error) {
	if c.oidcProvidersLoaded {
		return c.oidcProviders, nil
	}
	var output *awsiam.ListOpenIDConnectProvidersOutput
	err := c.recordAPICall(ctx, "ListOpenIDConnectProviders", func(callCtx context.Context) error {
		var err error
		output, err = c.iamClient.ListOpenIDConnectProviders(callCtx, &awsiam.ListOpenIDConnectProvidersInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	var records []oidcProviderRecord
	if output != nil {
		for _, provider := range output.OpenIDConnectProviderList {
			record, err := c.getOIDCProviderRecord(ctx, provider)
			if err != nil {
				return nil, err
			}
			if record.ARN != "" || record.IssuerURL != "" {
				records = append(records, record)
			}
		}
	}
	c.oidcProviders = records
	c.oidcProvidersLoaded = true
	return c.oidcProviders, nil
}

func (c *Client) getOIDCProviderRecord(
	ctx context.Context,
	provider awsiamtypes.OpenIDConnectProviderListEntry,
) (oidcProviderRecord, error) {
	arn := strings.TrimSpace(aws.ToString(provider.Arn))
	if arn == "" {
		return oidcProviderRecord{}, nil
	}
	var output *awsiam.GetOpenIDConnectProviderOutput
	err := c.recordAPICall(ctx, "GetOpenIDConnectProvider", func(callCtx context.Context) error {
		var err error
		output, err = c.iamClient.GetOpenIDConnectProvider(callCtx, &awsiam.GetOpenIDConnectProviderInput{
			OpenIDConnectProviderArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return oidcProviderRecord{}, fmt.Errorf("get IAM OIDC provider %q: %w", arn, err)
	}
	if output == nil {
		return oidcProviderRecord{ARN: arn, IssuerURL: oidcProviderURLFromARN(arn)}, nil
	}
	return oidcProviderRecord{
		ARN:         arn,
		ClientIDs:   cloneStrings(output.ClientIDList),
		IssuerURL:   firstNonEmpty(aws.ToString(output.Url), oidcProviderURLFromARN(arn)),
		Thumbprints: cloneStrings(output.ThumbprintList),
	}, nil
}

func matchOIDCProvider(issuerURL string, providers []oidcProviderRecord) (eksservice.OIDCProvider, bool) {
	normalizedIssuer := normalizeOIDCIssuer(issuerURL)
	if normalizedIssuer == "" {
		return eksservice.OIDCProvider{}, false
	}
	for _, provider := range providers {
		if normalizeOIDCIssuer(provider.IssuerURL) != normalizedIssuer &&
			normalizeOIDCIssuer(oidcProviderURLFromARN(provider.ARN)) != normalizedIssuer {
			continue
		}
		return eksservice.OIDCProvider{
			ARN:         strings.TrimSpace(provider.ARN),
			ClientIDs:   cloneStrings(provider.ClientIDs),
			IssuerURL:   firstNonEmpty(issuerURL, provider.IssuerURL),
			Thumbprints: cloneStrings(provider.Thumbprints),
		}, true
	}
	return eksservice.OIDCProvider{}, false
}

func normalizeOIDCIssuer(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	return strings.Trim(value, "/")
}

func oidcProviderURLFromARN(arn string) string {
	const marker = ":oidc-provider/"
	index := strings.LastIndex(arn, marker)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(arn[index+len(marker):])
}
