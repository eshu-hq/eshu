// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaoss "github.com/aws/aws-sdk-go-v2/service/opensearchserverless"
	awsaosstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"

	aossservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/opensearchserverless"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// batchGetCollectionLimit is the maximum collection ids BatchGetCollection
	// accepts per call.
	batchGetCollectionLimit = 100
	// batchGetVPCEndpointLimit is the maximum endpoint ids BatchGetVpcEndpoint
	// accepts per call.
	batchGetVPCEndpointLimit = 100
)

// encryptionPolicyDocument is the minimal projection of an OpenSearch Serverless
// encryption policy body the adapter parses. Only the collection resource
// patterns and the customer-managed KMS key ARN are read; the rest of the policy
// body is discarded immediately and never persisted.
type encryptionPolicyDocument struct {
	Rules []struct {
		ResourceType string   `json:"ResourceType"`
		Resource     []string `json:"Resource"`
	} `json:"Rules"`
	AWSOwnedKey bool   `json:"AWSOwnedKey"`
	KmsARN      string `json:"KmsARN"`
}

// listSecurityPolicies lists encryption and network security policies and, for
// encryption policies, parses the customer-managed KMS key binding from the
// policy document. The policy document body is never retained: only the
// summary metadata and the parsed key/pattern projection survive.
func (c *Client) listSecurityPolicies(
	ctx context.Context,
) ([]aossservice.SecurityPolicy, []aossservice.EncryptionKeyBinding, error) {
	var policies []aossservice.SecurityPolicy
	var bindings []aossservice.EncryptionKeyBinding
	for _, policyType := range awsaosstypes.SecurityPolicyTypeEncryption.Values() {
		summaries, err := c.listSecurityPoliciesByType(ctx, policyType)
		if err != nil {
			return nil, nil, err
		}
		for _, summary := range summaries {
			policies = append(policies, mapSecurityPolicy(summary, policyType))
		}
		if policyType != awsaosstypes.SecurityPolicyTypeEncryption {
			continue
		}
		for _, summary := range summaries {
			binding, ok, err := c.encryptionBinding(ctx, summary)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				bindings = append(bindings, binding)
			}
		}
	}
	return policies, bindings, nil
}

func (c *Client) listSecurityPoliciesByType(
	ctx context.Context,
	policyType awsaosstypes.SecurityPolicyType,
) ([]awsaosstypes.SecurityPolicySummary, error) {
	var summaries []awsaosstypes.SecurityPolicySummary
	var nextToken *string
	for {
		var page *awsaoss.ListSecurityPoliciesOutput
		err := c.recordAPICall(ctx, "ListSecurityPolicies", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListSecurityPolicies(callCtx, &awsaoss.ListSecurityPoliciesInput{
				Type:      policyType,
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		summaries = append(summaries, page.SecurityPolicySummaries...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	return summaries, nil
}

// encryptionBinding fetches one encryption policy and parses its KMS key and
// collection patterns. It returns ok=false when the policy assigns no
// customer-managed key or matches no collection resource, so AWS-owned-key
// policies produce no collection-to-KMS edge.
func (c *Client) encryptionBinding(
	ctx context.Context,
	summary awsaosstypes.SecurityPolicySummary,
) (aossservice.EncryptionKeyBinding, bool, error) {
	name := strings.TrimSpace(aws.ToString(summary.Name))
	if name == "" {
		return aossservice.EncryptionKeyBinding{}, false, nil
	}
	var output *awsaoss.GetSecurityPolicyOutput
	err := c.recordAPICall(ctx, "GetSecurityPolicy", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetSecurityPolicy(callCtx, &awsaoss.GetSecurityPolicyInput{
			Name: aws.String(name),
			Type: awsaosstypes.SecurityPolicyTypeEncryption,
		})
		return err
	})
	if err != nil {
		return aossservice.EncryptionKeyBinding{}, false, err
	}
	if output == nil || output.SecurityPolicyDetail == nil || output.SecurityPolicyDetail.Policy == nil {
		return aossservice.EncryptionKeyBinding{}, false, nil
	}
	document, err := parseEncryptionPolicy(output.SecurityPolicyDetail.Policy)
	if err != nil {
		return aossservice.EncryptionKeyBinding{}, false, err
	}
	keyARN := strings.TrimSpace(document.KmsARN)
	patterns := collectionPatterns(document)
	if keyARN == "" || len(patterns) == 0 {
		return aossservice.EncryptionKeyBinding{}, false, nil
	}
	return aossservice.EncryptionKeyBinding{
		PolicyName:         name,
		KMSKeyARN:          keyARN,
		CollectionPatterns: patterns,
	}, true, nil
}

// parseEncryptionPolicy reads the KMS key ARN and collection resource patterns
// from a smithy policy document. The raw document is unmarshaled into the minimal
// projection and never returned, so the policy body does not leave this function.
func parseEncryptionPolicy(policy smithyDocument) (encryptionPolicyDocument, error) {
	raw, err := policy.MarshalSmithyDocument()
	if err != nil {
		return encryptionPolicyDocument{}, err
	}
	var document encryptionPolicyDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return encryptionPolicyDocument{}, err
	}
	return document, nil
}

// smithyDocument is the minimal smithy document surface the adapter needs to read
// the encryption policy body: marshal it to JSON so only the projected fields are
// kept.
type smithyDocument interface {
	MarshalSmithyDocument() ([]byte, error)
}

func collectionPatterns(document encryptionPolicyDocument) []string {
	var patterns []string
	for _, rule := range document.Rules {
		if !strings.EqualFold(strings.TrimSpace(rule.ResourceType), "collection") {
			continue
		}
		for _, resource := range rule.Resource {
			if pattern := aossservice.CollectionPatternFromResource(resource); pattern != "" {
				patterns = append(patterns, pattern)
			}
		}
	}
	return patterns
}

func mapSecurityPolicy(
	summary awsaosstypes.SecurityPolicySummary,
	policyType awsaosstypes.SecurityPolicyType,
) aossservice.SecurityPolicy {
	return aossservice.SecurityPolicy{
		Name:             strings.TrimSpace(aws.ToString(summary.Name)),
		Type:             strings.TrimSpace(string(policyType)),
		PolicyVersion:    strings.TrimSpace(aws.ToString(summary.PolicyVersion)),
		Description:      strings.TrimSpace(aws.ToString(summary.Description)),
		CreatedDate:      epochMillis(summary.CreatedDate),
		LastModifiedDate: epochMillis(summary.LastModifiedDate),
	}
}

func (c *Client) mapCollection(
	ctx context.Context,
	detail awsaosstypes.CollectionDetail,
) (aossservice.Collection, error) {
	arn := strings.TrimSpace(aws.ToString(detail.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return aossservice.Collection{}, err
	}
	return aossservice.Collection{
		ARN:              arn,
		ID:               strings.TrimSpace(aws.ToString(detail.Id)),
		Name:             strings.TrimSpace(aws.ToString(detail.Name)),
		Type:             strings.TrimSpace(string(detail.Type)),
		Status:           strings.TrimSpace(string(detail.Status)),
		StandbyReplicas:  strings.TrimSpace(string(detail.StandbyReplicas)),
		KMSKeyARN:        strings.TrimSpace(aws.ToString(detail.KmsKeyArn)),
		CreatedDate:      epochMillis(detail.CreatedDate),
		LastModifiedDate: epochMillis(detail.LastModifiedDate),
		Tags:             tags,
	}, nil
}

func mapVPCEndpoint(detail awsaosstypes.VpcEndpointDetail) aossservice.VPCEndpoint {
	return aossservice.VPCEndpoint{
		ID:               strings.TrimSpace(aws.ToString(detail.Id)),
		Name:             strings.TrimSpace(aws.ToString(detail.Name)),
		Status:           strings.TrimSpace(string(detail.Status)),
		VPCID:            strings.TrimSpace(aws.ToString(detail.VpcId)),
		SubnetIDs:        trimmedStrings(detail.SubnetIds),
		SecurityGroupIDs: trimmedStrings(detail.SecurityGroupIds),
		CreatedDate:      epochMillis(detail.CreatedDate),
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsaoss.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsaoss.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for _, tag := range output.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func (c *Client) recordInstruments(ctx context.Context, operation, result string, throttled bool) {
	if c.instruments == nil {
		return
	}
	c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrService(c.boundary.ServiceKind),
		telemetry.AttrAccount(c.boundary.AccountID),
		telemetry.AttrRegion(c.boundary.Region),
		telemetry.AttrOperation(operation),
		telemetry.AttrResult(result),
	))
	if throttled {
		c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
		))
	}
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

// epochMillis converts an AWS epoch-millisecond timestamp into a UTC time, or the
// zero time when the value is nil so downstream payloads omit unknown timestamps.
func epochMillis(value *int64) time.Time {
	if value == nil || *value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(*value).UTC()
}

func collectionIDs(summaries []awsaosstypes.CollectionSummary) []string {
	var ids []string
	for _, summary := range summaries {
		if id := strings.TrimSpace(aws.ToString(summary.Id)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func trimmedStrings(input []string) []string {
	var output []string
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}

func chunk(values []string, size int) [][]string {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	var batches [][]string
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		batches = append(batches, values[start:end])
	}
	return batches
}
