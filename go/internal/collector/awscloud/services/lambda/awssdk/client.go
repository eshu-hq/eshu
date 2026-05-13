package awssdk

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	awslambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	lambdaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	GetFunction(context.Context, *awslambda.GetFunctionInput, ...func(*awslambda.Options)) (*awslambda.GetFunctionOutput, error)
	awslambda.ListAliasesAPIClient
	awslambda.ListEventSourceMappingsAPIClient
	awslambda.ListFunctionsAPIClient
}

// Client adapts AWS SDK Lambda pagination into scanner-owned Lambda records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Lambda SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awslambda.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListFunctions returns all Lambda functions visible to the configured AWS
// credentials. It calls GetFunction per listed function to capture tags and
// image URI evidence that ListFunctions does not fully return.
func (c *Client) ListFunctions(ctx context.Context) ([]lambdaservice.Function, error) {
	paginator := awslambda.NewListFunctionsPaginator(c.client, &awslambda.ListFunctionsInput{})
	var listed []awslambdatypes.FunctionConfiguration
	for paginator.HasMorePages() {
		var page *awslambda.ListFunctionsOutput
		err := c.recordAPICall(ctx, "ListFunctions", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		listed = append(listed, page.Functions...)
	}
	functions := make([]lambdaservice.Function, 0, len(listed))
	for _, configuration := range listed {
		functionName := firstNonEmpty(aws.ToString(configuration.FunctionArn), aws.ToString(configuration.FunctionName))
		if functionName == "" {
			continue
		}
		var output *awslambda.GetFunctionOutput
		err := c.recordAPICall(ctx, "GetFunction", func(callCtx context.Context) error {
			var err error
			output, err = c.client.GetFunction(callCtx, &awslambda.GetFunctionInput{
				FunctionName: aws.String(functionName),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			output = &awslambda.GetFunctionOutput{Configuration: &configuration}
		}
		if output.Configuration == nil {
			output.Configuration = &configuration
		}
		functions = append(functions, mapFunction(output))
	}
	return functions, nil
}

// ListAliases returns all aliases for one Lambda function.
func (c *Client) ListAliases(
	ctx context.Context,
	function lambdaservice.Function,
) ([]lambdaservice.Alias, error) {
	functionIdentifier := firstNonEmpty(function.ARN, function.Name)
	if functionIdentifier == "" {
		return nil, nil
	}
	paginator := awslambda.NewListAliasesPaginator(c.client, &awslambda.ListAliasesInput{
		FunctionName: aws.String(functionIdentifier),
	})
	var aliases []lambdaservice.Alias
	for paginator.HasMorePages() {
		var page *awslambda.ListAliasesOutput
		err := c.recordAPICall(ctx, "ListAliases", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, alias := range page.Aliases {
			aliases = append(aliases, mapAlias(functionIdentifier, alias))
		}
	}
	return aliases, nil
}

// ListEventSourceMappings returns all event source mappings for one Lambda
// function.
func (c *Client) ListEventSourceMappings(
	ctx context.Context,
	function lambdaservice.Function,
) ([]lambdaservice.EventSourceMapping, error) {
	functionIdentifier := firstNonEmpty(function.ARN, function.Name)
	if functionIdentifier == "" {
		return nil, nil
	}
	paginator := awslambda.NewListEventSourceMappingsPaginator(
		c.client,
		&awslambda.ListEventSourceMappingsInput{FunctionName: aws.String(functionIdentifier)},
	)
	var mappings []lambdaservice.EventSourceMapping
	for paginator.HasMorePages() {
		var page *awslambda.ListEventSourceMappingsOutput
		err := c.recordAPICall(ctx, "ListEventSourceMappings", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, mapping := range page.EventSourceMappings {
			mappings = append(mappings, mapEventSourceMapping(mapping))
		}
	}
	return mappings, nil
}

func mapFunction(output *awslambda.GetFunctionOutput) lambdaservice.Function {
	if output == nil {
		return lambdaservice.Function{}
	}
	configuration := output.Configuration
	if configuration == nil {
		configuration = &awslambdatypes.FunctionConfiguration{}
	}
	code := output.Code
	if code == nil {
		code = &awslambdatypes.FunctionCodeLocation{}
	}
	return lambdaservice.Function{
		ARN:              aws.ToString(configuration.FunctionArn),
		Name:             aws.ToString(configuration.FunctionName),
		Runtime:          string(configuration.Runtime),
		RoleARN:          aws.ToString(configuration.Role),
		Handler:          aws.ToString(configuration.Handler),
		Description:      aws.ToString(configuration.Description),
		State:            string(configuration.State),
		LastUpdateStatus: string(configuration.LastUpdateStatus),
		PackageType:      string(configuration.PackageType),
		Version:          aws.ToString(configuration.Version),
		CodeSHA256:       aws.ToString(configuration.CodeSha256),
		CodeSize:         configuration.CodeSize,
		ImageURI:         aws.ToString(code.ImageUri),
		ResolvedImageURI: aws.ToString(code.ResolvedImageUri),
		KMSKeyARN:        aws.ToString(configuration.KMSKeyArn),
		SourceKMSKeyARN:  aws.ToString(code.SourceKMSKeyArn),
		MemorySize:       aws.ToInt32(configuration.MemorySize),
		TimeoutSeconds:   aws.ToInt32(configuration.Timeout),
		LastModified:     parseLambdaTime(aws.ToString(configuration.LastModified)),
		Architectures:    architectureStrings(configuration.Architectures),
		Environment:      environmentVariables(configuration.Environment),
		VPCConfig:        mapVPCConfig(configuration.VpcConfig),
		LoggingConfig:    mapLoggingConfig(configuration.LoggingConfig),
		Tags:             cloneStringMap(output.Tags),
	}
}

func mapAlias(functionARN string, alias awslambdatypes.AliasConfiguration) lambdaservice.Alias {
	return lambdaservice.Alias{
		ARN:             aws.ToString(alias.AliasArn),
		Name:            aws.ToString(alias.Name),
		FunctionARN:     strings.TrimSpace(functionARN),
		FunctionVersion: aws.ToString(alias.FunctionVersion),
		Description:     aws.ToString(alias.Description),
		RevisionID:      aws.ToString(alias.RevisionId),
		RoutingWeights:  routingWeights(alias.RoutingConfig),
	}
}

func mapEventSourceMapping(mapping awslambdatypes.EventSourceMappingConfiguration) lambdaservice.EventSourceMapping {
	return lambdaservice.EventSourceMapping{
		ARN:                   aws.ToString(mapping.EventSourceMappingArn),
		UUID:                  aws.ToString(mapping.UUID),
		FunctionARN:           aws.ToString(mapping.FunctionArn),
		EventSourceARN:        aws.ToString(mapping.EventSourceArn),
		State:                 aws.ToString(mapping.State),
		LastProcessingResult:  aws.ToString(mapping.LastProcessingResult),
		StartingPosition:      string(mapping.StartingPosition),
		BatchSize:             aws.ToInt32(mapping.BatchSize),
		MaximumRetryAttempts:  aws.ToInt32(mapping.MaximumRetryAttempts),
		ParallelizationFactor: aws.ToInt32(mapping.ParallelizationFactor),
	}
}

func environmentVariables(environment *awslambdatypes.EnvironmentResponse) map[string]string {
	if environment == nil || len(environment.Variables) == 0 {
		return nil
	}
	return cloneStringMap(environment.Variables)
}

func mapVPCConfig(config *awslambdatypes.VpcConfigResponse) lambdaservice.VPCConfig {
	if config == nil {
		return lambdaservice.VPCConfig{}
	}
	return lambdaservice.VPCConfig{
		VPCID:            aws.ToString(config.VpcId),
		SubnetIDs:        cloneStrings(config.SubnetIds),
		SecurityGroupIDs: cloneStrings(config.SecurityGroupIds),
		IPv6AllowedForDS: aws.ToBool(config.Ipv6AllowedForDualStack),
	}
}

func mapLoggingConfig(config *awslambdatypes.LoggingConfig) lambdaservice.LoggingConfig {
	if config == nil {
		return lambdaservice.LoggingConfig{}
	}
	return lambdaservice.LoggingConfig{
		LogGroup:            aws.ToString(config.LogGroup),
		LogFormat:           string(config.LogFormat),
		ApplicationLogLevel: string(config.ApplicationLogLevel),
		SystemLogLevel:      string(config.SystemLogLevel),
	}
}

func architectureStrings(input []awslambdatypes.Architecture) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if text := strings.TrimSpace(string(value)); text != "" {
			output = append(output, text)
		}
	}
	return output
}

func routingWeights(config *awslambdatypes.AliasRoutingConfiguration) map[string]float64 {
	if config == nil || len(config.AdditionalVersionWeights) == 0 {
		return nil
	}
	output := make(map[string]float64, len(config.AdditionalVersionWeights))
	for version, weight := range config.AdditionalVersionWeights {
		if trimmed := strings.TrimSpace(version); trimmed != "" {
			output[trimmed] = weight
		}
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseLambdaTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.000-0700",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
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

var _ lambdaservice.Client = (*Client)(nil)

var _ apiClient = (*awslambda.Client)(nil)
