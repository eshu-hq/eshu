// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// ssmApplicationFixture is byte-identical to the #9556 fixture-pack valid
// payload sdk/go/factschema/fixturepack/payloads/
// aws_ssm_host_application.valid.json: the single input the stable-key oracle
// below is computed from.
const ssmApplicationFixture = `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-00000000000000001","resource_type":"AWS::EC2::Instance","source_type":"AWS:Application","schema_version":"1.0.0","capture_time":"2026-10-29T22:31:19Z","name":"example-agent","version":"1.0.0","release":"1.example","epoch":"0","package_id":"example-agent-1.0.0-1.example.x86_64","architecture":"x86_64","collector_instance_id":"collector-1"}`

// ssmPlatformFixture is byte-identical to the #9556 fixture-pack valid
// payload sdk/go/factschema/fixturepack/payloads/
// aws_ssm_host_platform.valid.json.
const ssmPlatformFixture = `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-00000000000000001","resource_type":"AWS::EC2::Instance","platform_type":"Linux","platform_name":"ExampleLinux","platform_version":"1.0","source_type":"ssm_inventory","capture_time":"2026-10-29T22:31:19Z","observation_time":"2026-10-29T22:33:02Z","collector_instance_id":"collector-1"}`

func TestDecodeApplicationStableKeyMatchesOracle(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_application", ssmApplicationFixture)
	app, err := DecodeApplication(env)
	if err != nil {
		t.Fatalf("DecodeApplication valid fixture: %v", err)
	}
	const want = "aws_ssm_host_application/aws/000000000001/us-east-1/i-00000000000000001/2026-10-29T22:31:19Z/06d00205a0a803d653b6d2ed24f0b8f73aa49b4a25bbb801db12cfc3be275240"
	if got := ApplicationStableKey(app); got != want {
		t.Fatalf("ApplicationStableKey = %q, want oracle %q", got, want)
	}
}

func TestDecodePlatformStableKeyMatchesOracle(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_platform", ssmPlatformFixture)
	platform, err := DecodePlatform(env)
	if err != nil {
		t.Fatalf("DecodePlatform valid fixture: %v", err)
	}
	const want = "aws_ssm_host_platform/aws/000000000001/us-east-1/i-00000000000000001/ssm_inventory"
	if got := PlatformStableKey(platform); got != want {
		t.Fatalf("PlatformStableKey = %q, want oracle %q", got, want)
	}
}

func TestDecodeApplicationMissingCaptureTimeIsInputInvalid(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_application", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","source_type":"AWS:Application","schema_version":"1.0.0"}`)
	_, err := DecodeApplication(env)
	if err == nil {
		t.Fatalf("DecodeApplication without capture_time succeeded, want input_invalid")
	}
	if field := decodeField(err); field != "capture_time" {
		t.Fatalf("decode field = %q, want %q", field, "capture_time")
	}
}

func TestDecodeApplicationRejectsNonApplicationSource(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_application", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","source_type":"AWS:File","schema_version":"1.0.0","capture_time":"2026-10-29T22:31:19Z"}`)
	_, err := DecodeApplication(env)
	if err == nil {
		t.Fatalf("DecodeApplication with AWS:File source succeeded, want input_invalid (custom inventory is excluded)")
	}
}

func TestDecodeApplicationRejectsNonEC2Resource(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_application", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::SSM::ManagedInstance","source_type":"AWS:Application","schema_version":"1.0.0","capture_time":"2026-10-29T22:31:19Z"}`)
	_, err := DecodeApplication(env)
	if err == nil {
		t.Fatalf("DecodeApplication with non-EC2 resource type succeeded, want input_invalid")
	}
}

func TestDecodePlatformAcceptsDescribeInstanceInformation(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_platform", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","platform_type":"Linux","source_type":"describe_instance_information","observation_time":"2026-10-29T22:50:00Z"}`)
	platform, err := DecodePlatform(env)
	if err != nil {
		t.Fatalf("DecodePlatform DII observation: %v", err)
	}
	if platform.CaptureTime != nil {
		t.Fatalf("DII platform CaptureTime set, want nil (no invented capture time)")
	}
}

func TestDecodePlatformRejectsUnknownSource(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_platform", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","source_type":"fleet_manager","observation_time":"2026-10-29T22:50:00Z"}`)
	_, err := DecodePlatform(env)
	if err == nil {
		t.Fatalf("DecodePlatform with unknown source succeeded, want input_invalid")
	}
}

func TestDecodeApplicationRejectsUnsupportedMajor(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_application", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","source_type":"AWS:Application","schema_version":"2.0.0","capture_time":"2026-10-29T22:31:19Z"}`)
	_, err := DecodeApplication(env)
	if err == nil {
		t.Fatalf("DecodeApplication with schema major 2 succeeded, want a fatal (non-quarantinable) error")
	}
	if isQuarantinable(err) {
		t.Fatalf("schema-major skew is quarantinable, want fatal so the intent fails for durable triage")
	}
}

func TestDecodeApplicationQuarantinesMistypedOptional(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_application", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","source_type":"AWS:Application","schema_version":"1.0.0","capture_time":"2026-10-29T22:31:19Z","version":1.0}`)
	_, err := DecodeApplication(env)
	if err == nil {
		t.Fatalf("DecodeApplication with numeric version succeeded, want input_invalid (present-but-malformed must quarantine, never hash as absent)")
	}
	if field := decodeField(err); field != "version" {
		t.Fatalf("decode field = %q, want %q", field, "version")
	}
	if !isQuarantinable(err) {
		t.Fatalf("mistyped optional is not quarantinable, want per-fact quarantine")
	}
}

func TestDecodePlatformQuarantinesMistypedOptional(t *testing.T) {
	env := ssmEnvelope("aws_ssm_host_platform", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","source_type":"ssm_inventory","observation_time":"2026-10-29T22:50:00Z","platform_type":42}`)
	_, err := DecodePlatform(env)
	if err == nil {
		t.Fatalf("DecodePlatform with numeric platform_type succeeded, want input_invalid")
	}
	if field := decodeField(err); field != "platform_type" {
		t.Fatalf("decode field = %q, want %q", field, "platform_type")
	}
}

func ssmEnvelope(kind, payload string) facts.Envelope {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		panic(err)
	}
	return facts.Envelope{FactID: "fact-1", FactKind: kind, Payload: decoded}
}

// decodeField extracts the offending required-field name from a decode error.
func decodeField(err error) string {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return decodeErr.Field
	}
	return ""
}

// isQuarantinable reports whether the extractor would quarantine the fact
// per-fact (true) or fail the whole intent (false) for the error, using the
// production classifier itself.
func isQuarantinable(err error) bool {
	_, ok, fatal := factdecode.PartitionDecodeFailures(facts.Envelope{}, err)
	return ok && fatal == nil
}
