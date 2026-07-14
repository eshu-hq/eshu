// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
	conformance "github.com/eshu-hq/eshu/sdk/go/collector/conformance"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// emitterConformanceCase names one internal fact emitter and builds a REAL
// envelope from it (no hand-built map[string]any stand-in), so the payload
// under test is exactly what the collector actually emits at runtime.
type emitterConformanceCase struct {
	// name identifies the subtest; it is also the fact kind's wire string,
	// which doubles as the committed schema's base filename
	// (schema/<name>.v1.schema.json) and the key both the conformance
	// validator and factschema.SchemaBytes look up by.
	name  string
	build func() (facts.Envelope, error)
}

// emitterConformanceCases lists the internal AWS/IAM/S3 fact emitters this
// test proves against their own committed JSON Schema. This closes the gap a
// prior review flagged: CompileSchema and the fixture-pack payloads prove the
// *validator* understands every checked-in schema construct, but nothing
// proved a real emitter's *actual output* satisfies its own schema until this
// test. Adding a new emitter here is the direct way to extend that proof.
func emitterConformanceCases() []emitterConformanceCase {
	return []emitterConformanceCase{
		{
			name: facts.AWSSecurityGroupRuleFactKind,
			build: func() (facts.Envelope, error) {
				return awscloud.NewSecurityGroupRuleEnvelope(securityGroupRuleObservation())
			},
		},
		{
			name: facts.EC2InstancePostureFactKind,
			build: func() (facts.Envelope, error) {
				return awscloud.NewEC2InstancePostureEnvelope(ec2InstancePostureObservation())
			},
		},
		{
			name: facts.AWSIAMPermissionFactKind,
			build: func() (facts.Envelope, error) {
				return awscloud.NewIAMPermissionEnvelope(iamPermissionObservation())
			},
		},
		{
			name: facts.AWSResourcePolicyPermissionFactKind,
			build: func() (facts.Envelope, error) {
				return awscloud.NewResourcePolicyPermissionEnvelope(resourcePolicyPermissionObservation())
			},
		},
		{
			name: facts.S3BucketPostureFactKind,
			build: func() (facts.Envelope, error) {
				return awscloud.NewS3BucketPostureEnvelope(s3BucketPostureObservation())
			},
		},
		{
			name: facts.AWSIAMPrincipalFactKind,
			build: func() (facts.Envelope, error) {
				return secretsiam.NewPrincipalEnvelope(principalObservation())
			},
		},
	}
}

// TestEmitterPayloadsConformToCommittedSchemas builds one real envelope per
// named emitter and validates its payload against the committed JSON Schema
// for that exact fact kind, through the same dependency-free conformance
// validator an out-of-tree collector's own CI runs
// (sdk/go/collector/conformance, via ValidatePayloadSchemas). Passing here is
// the direct "the schema matches what the emitter emits" proof; see
// TestEmitterPayloadMissingRequiredFieldFailsConformance for the fail-closed
// half of that proof.
func TestEmitterPayloadsConformToCommittedSchemas(t *testing.T) {
	t.Parallel()

	for _, tc := range emitterConformanceCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env, err := tc.build()
			if err != nil {
				t.Fatalf("build real envelope for %q: %v", tc.name, err)
			}
			if env.FactKind != tc.name {
				t.Fatalf("envelope FactKind = %q, want %q", env.FactKind, tc.name)
			}

			schemaBytes, ok := factschema.SchemaBytes(tc.name)
			if !ok {
				t.Fatalf("factschema.SchemaBytes(%q) ok = false; the fact kind must ship a committed schema", tc.name)
			}

			schemas := map[string]json.RawMessage{tc.name: schemaBytes}
			if err := conformance.ValidatePayloadSchemas(schemas, singleFactResult(tc.name, env.Payload)); err != nil {
				t.Fatalf("real emitter payload for %q failed conformance against its own committed schema: %v", tc.name, err)
			}
		})
	}
}

// TestEmitterPayloadMissingRequiredFieldFailsConformance proves the proof
// above has teeth: every checked-in schema in emitterConformanceCases
// requires account_id (the AWS account boundary every one of these fact
// kinds anchors to). Stripping it from a real emitter's payload MUST fail
// conformance validation; if this test ever passed, the positive test above
// would not actually be exercising payload-shape validation.
func TestEmitterPayloadMissingRequiredFieldFailsConformance(t *testing.T) {
	t.Parallel()

	const requiredField = "account_id"

	for _, tc := range emitterConformanceCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env, err := tc.build()
			if err != nil {
				t.Fatalf("build real envelope for %q: %v", tc.name, err)
			}
			if _, present := env.Payload[requiredField]; !present {
				t.Fatalf("test assumption broken: %q payload has no %q field to remove", tc.name, requiredField)
			}

			schemaBytes, ok := factschema.SchemaBytes(tc.name)
			if !ok {
				t.Fatalf("factschema.SchemaBytes(%q) ok = false", tc.name)
			}

			broken := clonePayload(env.Payload)
			delete(broken, requiredField)

			schemas := map[string]json.RawMessage{tc.name: schemaBytes}
			err = conformance.ValidatePayloadSchemas(schemas, singleFactResult(tc.name, broken))
			if err == nil {
				t.Fatalf("payload for %q missing required field %q passed conformance validation, want a failure", tc.name, requiredField)
			}
		})
	}
}

// singleFactResult wraps one fact kind/payload pair in the minimal
// collector.Result shape ValidatePayloadSchemas inspects: it reads only
// Facts[i].Kind and Facts[i].Payload, so no claim, generation, or protocol
// metadata is required to exercise payload-schema validation in isolation.
func singleFactResult(kind string, payload map[string]any) sdkcollector.Result {
	return sdkcollector.Result{
		Facts: []sdkcollector.Fact{{
			Kind:    kind,
			Payload: payload,
		}},
	}
}

// clonePayload returns a shallow copy of payload so a test can add or delete a
// top-level key on the copy without mutating the envelope the emitter produced
// (which other subtests or a shared fixture may still reference). The copy is
// shallow: nested slice and map values are shared with the original, so a
// caller must not mutate a nested value in place — under t.Parallel that would
// race a reader of the original envelope. The only mutation this helper is
// built for is deleting a required top-level key to prove the negative case.
func clonePayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

// awsBoundary builds a valid awscloud.Boundary for one AWS service kind, the
// common identity every awscloud emitter observation requires.
func awsBoundary(serviceKind string, observedAt time.Time) awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         serviceKind,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:" + serviceKind + ":1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          observedAt,
	}
}

func securityGroupRuleObservation() awscloud.SecurityGroupRuleObservation {
	fromPort := int32(443)
	toPort := int32(443)
	return awscloud.SecurityGroupRuleObservation{
		Boundary:     awsBoundary(awscloud.ServiceEC2, time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)),
		RuleID:       "sgr-123",
		GroupID:      "sg-123",
		GroupOwnerID: "123456789012",
		IsEgress:     false,
		IPProtocol:   "tcp",
		FromPort:     &fromPort,
		ToPort:       &toPort,
		CIDRIPv4:     "0.0.0.0/0",
		Description:  "public https",
	}
}

func ec2InstancePostureObservation() awscloud.EC2InstancePostureObservation {
	imdsv2 := true
	hopLimit := int32(1)
	userData := true
	volumeEncrypted := true
	return awscloud.EC2InstancePostureObservation{
		Boundary:                awsBoundary(awscloud.ServiceEC2, time.Date(2026, 5, 31, 18, 30, 0, 0, time.UTC)),
		ARN:                     "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
		InstanceID:              "i-1234567890abcdef0",
		State:                   "running",
		IMDSv2Required:          &imdsv2,
		HTTPEndpoint:            "enabled",
		HTTPPutResponseHopLimit: &hopLimit,
		UserDataPresent:         &userData,
		DetailedMonitoring:      true,
		EBSOptimized:            true,
		PublicIPAssociated:      true,
		PublicIPAddress:         "203.0.113.10",
		InstanceProfileARN:      "arn:aws:iam::123456789012:instance-profile/app",
		Tenancy:                 "default",
		NitroEnclaveEnabled:     true,
		BlockDevices: []awscloud.EC2BlockDevicePosture{{
			DeviceName:          "/dev/xvda",
			VolumeID:            "vol-0abc",
			DeleteOnTermination: true,
			Status:              "attached",
			Encrypted:           &volumeEncrypted,
		}},
	}
}

func iamPermissionObservation() awscloud.IAMPermissionObservation {
	return awscloud.IAMPermissionObservation{
		Boundary:      awsBoundary(awscloud.ServiceIAM, time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)),
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: awscloud.ResourceTypeIAMRole,
		PolicySource:  awscloud.IAMPolicySourceInline,
		PolicyName:    "inline-escalate",
		StatementSID:  "AllowPassRole",
		Effect:        "Allow",
		Actions:       []string{"iam:PassRole"},
		Resources:     []string{"arn:aws:iam::123456789012:role/*"},
	}
}

func resourcePolicyPermissionObservation() awscloud.ResourcePolicyPermissionObservation {
	return awscloud.ResourcePolicyPermissionObservation{
		Boundary:            awsBoundary(awscloud.ServiceS3, time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)),
		ResourceARN:         "arn:aws:s3:::eshu-shared-bucket",
		ResourceType:        awscloud.ResourceTypeS3Bucket,
		StatementSID:        "AllowPartner",
		Effect:              "Allow",
		Actions:             []string{"s3:GetObject"},
		Resources:           []string{"arn:aws:s3:::eshu-shared-bucket/*"},
		PrincipalARNs:       []string{"arn:aws:iam::111122223333:role/partner"},
		PrincipalAccountIDs: []string{"111122223333"},
		PrincipalTypes:      []string{awscloud.ResourcePolicyPrincipalTypeAWS},
		IsCrossAccount:      true,
	}
}

func s3BucketPostureObservation() awscloud.S3BucketPostureObservation {
	trueVal := true
	falseVal := false
	return awscloud.S3BucketPostureObservation{
		Boundary:                    awsBoundary(awscloud.ServiceS3, time.Date(2026, 5, 14, 17, 30, 0, 0, time.UTC)),
		BucketARN:                   "arn:aws:s3:::orders-artifacts",
		BucketName:                  "orders-artifacts",
		BlockPublicACLs:             &trueVal,
		IgnorePublicACLs:            &trueVal,
		BlockPublicPolicy:           &trueVal,
		RestrictPublicBuckets:       &trueVal,
		BlockPublicAccessAllEnabled: &trueVal,
		DefaultEncryptionEnabled:    true,
		EncryptionAlgorithms:        []string{"aws:kms"},
		SSEKMSKeyARN:                "arn:aws:kms:us-east-1:123456789012:key/orders",
		BucketKeyEnabled:            true,
		VersioningStatus:            "Enabled",
		VersioningEnabled:           true,
		ObjectOwnership:             []string{"BucketOwnerEnforced"},
		ACLDisabled:                 true,
		LoggingEnabled:              true,
		LoggingTargetBucket:         "orders-logs",
		ReplicationEnabled:          true,
		PolicyPresent:               true,
		PolicyGrantsPublic:          &falseVal,
		PolicyGrantsCrossAccount:    &trueVal,
	}
}

func secretsIAMContext() secretsiam.EnvelopeContext {
	return secretsiam.EnvelopeContext{
		AccountID:           "123456789012",
		Region:              "aws-global",
		ScopeID:             "aws:123456789012:aws-global",
		GenerationID:        "aws:123456789012:aws-global:iam:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	}
}

func principalObservation() secretsiam.PrincipalObservation {
	return secretsiam.PrincipalObservation{
		Context:       secretsIAMContext(),
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: secretsiam.PrincipalTypeAWSRole,
		Name:          "eshu-runtime",
	}
}
