// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssesv2 "github.com/aws/aws-sdk-go-v2/service/sesv2"
	awssesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientExcludesSendAndMutationOperations is the metadata-only exclusion
// guard. It reflects over the adapter's apiClient interface and fails if any
// send, mutation, or non-allowed operation is reachable. The adapter must never
// expose the SES send or write path, so the interface is the proof: a method
// that is not on it cannot be called.
func TestAPIClientExcludesSendAndMutationOperations(t *testing.T) {
	forbiddenPrefixes := []string{
		"Send", "Create", "Update", "Delete", "Put",
		"Tag", "Untag", "Test",
	}
	apiType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < apiType.NumMethod(); i++ {
		name := apiType.Method(i).Name
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes forbidden operation %q (prefix %q); the metadata-only adapter must not reach send or mutation APIs", name, prefix)
			}
		}
	}

	allowed := map[string]struct{}{
		"ListEmailIdentities":                  {},
		"GetEmailIdentity":                     {},
		"ListConfigurationSets":                {},
		"GetConfigurationSet":                  {},
		"GetConfigurationSetEventDestinations": {},
		"ListDedicatedIpPools":                 {},
	}
	if got := apiType.NumMethod(); got != len(allowed) {
		t.Fatalf("apiClient has %d methods, want %d (only the read surface)", got, len(allowed))
	}
	for i := 0; i < apiType.NumMethod(); i++ {
		name := apiType.Method(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient exposes unexpected operation %q; the read surface is the six SES list/get reads only", name)
		}
	}
}

func TestClientSnapshotMapsSafeMetadata(t *testing.T) {
	client := &fakeSESAPI{
		identityPages: []*awssesv2.ListEmailIdentitiesOutput{{
			EmailIdentities: []awssesv2types.IdentityInfo{{
				IdentityName:       aws.String("marketing.example.com"),
				IdentityType:       awssesv2types.IdentityTypeDomain,
				VerificationStatus: awssesv2types.VerificationStatusSuccess,
				SendingEnabled:     true,
			}},
		}},
		getIdentity: map[string]*awssesv2.GetEmailIdentityOutput{
			"marketing.example.com": {
				ConfigurationSetName:     aws.String("primary"),
				IdentityType:             awssesv2types.IdentityTypeDomain,
				VerificationStatus:       awssesv2types.VerificationStatusSuccess,
				VerifiedForSendingStatus: true,
				FeedbackForwardingStatus: true,
				DkimAttributes: &awssesv2types.DkimAttributes{
					SigningEnabled:          true,
					Status:                  awssesv2types.DkimStatusSuccess,
					SigningAttributesOrigin: awssesv2types.DkimSigningAttributesOriginAwsSes,
					// Tokens must never be mapped.
					Tokens: []string{"token-one", "token-two"},
				},
				MailFromAttributes: &awssesv2types.MailFromAttributes{
					MailFromDomain:       aws.String("mail.example.com"),
					MailFromDomainStatus: awssesv2types.MailFromDomainStatusSuccess,
					BehaviorOnMxFailure:  awssesv2types.BehaviorOnMxFailureUseDefaultValue,
				},
				// Policies must never be mapped.
				Policies: map[string]string{"policy-1": "{\"Version\":\"2012-10-17\"}"},
				Tags:     []awssesv2types.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
			},
		},
		configSetPages: []*awssesv2.ListConfigurationSetsOutput{{
			ConfigurationSets: []string{"primary"},
		}},
		getConfigSet: map[string]*awssesv2.GetConfigurationSetOutput{
			"primary": {
				ConfigurationSetName: aws.String("primary"),
				SendingOptions:       &awssesv2types.SendingOptions{SendingEnabled: true},
				ReputationOptions:    &awssesv2types.ReputationOptions{ReputationMetricsEnabled: true},
				DeliveryOptions: &awssesv2types.DeliveryOptions{
					TlsPolicy:       awssesv2types.TlsPolicyRequire,
					SendingPoolName: aws.String("dedicated-prod"),
				},
				TrackingOptions: &awssesv2types.TrackingOptions{
					CustomRedirectDomain: aws.String("click.example.com"),
				},
				Tags: []awssesv2types.Tag{{Key: aws.String("Team"), Value: aws.String("growth")}},
			},
		},
		eventDestinations: map[string]*awssesv2.GetConfigurationSetEventDestinationsOutput{
			"primary": {
				EventDestinations: []awssesv2types.EventDestination{{
					Name:               aws.String("all-events"),
					Enabled:            true,
					MatchingEventTypes: []awssesv2types.EventType{awssesv2types.EventTypeSend, awssesv2types.EventTypeBounce},
					SnsDestination: &awssesv2types.SnsDestination{
						TopicArn: aws.String("arn:aws:sns:us-east-1:123456789012:ses-events"),
					},
					KinesisFirehoseDestination: &awssesv2types.KinesisFirehoseDestination{
						DeliveryStreamArn: aws.String("arn:aws:firehose:us-east-1:123456789012:deliverystream/ses-events"),
						IamRoleArn:        aws.String("arn:aws:iam::123456789012:role/ses-firehose"),
					},
					CloudWatchDestination: &awssesv2types.CloudWatchDestination{},
				}},
			},
		},
		poolPages: []*awssesv2.ListDedicatedIpPoolsOutput{{
			DedicatedIpPools: []string{"dedicated-prod"},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if got, want := len(snapshot.EmailIdentities), 1; got != want {
		t.Fatalf("len(EmailIdentities) = %d, want %d", got, want)
	}
	identity := snapshot.EmailIdentities[0]
	if identity.Name != "marketing.example.com" {
		t.Fatalf("identity.Name = %q", identity.Name)
	}
	if identity.ConfigurationSetName != "primary" {
		t.Fatalf("identity.ConfigurationSetName = %q, want primary", identity.ConfigurationSetName)
	}
	if !identity.DKIMEnabled || identity.DKIMStatus != "SUCCESS" {
		t.Fatalf("identity DKIM = %+v, want enabled SUCCESS", identity)
	}
	if identity.DKIMSigningAttributesOrigin != "AWS_SES" {
		t.Fatalf("identity.DKIMSigningAttributesOrigin = %q, want AWS_SES", identity.DKIMSigningAttributesOrigin)
	}
	if identity.MailFromDomain != "mail.example.com" {
		t.Fatalf("identity.MailFromDomain = %q", identity.MailFromDomain)
	}
	if identity.Tags["Environment"] != "prod" {
		t.Fatalf("identity.Tags = %#v, want Environment=prod", identity.Tags)
	}

	if got, want := len(snapshot.ConfigurationSets), 1; got != want {
		t.Fatalf("len(ConfigurationSets) = %d, want %d", got, want)
	}
	set := snapshot.ConfigurationSets[0]
	if set.TLSPolicy != "REQUIRE" || set.SendingPoolName != "dedicated-prod" {
		t.Fatalf("set options = %+v", set)
	}
	if got, want := len(set.EventDestinations), 1; got != want {
		t.Fatalf("len(EventDestinations) = %d, want %d", got, want)
	}
	destination := set.EventDestinations[0]
	if destination.SNSTopicARN != "arn:aws:sns:us-east-1:123456789012:ses-events" {
		t.Fatalf("destination.SNSTopicARN = %q", destination.SNSTopicARN)
	}
	if destination.FirehoseDeliveryStreamARN != "arn:aws:firehose:us-east-1:123456789012:deliverystream/ses-events" {
		t.Fatalf("destination.FirehoseDeliveryStreamARN = %q", destination.FirehoseDeliveryStreamARN)
	}
	if !destination.CloudWatchEnabled {
		t.Fatalf("destination.CloudWatchEnabled = false, want true")
	}

	if got, want := len(snapshot.DedicatedIPPools), 1; got != want {
		t.Fatalf("len(DedicatedIPPools) = %d, want %d", got, want)
	}
	if snapshot.DedicatedIPPools[0].Name != "dedicated-prod" {
		t.Fatalf("pool name = %q", snapshot.DedicatedIPPools[0].Name)
	}
}

func TestClientNeverMapsDKIMTokensOrPolicies(t *testing.T) {
	output := &awssesv2.GetEmailIdentityOutput{
		DkimAttributes: &awssesv2types.DkimAttributes{
			SigningEnabled: true,
			Tokens:         []string{"secret-token"},
		},
		Policies: map[string]string{"p": "policy-body"},
	}
	identity := mapIdentityInfo(awssesv2types.IdentityInfo{IdentityName: aws.String("a.example.com")})
	applyIdentityDetail(&identity, output)

	// The scanner-owned identity model has no token/policy fields by design, so
	// proving the mapping leaves DKIMKMSKeyID empty (the only key-ish field) is
	// the in-package check that no signing material leaks.
	if identity.DKIMKMSKeyID != "" {
		t.Fatalf("DKIMKMSKeyID = %q, want empty; SES v2 surfaces no customer DKIM key", identity.DKIMKMSKeyID)
	}
	if !identity.DKIMEnabled {
		t.Fatalf("DKIMEnabled = false, want true")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSES,
		ScopeID:     "aws:123456789012:us-east-1",
	}
}

type fakeSESAPI struct {
	identityPages     []*awssesv2.ListEmailIdentitiesOutput
	identityCalls     int
	getIdentity       map[string]*awssesv2.GetEmailIdentityOutput
	configSetPages    []*awssesv2.ListConfigurationSetsOutput
	configSetCalls    int
	getConfigSet      map[string]*awssesv2.GetConfigurationSetOutput
	eventDestinations map[string]*awssesv2.GetConfigurationSetEventDestinationsOutput
	poolPages         []*awssesv2.ListDedicatedIpPoolsOutput
	poolCalls         int
}

func (f *fakeSESAPI) ListEmailIdentities(
	_ context.Context,
	_ *awssesv2.ListEmailIdentitiesInput,
	_ ...func(*awssesv2.Options),
) (*awssesv2.ListEmailIdentitiesOutput, error) {
	if f.identityCalls >= len(f.identityPages) {
		return &awssesv2.ListEmailIdentitiesOutput{}, nil
	}
	page := f.identityPages[f.identityCalls]
	f.identityCalls++
	return page, nil
}

func (f *fakeSESAPI) GetEmailIdentity(
	_ context.Context,
	input *awssesv2.GetEmailIdentityInput,
	_ ...func(*awssesv2.Options),
) (*awssesv2.GetEmailIdentityOutput, error) {
	if output, ok := f.getIdentity[aws.ToString(input.EmailIdentity)]; ok {
		return output, nil
	}
	return &awssesv2.GetEmailIdentityOutput{}, nil
}

func (f *fakeSESAPI) ListConfigurationSets(
	_ context.Context,
	_ *awssesv2.ListConfigurationSetsInput,
	_ ...func(*awssesv2.Options),
) (*awssesv2.ListConfigurationSetsOutput, error) {
	if f.configSetCalls >= len(f.configSetPages) {
		return &awssesv2.ListConfigurationSetsOutput{}, nil
	}
	page := f.configSetPages[f.configSetCalls]
	f.configSetCalls++
	return page, nil
}

func (f *fakeSESAPI) GetConfigurationSet(
	_ context.Context,
	input *awssesv2.GetConfigurationSetInput,
	_ ...func(*awssesv2.Options),
) (*awssesv2.GetConfigurationSetOutput, error) {
	if output, ok := f.getConfigSet[aws.ToString(input.ConfigurationSetName)]; ok {
		return output, nil
	}
	return &awssesv2.GetConfigurationSetOutput{}, nil
}

func (f *fakeSESAPI) GetConfigurationSetEventDestinations(
	_ context.Context,
	input *awssesv2.GetConfigurationSetEventDestinationsInput,
	_ ...func(*awssesv2.Options),
) (*awssesv2.GetConfigurationSetEventDestinationsOutput, error) {
	if output, ok := f.eventDestinations[aws.ToString(input.ConfigurationSetName)]; ok {
		return output, nil
	}
	return &awssesv2.GetConfigurationSetEventDestinationsOutput{}, nil
}

func (f *fakeSESAPI) ListDedicatedIpPools(
	_ context.Context,
	_ *awssesv2.ListDedicatedIpPoolsInput,
	_ ...func(*awssesv2.Options),
) (*awssesv2.ListDedicatedIpPoolsOutput, error) {
	if f.poolCalls >= len(f.poolPages) {
		return &awssesv2.ListDedicatedIpPoolsOutput{}, nil
	}
	page := f.poolPages[f.poolCalls]
	f.poolCalls++
	return page, nil
}

var _ apiClient = (*fakeSESAPI)(nil)
