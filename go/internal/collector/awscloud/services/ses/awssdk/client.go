// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssesv2 "github.com/aws/aws-sdk-go-v2/service/sesv2"
	awssesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	sesservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ses"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK SES v2 surface the adapter consumes. It lists
// only the read operations the scanner needs: identity, configuration set,
// event-destination, dedicated-IP-pool, and resource-tag reads. Every send,
// template, suppression-read, contact-read, mutation, and DKIM signing-key API
// is intentionally absent so it is unreachable through this adapter by
// construction. A reflection guard test enforces the contract.
type apiClient interface {
	ListEmailIdentities(
		context.Context,
		*awssesv2.ListEmailIdentitiesInput,
		...func(*awssesv2.Options),
	) (*awssesv2.ListEmailIdentitiesOutput, error)
	GetEmailIdentity(
		context.Context,
		*awssesv2.GetEmailIdentityInput,
		...func(*awssesv2.Options),
	) (*awssesv2.GetEmailIdentityOutput, error)
	ListConfigurationSets(
		context.Context,
		*awssesv2.ListConfigurationSetsInput,
		...func(*awssesv2.Options),
	) (*awssesv2.ListConfigurationSetsOutput, error)
	GetConfigurationSet(
		context.Context,
		*awssesv2.GetConfigurationSetInput,
		...func(*awssesv2.Options),
	) (*awssesv2.GetConfigurationSetOutput, error)
	GetConfigurationSetEventDestinations(
		context.Context,
		*awssesv2.GetConfigurationSetEventDestinationsInput,
		...func(*awssesv2.Options),
	) (*awssesv2.GetConfigurationSetEventDestinationsOutput, error)
	ListDedicatedIpPools(
		context.Context,
		*awssesv2.ListDedicatedIpPoolsInput,
		...func(*awssesv2.Options),
	) (*awssesv2.ListDedicatedIpPoolsOutput, error)
}

// Client adapts the AWS SDK SES v2 read operations into the scanner-owned
// metadata-only Client interface. It lists identities and configuration sets,
// gets their control-plane attributes and event destinations, lists dedicated
// IP pools, and maps each into safe identity, verification, and reference
// metadata. It never sends email, never reads message or template bodies, and
// never persists DKIM private keys, DKIM signing tokens, identity policy
// documents, or SMTP credentials.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an SES v2 SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssesv2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns SES email-identity, configuration-set (with event
// destinations), and dedicated-IP-pool metadata visible to the configured AWS
// credentials. Message bodies, template bodies, DKIM private keys, and signing
// tokens are never read.
func (c *Client) Snapshot(ctx context.Context) (sesservice.Snapshot, error) {
	identities, err := c.listEmailIdentities(ctx)
	if err != nil {
		return sesservice.Snapshot{}, err
	}
	sets, err := c.listConfigurationSets(ctx)
	if err != nil {
		return sesservice.Snapshot{}, err
	}
	pools, err := c.listDedicatedIPPools(ctx)
	if err != nil {
		return sesservice.Snapshot{}, err
	}
	return sesservice.Snapshot{
		EmailIdentities:   identities,
		ConfigurationSets: sets,
		DedicatedIPPools:  pools,
	}, nil
}

func (c *Client) listEmailIdentities(ctx context.Context) ([]sesservice.EmailIdentity, error) {
	var identities []sesservice.EmailIdentity
	var nextToken *string
	for {
		var page *awssesv2.ListEmailIdentitiesOutput
		err := c.recordAPICall(ctx, "ListEmailIdentities", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListEmailIdentities(callCtx, &awssesv2.ListEmailIdentitiesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return identities, nil
		}
		for _, info := range page.EmailIdentities {
			identity, err := c.getEmailIdentity(ctx, info)
			if err != nil {
				return nil, err
			}
			identities = append(identities, identity)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return identities, nil
		}
	}
}

// getEmailIdentity enriches a list-summary identity with its DKIM, MAIL FROM,
// default configuration set, and tag attributes. It returns the list-only
// metadata when GetEmailIdentity omits the detail so the scanner still records
// the identity's presence.
func (c *Client) getEmailIdentity(
	ctx context.Context,
	info awssesv2types.IdentityInfo,
) (sesservice.EmailIdentity, error) {
	identity := mapIdentityInfo(info)
	if identity.Name == "" {
		return identity, nil
	}
	var output *awssesv2.GetEmailIdentityOutput
	err := c.recordAPICall(ctx, "GetEmailIdentity", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetEmailIdentity(callCtx, &awssesv2.GetEmailIdentityInput{
			EmailIdentity: aws.String(identity.Name),
		})
		return err
	})
	if err != nil {
		return sesservice.EmailIdentity{}, err
	}
	if output == nil {
		return identity, nil
	}
	applyIdentityDetail(&identity, output)
	return identity, nil
}

func (c *Client) listConfigurationSets(ctx context.Context) ([]sesservice.ConfigurationSet, error) {
	var names []string
	var nextToken *string
	for {
		var page *awssesv2.ListConfigurationSetsOutput
		err := c.recordAPICall(ctx, "ListConfigurationSets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListConfigurationSets(callCtx, &awssesv2.ListConfigurationSetsInput{
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
		for _, name := range page.ConfigurationSets {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				names = append(names, trimmed)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	sets := make([]sesservice.ConfigurationSet, 0, len(names))
	for _, name := range names {
		set, err := c.getConfigurationSet(ctx, name)
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	return sets, nil
}

// getConfigurationSet reads one configuration set's control-plane options and
// event destinations. It returns a name-only set when GetConfigurationSet omits
// the detail so the scanner still records the set's presence.
func (c *Client) getConfigurationSet(ctx context.Context, name string) (sesservice.ConfigurationSet, error) {
	set := sesservice.ConfigurationSet{Name: strings.TrimSpace(name)}
	var output *awssesv2.GetConfigurationSetOutput
	err := c.recordAPICall(ctx, "GetConfigurationSet", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetConfigurationSet(callCtx, &awssesv2.GetConfigurationSetInput{
			ConfigurationSetName: aws.String(set.Name),
		})
		return err
	})
	if err != nil {
		return sesservice.ConfigurationSet{}, err
	}
	if output != nil {
		applyConfigurationSetDetail(&set, output)
	}
	destinations, err := c.getEventDestinations(ctx, set.Name)
	if err != nil {
		return sesservice.ConfigurationSet{}, err
	}
	set.EventDestinations = destinations
	return set, nil
}

func (c *Client) getEventDestinations(ctx context.Context, setName string) ([]sesservice.EventDestination, error) {
	setName = strings.TrimSpace(setName)
	if setName == "" {
		return nil, nil
	}
	var output *awssesv2.GetConfigurationSetEventDestinationsOutput
	err := c.recordAPICall(ctx, "GetConfigurationSetEventDestinations", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetConfigurationSetEventDestinations(
			callCtx,
			&awssesv2.GetConfigurationSetEventDestinationsInput{
				ConfigurationSetName: aws.String(setName),
			},
		)
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	destinations := make([]sesservice.EventDestination, 0, len(output.EventDestinations))
	for _, destination := range output.EventDestinations {
		destinations = append(destinations, mapEventDestination(destination))
	}
	if len(destinations) == 0 {
		return nil, nil
	}
	return destinations, nil
}

func (c *Client) listDedicatedIPPools(ctx context.Context) ([]sesservice.DedicatedIPPool, error) {
	var pools []sesservice.DedicatedIPPool
	var nextToken *string
	for {
		var page *awssesv2.ListDedicatedIpPoolsOutput
		err := c.recordAPICall(ctx, "ListDedicatedIpPools", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDedicatedIpPools(callCtx, &awssesv2.ListDedicatedIpPoolsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return pools, nil
		}
		for _, name := range page.DedicatedIpPools {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				pools = append(pools, sesservice.DedicatedIPPool{Name: trimmed})
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return pools, nil
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

// isThrottleError reports whether err is an AWS throttle/rate-limit error so the
// adapter records it on the throttle counter without retrying here.
func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException" ||
		code == "LimitExceededException"
}

var _ sesservice.Client = (*Client)(nil)

var _ apiClient = (*awssesv2.Client)(nil)
