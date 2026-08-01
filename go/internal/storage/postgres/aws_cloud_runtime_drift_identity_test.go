// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"
	"testing"
)

// Identity-side coverage for the terraform-state redaction contract. Kept apart
// from the value-attribute tests because the failure modes differ in kind: a
// redacted VALUE costs a comparison, while a redacted JOIN KEY removes the
// declared row from correlation entirely (#5859, #5870).

// TestStateRowFromPayloadRejectsARedactedARN pins the identity half of the
// redaction problem, which is a different failure from the value half #5859
// fixes.
//
// LoadPackagedSchemaResolver returns (nil, nil) when no provider-schema bundle
// parses (schema_resolver.go:102-103) -- success carrying a nil resolver, not an
// error -- and parseSchemaInto swallows per-file parse failures, so a corrupt or
// empty bundle silently yields that nil. schemaTrust then answers SchemaUnknown
// for EVERY (resourceType, attributeKey) pair with no exemption, so the
// terraform-state parser fail-closed-redacts every scalar, "arn" included.
//
// "arn" is not a value, it is the join key. A redaction map rendered through
// coerceJSONString's fmt.Sprint default is a non-empty garbage string, so it
// passes the only guard here (`arn == ""`) and becomes the key into stateByARN.
// It matches no AWS-observed ARN, so the declared row is unreachable and every
// cloud resource under that broken bundle classifies orphaned_cloud_resource.
//
// Rejecting the row does NOT change that outcome -- the caller iterates
// observed ARNs, so a key nothing matches and a row never stored are
// indistinguishable downstream. What it changes is visibility: only the
// rejection lets the caller name the cause as state_resource_arn_redacted
// rather than as generic decode noise, which is the difference between an
// operator landing on the provider-schema bundle and sifting malformed-payload
// warnings while an account reads as unmanaged. This test pins the rejection;
// TestStateResourceDecodeFailureClass below pins the label it produces.
//
// Whether an identity anchor should be fail-closed-redacted at all is the
// upstream policy question and stays open on #5870.
func TestStateRowFromPayloadRejectsARedactedARN(t *testing.T) {
	t.Parallel()

	redactedARN := map[string]any{
		"marker": "redacted:hmac-sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"reason": "unknown_provider_schema",
		"source": "resources.*.attributes.arn",
	}

	for _, tc := range []struct {
		name    string
		payload map[string]any
	}{{
		name: "aws_instance",
		payload: map[string]any{
			"address":    "module.ecs.aws_instance.supply-chain-demo",
			"type":       "aws_instance",
			"attributes": map[string]any{"arn": redactedARN, "ami": "ami-declared"},
		},
	}, {
		// A readable declared value alongside the redacted key is the case
		// worth naming: the row still goes, because an unusable join key makes
		// every value on it unreachable regardless of how readable it is.
		name: "aws_lambda_function with readable comparables",
		payload: map[string]any{
			"address":    "module.fn.aws_lambda_function.demo",
			"type":       "aws_lambda_function",
			"attributes": map[string]any{"arn": redactedARN, "image_uri": "repo/img:v1", "version": "7"},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			row, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", payload)
			if ok {
				t.Fatalf("awsRuntimeStateRowFromPayload() accepted a redacted ARN as the join key: ARN=%q\n"+
					"a garbage ARN matches no observed resource, so the row is unreachable either way; "+
					"rejecting it is what lets the caller name the cause as %s", row.ARN, stateResourceARNRedacted)
			}
		})
	}
}

// TestStateResourceDecodeFailureClass pins the label the redacted-join-key
// rejection exists to produce. The rejection itself changes no finding
// outcome -- a garbage ARN key and an absent row are indistinguishable to the
// observed-ARN lookup that reads stateByARN -- so this label IS the
// deliverable, and it was untested until this case.
//
// The false-positive half matters as much as the true-positive half: ordinary
// malformed payloads must keep the generic class, or the signal that says
// "your provider-schema bundle is broken" fires on every bad row and stops
// meaning anything.
func TestStateResourceDecodeFailureClass(t *testing.T) {
	t.Parallel()

	redactedARN := `{"marker":"redacted:hmac-sha256:` + strings.Repeat("0", 64) +
		`","reason":"unknown_provider_schema","source":"resources.*.attributes.arn"}`

	for _, tc := range []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "redacted arn",
			payload: `{"address":"a","type":"aws_instance","attributes":{"arn":` + redactedARN + `}}`,
			want:    stateResourceARNRedacted,
		},
		{
			name:    "real arn that failed for another reason",
			payload: `{"type":"aws_instance","attributes":{"arn":"arn:aws:ec2:us-east-1:1:instance/i-0"}}`,
			want:    stateResourceDecodeFailure,
		},
		{
			name:    "missing arn",
			payload: `{"address":"a","type":"aws_instance","attributes":{"ami":"ami-0"}}`,
			want:    stateResourceDecodeFailure,
		},
		{
			name:    "unparseable payload",
			payload: `{not json`,
			want:    stateResourceDecodeFailure,
		},
		{
			name:    "empty payload",
			payload: ``,
			want:    stateResourceDecodeFailure,
		},
		{
			name:    "arn is a map but not a marker",
			payload: `{"address":"a","attributes":{"arn":{"some":"object"}}}`,
			want:    stateResourceDecodeFailure,
		},
		{
			name: "arn string merely resembling marker text",
			payload: `{"address":"a","attributes":{"arn":"redacted:hmac-sha256:` +
				strings.Repeat("0", 64) + `"}}`,
			want: stateResourceDecodeFailure,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stateResourceDecodeFailureClass([]byte(tc.payload)); got != tc.want {
				t.Fatalf("stateResourceDecodeFailureClass(%s) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}
