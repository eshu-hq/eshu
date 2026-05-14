package awssdk

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	awsssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListsSSMParameterMetadataOnly(t *testing.T) {
	parameterARN := "arn:aws:ssm:us-east-1:123456789012:parameter/orders/db/password"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	modifiedAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeSSMAPI{
		pages: []*awsssm.DescribeParametersOutput{{
			Parameters: []awsssmtypes.ParameterMetadata{{
				ARN:              aws.String(parameterARN),
				Name:             aws.String("/orders/db/password"),
				Type:             awsssmtypes.ParameterTypeSecureString,
				Tier:             awsssmtypes.ParameterTierAdvanced,
				DataType:         aws.String("text"),
				KeyId:            aws.String(kmsARN),
				LastModifiedDate: aws.Time(modifiedAt),
				Description:      aws.String("database password"),
				AllowedPattern:   aws.String("^prod-.*$"),
				Policies: []awsssmtypes.ParameterInlinePolicy{{
					PolicyType:   aws.String("Expiration"),
					PolicyStatus: aws.String("Pending"),
					PolicyText:   aws.String(`{"secret":"do-not-store"}`),
				}},
				Version: 7,
			}},
			NextToken: aws.String("parameters-next"),
		}, {
			Parameters: []awsssmtypes.ParameterMetadata{{
				ARN:  aws.String("arn:aws:ssm:us-east-1:123456789012:parameter/payments/db/password"),
				Name: aws.String("/payments/db/password"),
				Type: awsssmtypes.ParameterTypeSecureString,
			}},
		}},
		tags: map[string][]awsssmtypes.Tag{
			"/orders/db/password": {{
				Key:   aws.String("Environment"),
				Value: aws.String("prod"),
			}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	parameters, err := adapter.ListParameters(context.Background())
	if err != nil {
		t.Fatalf("ListParameters() error = %v, want nil", err)
	}

	if got, want := len(parameters), 2; got != want {
		t.Fatalf("len(parameters) = %d, want %d", got, want)
	}
	if got, want := api.maxResults, []int32{50, 50}; !slices.Equal(got, want) {
		t.Fatalf("DescribeParameters MaxResults = %#v, want %#v", got, want)
	}
	if got, want := api.nextTokens, []string{"", "parameters-next"}; !slices.Equal(got, want) {
		t.Fatalf("DescribeParameters NextToken = %#v, want %#v", got, want)
	}
	if got, want := api.tagResourceIDs, []string{"/orders/db/password", "/payments/db/password"}; !slices.Equal(got, want) {
		t.Fatalf("ListTagsForResource ResourceId = %#v, want %#v", got, want)
	}
	if got, want := api.tagResourceTypes, []awsssmtypes.ResourceTypeForTagging{
		awsssmtypes.ResourceTypeForTaggingParameter,
		awsssmtypes.ResourceTypeForTaggingParameter,
	}; !slices.Equal(got, want) {
		t.Fatalf("ListTagsForResource ResourceType = %#v, want %#v", got, want)
	}
	parameter := parameters[0]
	if parameter.ARN != parameterARN || parameter.Name != "/orders/db/password" {
		t.Fatalf("parameter identity = %#v, want ARN and name", parameter)
	}
	if parameter.Type != "SecureString" || parameter.Tier != "Advanced" ||
		parameter.DataType != "text" || parameter.KeyID != kmsARN {
		t.Fatalf("parameter metadata = %#v, want type/tier/data/KMS", parameter)
	}
	if !parameter.DescriptionPresent || !parameter.AllowedPatternPresent {
		t.Fatalf("safe presence flags = %#v, want description and allowed pattern present", parameter)
	}
	if got := parameter.Policies[0]; got.Type != "Expiration" || got.Status != "Pending" {
		t.Fatalf("policy metadata = %#v, want type/status only", parameter.Policies)
	}
	if parameter.Tags["Environment"] != "prod" {
		t.Fatalf("tags = %#v, want Environment tag", parameter.Tags)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSSM,
	}
}

type fakeSSMAPI struct {
	pages            []*awsssm.DescribeParametersOutput
	tags             map[string][]awsssmtypes.Tag
	calls            int
	maxResults       []int32
	nextTokens       []string
	tagResourceIDs   []string
	tagResourceTypes []awsssmtypes.ResourceTypeForTagging
}

func (f *fakeSSMAPI) DescribeParameters(
	_ context.Context,
	input *awsssm.DescribeParametersInput,
	_ ...func(*awsssm.Options),
) (*awsssm.DescribeParametersOutput, error) {
	f.maxResults = append(f.maxResults, aws.ToInt32(input.MaxResults))
	f.nextTokens = append(f.nextTokens, aws.ToString(input.NextToken))
	if f.calls >= len(f.pages) {
		return &awsssm.DescribeParametersOutput{}, nil
	}
	page := f.pages[f.calls]
	f.calls++
	return page, nil
}

func (f *fakeSSMAPI) ListTagsForResource(
	_ context.Context,
	input *awsssm.ListTagsForResourceInput,
	_ ...func(*awsssm.Options),
) (*awsssm.ListTagsForResourceOutput, error) {
	resourceID := aws.ToString(input.ResourceId)
	f.tagResourceIDs = append(f.tagResourceIDs, resourceID)
	f.tagResourceTypes = append(f.tagResourceTypes, input.ResourceType)
	return &awsssm.ListTagsForResourceOutput{TagList: f.tags[resourceID]}, nil
}

var _ apiClient = (*fakeSSMAPI)(nil)
