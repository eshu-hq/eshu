package awssdk

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	awslambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
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
