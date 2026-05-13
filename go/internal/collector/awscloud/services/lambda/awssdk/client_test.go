package awssdk

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	awslambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	lambdaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda"
)

func TestMapFunctionPreservesImageURIAndTagsWithoutLocation(t *testing.T) {
	lastModified := "2026-05-13T12:00:00.000+0000"
	function := mapFunction(&awslambda.GetFunctionOutput{
		Code: &awslambdatypes.FunctionCodeLocation{
			ImageUri:         aws.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod"),
			Location:         aws.String("https://lambda-downloads.example/presigned"),
			RepositoryType:   aws.String("ECR"),
			ResolvedImageUri: aws.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api@sha256:abc123"),
			SourceKMSKeyArn:  aws.String("arn:aws:kms:us-east-1:123456789012:key/code"),
		},
		Configuration: &awslambdatypes.FunctionConfiguration{
			Architectures: []awslambdatypes.Architecture{awslambdatypes.ArchitectureArm64},
			CodeSha256:    aws.String("code-sha"),
			CodeSize:      1234,
			Environment: &awslambdatypes.EnvironmentResponse{
				Variables: map[string]string{"DATABASE_URL": "postgres://secret"},
			},
			FunctionArn:      aws.String("arn:aws:lambda:us-east-1:123456789012:function:api"),
			FunctionName:     aws.String("api"),
			Handler:          aws.String("index.handler"),
			KMSKeyArn:        aws.String("arn:aws:kms:us-east-1:123456789012:key/env"),
			LastModified:     aws.String(lastModified),
			LastUpdateStatus: awslambdatypes.LastUpdateStatusSuccessful,
			MemorySize:       aws.Int32(512),
			PackageType:      awslambdatypes.PackageTypeImage,
			Role:             aws.String("arn:aws:iam::123456789012:role/api-lambda"),
			Runtime:          awslambdatypes.RuntimeNodejs20x,
			State:            awslambdatypes.StateActive,
			Timeout:          aws.Int32(30),
			Version:          aws.String("$LATEST"),
			VpcConfig: &awslambdatypes.VpcConfigResponse{
				Ipv6AllowedForDualStack: aws.Bool(true),
				SecurityGroupIds:        []string{"sg-123"},
				SubnetIds:               []string{"subnet-123"},
				VpcId:                   aws.String("vpc-123"),
			},
		},
		Tags: map[string]string{"environment": "prod"},
	})

	if function.ImageURI != "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod" {
		t.Fatalf("ImageURI = %q", function.ImageURI)
	}
	if function.ResolvedImageURI != "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api@sha256:abc123" {
		t.Fatalf("ResolvedImageURI = %q", function.ResolvedImageURI)
	}
	if function.Tags["environment"] != "prod" {
		t.Fatalf("tags = %#v, want environment=prod", function.Tags)
	}
	if function.Environment["DATABASE_URL"] != "postgres://secret" {
		t.Fatalf("environment not preserved for scanner redaction")
	}
	if function.VPCConfig.VPCID != "vpc-123" || len(function.VPCConfig.SubnetIDs) != 1 {
		t.Fatalf("VPCConfig = %#v", function.VPCConfig)
	}
	if function.LastModified.IsZero() {
		t.Fatalf("LastModified was not parsed from %q", lastModified)
	}
}

func TestMapAliasPreservesWeightedRouting(t *testing.T) {
	alias := mapAlias(
		"arn:aws:lambda:us-east-1:123456789012:function:api",
		awslambdatypes.AliasConfiguration{
			AliasArn:        aws.String("arn:aws:lambda:us-east-1:123456789012:function:api:prod"),
			FunctionVersion: aws.String("12"),
			Name:            aws.String("prod"),
			RoutingConfig: &awslambdatypes.AliasRoutingConfiguration{
				AdditionalVersionWeights: map[string]float64{"13": 0.1},
			},
		},
	)

	if alias.FunctionARN != "arn:aws:lambda:us-east-1:123456789012:function:api" {
		t.Fatalf("FunctionARN = %q", alias.FunctionARN)
	}
	if alias.RoutingWeights["13"] != 0.1 {
		t.Fatalf("RoutingWeights = %#v", alias.RoutingWeights)
	}
}

func TestMapEventSourceMappingPreservesFunctionAndSource(t *testing.T) {
	mapping := mapEventSourceMapping(awslambdatypes.EventSourceMappingConfiguration{
		BatchSize:             aws.Int32(10),
		EventSourceArn:        aws.String("arn:aws:sqs:us-east-1:123456789012:api-events"),
		EventSourceMappingArn: aws.String("arn:aws:lambda:us-east-1:123456789012:event-source-mapping:11111111-2222-3333-4444-555555555555"),
		FunctionArn:           aws.String("arn:aws:lambda:us-east-1:123456789012:function:api"),
		LastModified:          aws.Time(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)),
		State:                 aws.String("Enabled"),
		UUID:                  aws.String("11111111-2222-3333-4444-555555555555"),
	})

	if mapping.FunctionARN != "arn:aws:lambda:us-east-1:123456789012:function:api" {
		t.Fatalf("FunctionARN = %q", mapping.FunctionARN)
	}
	if mapping.EventSourceARN != "arn:aws:sqs:us-east-1:123456789012:api-events" {
		t.Fatalf("EventSourceARN = %q", mapping.EventSourceARN)
	}
	if mapping.BatchSize != 10 {
		t.Fatalf("BatchSize = %d, want 10", mapping.BatchSize)
	}
}

func TestListFunctionsSkipsEntriesWithoutIdentifier(t *testing.T) {
	client := &fakeAPIClient{
		functions:            []awslambdatypes.FunctionConfiguration{{}},
		failBlankGetFunction: true,
	}
	adapter := Client{client: client}

	functions, err := adapter.ListFunctions(context.Background())
	if err != nil {
		t.Fatalf("ListFunctions() error = %v", err)
	}
	if len(functions) != 0 {
		t.Fatalf("functions = %#v, want none", functions)
	}
	if client.getFunctionCalls != 0 {
		t.Fatalf("GetFunction calls = %d, want 0", client.getFunctionCalls)
	}
}

func TestListAliasesUsesFunctionNameFallbackAndSkipsMissingIdentifier(t *testing.T) {
	client := &fakeAPIClient{
		aliases: []awslambdatypes.AliasConfiguration{{
			AliasArn: aws.String("arn:aws:lambda:us-east-1:123456789012:function:api:prod"),
			Name:     aws.String("prod"),
		}},
		failBlankListAliases: true,
	}
	adapter := Client{client: client}

	aliases, err := adapter.ListAliases(context.Background(), lambdaserviceFunction("api"))
	if err != nil {
		t.Fatalf("ListAliases() error = %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("alias count = %d, want 1", len(aliases))
	}
	if got := client.aliasInputs[0]; got != "api" {
		t.Fatalf("ListAliases FunctionName = %q, want api", got)
	}

	aliases, err = adapter.ListAliases(context.Background(), lambdaserviceFunction(""))
	if err != nil {
		t.Fatalf("ListAliases(blank) error = %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("blank aliases = %#v, want none", aliases)
	}
}

func TestListEventSourceMappingsUsesFunctionNameFallbackAndSkipsMissingIdentifier(t *testing.T) {
	client := &fakeAPIClient{
		mappings: []awslambdatypes.EventSourceMappingConfiguration{{
			FunctionArn: aws.String("arn:aws:lambda:us-east-1:123456789012:function:api"),
			UUID:        aws.String("mapping-1"),
		}},
		failBlankListMappings: true,
	}
	adapter := Client{client: client}

	mappings, err := adapter.ListEventSourceMappings(context.Background(), lambdaserviceFunction("api"))
	if err != nil {
		t.Fatalf("ListEventSourceMappings() error = %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("mapping count = %d, want 1", len(mappings))
	}
	if got := client.mappingInputs[0]; got != "api" {
		t.Fatalf("ListEventSourceMappings FunctionName = %q, want api", got)
	}

	mappings, err = adapter.ListEventSourceMappings(context.Background(), lambdaserviceFunction(""))
	if err != nil {
		t.Fatalf("ListEventSourceMappings(blank) error = %v", err)
	}
	if len(mappings) != 0 {
		t.Fatalf("blank mappings = %#v, want none", mappings)
	}
}

func lambdaserviceFunction(name string) lambdaservice.Function {
	return lambdaservice.Function{Name: name}
}

type fakeAPIClient struct {
	functions             []awslambdatypes.FunctionConfiguration
	aliases               []awslambdatypes.AliasConfiguration
	mappings              []awslambdatypes.EventSourceMappingConfiguration
	getFunctionCalls      int
	aliasInputs           []string
	mappingInputs         []string
	failBlankGetFunction  bool
	failBlankListAliases  bool
	failBlankListMappings bool
}

func (c *fakeAPIClient) ListFunctions(
	context.Context,
	*awslambda.ListFunctionsInput,
	...func(*awslambda.Options),
) (*awslambda.ListFunctionsOutput, error) {
	return &awslambda.ListFunctionsOutput{Functions: c.functions}, nil
}

func (c *fakeAPIClient) GetFunction(
	_ context.Context,
	input *awslambda.GetFunctionInput,
	_ ...func(*awslambda.Options),
) (*awslambda.GetFunctionOutput, error) {
	c.getFunctionCalls++
	if c.failBlankGetFunction && aws.ToString(input.FunctionName) == "" {
		return nil, fmt.Errorf("blank function name")
	}
	return &awslambda.GetFunctionOutput{Configuration: &awslambdatypes.FunctionConfiguration{
		FunctionArn:  input.FunctionName,
		FunctionName: input.FunctionName,
	}}, nil
}

func (c *fakeAPIClient) ListAliases(
	_ context.Context,
	input *awslambda.ListAliasesInput,
	_ ...func(*awslambda.Options),
) (*awslambda.ListAliasesOutput, error) {
	functionName := aws.ToString(input.FunctionName)
	c.aliasInputs = append(c.aliasInputs, functionName)
	if c.failBlankListAliases && functionName == "" {
		return nil, fmt.Errorf("blank function name")
	}
	return &awslambda.ListAliasesOutput{Aliases: c.aliases}, nil
}

func (c *fakeAPIClient) ListEventSourceMappings(
	_ context.Context,
	input *awslambda.ListEventSourceMappingsInput,
	_ ...func(*awslambda.Options),
) (*awslambda.ListEventSourceMappingsOutput, error) {
	functionName := aws.ToString(input.FunctionName)
	c.mappingInputs = append(c.mappingInputs, functionName)
	if c.failBlankListMappings && functionName == "" {
		return nil, fmt.Errorf("blank function name")
	}
	return &awslambda.ListEventSourceMappingsOutput{EventSourceMappings: c.mappings}, nil
}
