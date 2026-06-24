// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ses

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testTopicARN    = "arn:aws:sns:us-east-1:123456789012:ses-events"
	testFirehoseARN = "arn:aws:firehose:us-east-1:123456789012:deliverystream/ses-events"
	testRoleARN     = "arn:aws:iam::123456789012:role/ses-firehose"
	testKMSARN      = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		EmailIdentities: []EmailIdentity{{
			Name:                        "marketing.example.com",
			IdentityType:                "DOMAIN",
			VerificationStatus:          "SUCCESS",
			VerifiedForSendingStatus:    true,
			SendingEnabled:              true,
			FeedbackForwardingStatus:    true,
			ConfigurationSetName:        "primary",
			DKIMEnabled:                 true,
			DKIMStatus:                  "SUCCESS",
			DKIMSigningAttributesOrigin: "AWS_SES",
			MailFromDomain:              "mail.example.com",
			MailFromDomainStatus:        "SUCCESS",
			MailFromBehaviorOnMxFailure: "USE_DEFAULT_VALUE",
			Tags:                        map[string]string{"Environment": "prod"},
		}},
		ConfigurationSets: []ConfigurationSet{{
			Name:                     "primary",
			SendingEnabled:           true,
			ReputationMetricsEnabled: true,
			TLSPolicy:                "REQUIRE",
			SendingPoolName:          "dedicated-prod",
			CustomRedirectDomain:     "click.example.com",
			Tags:                     map[string]string{"Team": "growth"},
			EventDestinations: []EventDestination{{
				Name:                      "all-events",
				Enabled:                   true,
				MatchingEventTypes:        []string{"SEND", "DELIVERY", "BOUNCE"},
				SNSTopicARN:               testTopicARN,
				FirehoseDeliveryStreamARN: testFirehoseARN,
				FirehoseIAMRoleARN:        testRoleARN,
				CloudWatchEnabled:         true,
			}},
		}},
		DedicatedIPPools: []DedicatedIPPool{{Name: "dedicated-prod"}},
	}
}

func TestScannerEmitsSESMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(
		context.Background(),
		testBoundary(),
	)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	identity := resourceByType(t, envelopes, awscloud.ResourceTypeSESEmailIdentity)
	if got, want := identity.Payload["resource_id"], "marketing.example.com"; got != want {
		t.Fatalf("identity resource_id = %#v, want %q", got, want)
	}
	if got, want := identity.Payload["arn"], "arn:aws:ses:us-east-1:123456789012:identity/marketing.example.com"; got != want {
		t.Fatalf("identity arn = %#v, want %q", got, want)
	}
	idAttrs := attributesOf(t, identity)
	assertAttribute(t, idAttrs, "identity_type", "DOMAIN")
	assertAttribute(t, idAttrs, "verification_status", "SUCCESS")
	assertAttribute(t, idAttrs, "dkim_enabled", true)
	assertAttribute(t, idAttrs, "dkim_signing_attributes_origin", "AWS_SES")
	assertAttribute(t, idAttrs, "mail_from_domain", "mail.example.com")

	set := resourceByType(t, envelopes, awscloud.ResourceTypeSESConfigurationSet)
	if got, want := set.Payload["resource_id"], "primary"; got != want {
		t.Fatalf("configuration set resource_id = %#v, want %q", got, want)
	}
	setAttrs := attributesOf(t, set)
	assertAttribute(t, setAttrs, "tls_policy", "REQUIRE")
	assertAttribute(t, setAttrs, "sending_pool_name", "dedicated-prod")
	assertAttribute(t, setAttrs, "event_destination_count", 1)

	destination := resourceByType(t, envelopes, awscloud.ResourceTypeSESEventDestination)
	if got, want := destination.Payload["resource_id"], "primary/all-events"; got != want {
		t.Fatalf("event destination resource_id = %#v, want %q", got, want)
	}
	destAttrs := attributesOf(t, destination)
	assertAttribute(t, destAttrs, "matching_event_types", []string{"SEND", "DELIVERY", "BOUNCE"})
	assertAttribute(t, destAttrs, "destination_classes", []string{"sns", "kinesis_firehose", "cloud_watch"})

	pool := resourceByType(t, envelopes, awscloud.ResourceTypeSESDedicatedIPPool)
	if got, want := pool.Payload["resource_id"], "dedicated-prod"; got != want {
		t.Fatalf("dedicated ip pool resource_id = %#v, want %q", got, want)
	}

	// identity -> default configuration set edge, keyed by the set name.
	idSet := relationshipByType(t, envelopes, awscloud.RelationshipSESEmailIdentityUsesConfigurationSet)
	assertEdgeTarget(t, idSet, awscloud.ResourceTypeSESConfigurationSet, "primary")
	if got, want := idSet.Payload["source_resource_id"], "marketing.example.com"; got != want {
		t.Fatalf("identity->set source_resource_id = %#v, want %q", got, want)
	}

	// configuration set -> dedicated IP pool edge, keyed by the pool name.
	setPool := relationshipByType(t, envelopes, awscloud.RelationshipSESConfigurationSetUsesDedicatedIPPool)
	assertEdgeTarget(t, setPool, awscloud.ResourceTypeSESDedicatedIPPool, "dedicated-prod")

	// event destination -> SNS topic edge, keyed by the reported topic ARN.
	destSNS := relationshipByType(t, envelopes, awscloud.RelationshipSESEventDestinationPublishesToSNSTopic)
	assertEdgeTarget(t, destSNS, awscloud.ResourceTypeSNSTopic, testTopicARN)
	if got, want := destSNS.Payload["target_arn"], testTopicARN; got != want {
		t.Fatalf("dest->sns target_arn = %#v, want %q", got, want)
	}
	if got, want := destSNS.Payload["source_resource_id"], "primary/all-events"; got != want {
		t.Fatalf("dest->sns source_resource_id = %#v, want %q", got, want)
	}

	// event destination -> Firehose delivery stream edge, keyed by the stream ARN.
	destFirehose := relationshipByType(t, envelopes, awscloud.RelationshipSESEventDestinationStreamsToFirehose)
	assertEdgeTarget(t, destFirehose, awscloud.ResourceTypeFirehoseDeliveryStream, testFirehoseARN)
	if got, want := destFirehose.Payload["target_arn"], testFirehoseARN; got != want {
		t.Fatalf("dest->firehose target_arn = %#v, want %q", got, want)
	}

	// No DKIM tokens, signing keys, policies, or message bodies anywhere.
	assertNoForbiddenAttributes(t, envelopes)
}

func TestScannerSynthesizesGovCloudIdentityARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	snapshot := Snapshot{EmailIdentities: []EmailIdentity{{
		Name:               "gov.example.com",
		VerificationStatus: "SUCCESS",
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	identity := resourceByType(t, envelopes, awscloud.ResourceTypeSESEmailIdentity)
	want := "arn:aws-us-gov:ses:us-gov-west-1:123456789012:identity/gov.example.com"
	if got := identity.Payload["arn"]; got != want {
		t.Fatalf("GovCloud identity arn = %#v, want %q", got, want)
	}
}

func TestScannerSynthesizesChinaConfigurationSetARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	snapshot := Snapshot{ConfigurationSets: []ConfigurationSet{{Name: "cn-set"}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	set := resourceByType(t, envelopes, awscloud.ResourceTypeSESConfigurationSet)
	want := "arn:aws-cn:ses:cn-north-1:123456789012:configuration-set/cn-set"
	if got := set.Payload["arn"]; got != want {
		t.Fatalf("China configuration set arn = %#v, want %q", got, want)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	snapshot := Snapshot{
		EmailIdentities:   []EmailIdentity{{Name: "bare.example.com", VerificationStatus: "PENDING"}},
		ConfigurationSets: []ConfigurationSet{{Name: "shared"}},
		DedicatedIPPools:  []DedicatedIPPool{{Name: "unused"}},
	}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerEmitsDefensiveDKIMKMSEdge(t *testing.T) {
	snapshot := Snapshot{EmailIdentities: []EmailIdentity{{
		Name:         "kms.example.com",
		DKIMKMSKeyID: testKMSARN,
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	dkimKMS := relationshipByType(t, envelopes, awscloud.RelationshipSESEmailIdentityDKIMUsesKMSKey)
	assertEdgeTarget(t, dkimKMS, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := dkimKMS.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("dkim->kms target_arn = %#v, want %q", got, want)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	snapshot := fullSnapshot()
	snapshot.EmailIdentities[0].DKIMKMSKeyID = testKMSARN
	identity := snapshot.EmailIdentities[0]
	set := snapshot.ConfigurationSets[0]
	destination := set.EventDestinations[0]
	setID := configurationSetResourceID(set)

	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		identityConfigurationSetRelationship(boundary, identity),
		identityDKIMKMSRelationship(boundary, identity),
		configurationSetDedicatedIPPoolRelationship(boundary, set),
		eventDestinationSNSTopicRelationship(boundary, setID, destination),
		eventDestinationFirehoseRelationship(boundary, setID, destination),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerCanonicalizesBlankServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""

	envelopes, err := (Scanner{Client: fakeClient{snapshot: Snapshot{
		DedicatedIPPools: []DedicatedIPPool{{Name: "p1"}},
	}}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	pool := resourceByType(t, envelopes, awscloud.ResourceTypeSESDedicatedIPPool)
	if got, want := pool.Payload["service_kind"], awscloud.ServiceSES; got != want {
		t.Fatalf("service_kind = %#v, want %q", got, want)
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	snapshot := Snapshot{
		EmailIdentities: []EmailIdentity{{Name: "a.example.com"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "SES ListConfigurationSets throttled after SDK retries; set metadata omitted for this scan",
			SourceRecordID: "ses_configuration_sets_throttled",
		}},
	}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func assertNoForbiddenAttributes(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"dkim_tokens", "dkim_signing_key", "dkim_private_key", "signing_tokens",
			"smtp_password", "smtp_credentials", "policies", "policy_document",
			"message", "message_body", "template_body", "raw_message",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; SES scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSES,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ses:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
