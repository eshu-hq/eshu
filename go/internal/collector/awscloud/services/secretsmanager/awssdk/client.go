package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	secretsmanagerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/secretsmanager"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const listSecretsLimit int32 = 100

type apiClient interface {
	ListSecrets(
		context.Context,
		*awssecretsmanager.ListSecretsInput,
		...func(*awssecretsmanager.Options),
	) (*awssecretsmanager.ListSecretsOutput, error)
}

// Client adapts AWS SDK Secrets Manager control-plane calls into scanner-owned
// metadata. It never calls GetSecretValue, BatchGetSecretValue,
// ListSecretVersionIds, GetResourcePolicy, or mutation APIs.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Secrets Manager SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssecretsmanager.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListSecrets returns Secrets Manager metadata visible to the configured AWS
// credentials.
func (c *Client) ListSecrets(ctx context.Context) ([]secretsmanagerservice.Secret, error) {
	var secrets []secretsmanagerservice.Secret
	var nextToken *string
	for {
		var page *awssecretsmanager.ListSecretsOutput
		err := c.recordAPICall(ctx, "ListSecrets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListSecrets(callCtx, &awssecretsmanager.ListSecretsInput{
				IncludePlannedDeletion: aws.Bool(true),
				MaxResults:             aws.Int32(listSecretsLimit),
				NextToken:              nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return secrets, nil
		}
		for _, raw := range page.SecretList {
			secrets = append(secrets, mapSecret(raw))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return secrets, nil
		}
	}
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
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
	return err
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

var _ secretsmanagerservice.Client = (*Client)(nil)

var _ apiClient = (*awssecretsmanager.Client)(nil)
