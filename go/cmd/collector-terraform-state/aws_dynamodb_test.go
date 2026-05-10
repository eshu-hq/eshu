package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestAWSDynamoDBLockMetadataClientReadsOnlyDigest(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 15, 0, 0, 0, time.UTC)
	fake := &fakeDynamoDBGetItemClient{
		output: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"Digest": &types.AttributeValueMemberS{Value: "digest-123"},
				"Info":   &types.AttributeValueMemberS{Value: `{"Who":"operator"}`},
			},
		},
	}
	client := &awsDynamoDBLockMetadataClient{
		clients: map[string]dynamoDBGetItemAPI{"us-east-1": fake},
		now:     func() time.Time { return observedAt },
	}

	metadata, err := client.ReadLockMetadata(context.Background(), terraformstate.LockMetadataInput{
		TableName: "tfstate-locks",
		LockID:    "tfstate-prod/services/api/terraform.tfstate-md5",
		Region:    "us-east-1",
	})
	if err != nil {
		t.Fatalf("ReadLockMetadata() error = %v, want nil", err)
	}

	if got, want := awsv2.ToString(fake.input.TableName), "tfstate-locks"; got != want {
		t.Fatalf("TableName = %q, want %q", got, want)
	}
	lockID, ok := fake.input.Key["LockID"].(*types.AttributeValueMemberS)
	if !ok {
		t.Fatalf("LockID key type = %T, want string attribute", fake.input.Key["LockID"])
	}
	if got, want := lockID.Value, "tfstate-prod/services/api/terraform.tfstate-md5"; got != want {
		t.Fatalf("LockID = %q, want %q", got, want)
	}
	if got, want := awsv2.ToString(fake.input.ProjectionExpression), "#D"; got != want {
		t.Fatalf("ProjectionExpression = %q, want %q", got, want)
	}
	if got, want := fake.input.ExpressionAttributeNames["#D"], "Digest"; got != want {
		t.Fatalf("ExpressionAttributeNames[#D] = %q, want %q", got, want)
	}
	if fake.input.ConsistentRead == nil || !*fake.input.ConsistentRead {
		t.Fatal("ConsistentRead = false, want true")
	}
	if got, want := metadata.Digest, "digest-123"; got != want {
		t.Fatalf("Digest = %q, want %q", got, want)
	}
	if got := metadata.ObservedAt; !got.Equal(observedAt) {
		t.Fatalf("ObservedAt = %v, want %v", got, observedAt)
	}
}

func TestSafeDynamoDBGetItemErrorDoesNotLeakLockConfig(t *testing.T) {
	t.Parallel()

	raw := errors.New("GetItem table tfstate-locks lock tfstate-prod/services/api/terraform.tfstate-md5 request id abc-123 failed")
	err := safeDynamoDBGetItemError(raw)
	if err == nil {
		t.Fatal("safeDynamoDBGetItemError() = nil, want error")
	}
	for _, leaked := range []string{
		"tfstate-locks",
		"tfstate-prod/services/api/terraform.tfstate-md5",
		"abc-123",
	} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("safe error = %q, leaked %q", err.Error(), leaked)
		}
	}
}

func TestSafeDynamoDBGetItemErrorPreservesContextCancellation(t *testing.T) {
	t.Parallel()

	if !errors.Is(safeDynamoDBGetItemError(context.Canceled), context.Canceled) {
		t.Fatal("safeDynamoDBGetItemError(context.Canceled) does not preserve cancellation")
	}
}

func TestSafeDynamoDBGetItemErrorRedactsAPIMessage(t *testing.T) {
	t.Parallel()

	err := safeDynamoDBGetItemError(&smithy.GenericAPIError{
		Code:    "ResourceNotFoundException",
		Message: "Cannot do operations on a non-existent table tfstate-locks",
		Fault:   smithy.FaultClient,
	})
	if err == nil {
		t.Fatal("safeDynamoDBGetItemError() = nil, want error")
	}
	if strings.Contains(err.Error(), "tfstate-locks") {
		t.Fatalf("safe error = %q, leaked table name", err.Error())
	}
	if got, want := err.Error(), "aws dynamodb api error code=ResourceNotFoundException fault=client"; got != want {
		t.Fatalf("safe error = %q, want %q", got, want)
	}
}

type fakeDynamoDBGetItemClient struct {
	input  *dynamodb.GetItemInput
	output *dynamodb.GetItemOutput
	err    error
}

func (c *fakeDynamoDBGetItemClient) GetItem(
	ctx context.Context,
	input *dynamodb.GetItemInput,
	_ ...func(*dynamodb.Options),
) (*dynamodb.GetItemOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.input = input
	if c.err != nil {
		return nil, c.err
	}
	return c.output, nil
}
