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
// Until #5870, LoadPackagedSchemaResolver returned (nil, nil) when no
// provider-schema bundle parsed -- success carrying a nil resolver, not an
// error -- and parseSchemaInto swallows per-file parse failures, so a corrupt
// or empty bundle silently yielded that nil. schemaTrust then answers
// SchemaUnknown for EVERY (resourceType, attributeKey) pair with no exemption,
// so the terraform-state parser fail-closed-redacts every scalar, "arn"
// included.
//
// That constructor now returns an error instead, so the empty-bundle route to a
// nil resolver is closed. This test still pins the behaviour, because
// SchemaUnknown is the answer for any pair a resolver does not carry -- not
// only for a nil resolver -- so a redacted "arn" stays reachable.
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
// TestAWSRuntimeStateRowFromPayloadFailureClass below pins the label it produces.
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
			row, ok, failureClass := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", payload)
			if ok {
				t.Fatalf("awsRuntimeStateRowFromPayload() accepted a redacted ARN as the join key: ARN=%q\n"+
					"a garbage ARN matches no observed resource, so the row is unreachable either way; "+
					"rejecting it is what lets the caller name the cause as %s", row.ARN, stateResourceARNRedacted)
			}
			if failureClass != stateResourceARNRedacted {
				t.Fatalf("awsRuntimeStateRowFromPayload() failureClass = %q, want %q", failureClass, stateResourceARNRedacted)
			}
		})
	}
}

// TestAWSRuntimeStateRowFromPayloadFailureClass pins the label
// awsRuntimeStateRowFromPayload returns alongside ok=false for the caller's
// WARN log. It was previously covered indirectly through a standalone
// stateResourceDecodeFailureClass(payload []byte) helper that re-unmarshaled
// the payload a second time; that helper was folded into the decoder itself
// (Copilot review on #5904) so the hot path of a broken-provider-schema-bundle
// run -- where EVERY state row takes the !ok branch -- does not pay for a
// second json.Unmarshal per row. This table now calls
// awsRuntimeStateRowFromPayload directly, the seam the classification
// actually runs on, rather than asserting against a copy of the logic the
// production path no longer executes.
//
// The false-positive half matters as much as the true-positive half: ordinary
// malformed payloads must keep the generic class, or the signal that says
// "your provider-schema bundle is broken" fires on every bad row and stops
// meaning anything.
func TestAWSRuntimeStateRowFromPayloadFailureClass(t *testing.T) {
	t.Parallel()

	redactedARN := `{"marker":"redacted:hmac-sha256:` + strings.Repeat("0", 64) +
		`","reason":"unknown_provider_schema","source":"resources.*.attributes.arn"}`

	for _, tc := range []struct {
		name        string
		address     string
		payload     string
		wantOK      bool
		wantFailure string
	}{
		{
			name:        "redacted arn",
			address:     "a",
			payload:     `{"address":"a","type":"aws_instance","attributes":{"arn":` + redactedARN + `}}`,
			wantOK:      false,
			wantFailure: stateResourceARNRedacted,
		},
		{
			// No "address" field in the payload AND a blank address argument:
			// the row is unusable for a reason unrelated to redaction, so this
			// must keep the generic class rather than being swept into
			// state_resource_arn_redacted.
			name:        "real arn but blank address",
			address:     "",
			payload:     `{"type":"aws_instance","attributes":{"arn":"arn:aws:ec2:us-east-1:1:instance/i-0"}}`,
			wantOK:      false,
			wantFailure: stateResourceDecodeFailure,
		},
		{
			name:        "missing arn",
			address:     "a",
			payload:     `{"address":"a","type":"aws_instance","attributes":{"ami":"ami-0"}}`,
			wantOK:      false,
			wantFailure: stateResourceDecodeFailure,
		},
		{
			name:        "unparseable payload",
			address:     "a",
			payload:     `{not json`,
			wantOK:      false,
			wantFailure: stateResourceDecodeFailure,
		},
		{
			name:        "empty payload",
			address:     "",
			payload:     ``,
			wantOK:      false,
			wantFailure: stateResourceDecodeFailure,
		},
		{
			// A map-shaped "arn" that lacks the marker/reason/source shape is
			// NOT a redaction marker (redact.IsRedactedValue says false) and is
			// not otherwise rejected -- it decodes successfully with a
			// fmt.Sprint-rendered garbage ARN. Proving that case succeeds is
			// what makes the redacted-arn rejection above a true positive and
			// not an accidental "any map in the arn slot fails" rule.
			name:    "arn is a map but not a marker",
			address: "a",
			payload: `{"address":"a","attributes":{"arn":{"some":"object"}}}`,
			wantOK:  true,
		},
		{
			// A plain string that merely starts with the marker prefix is not
			// the shape IsRedactedValue matches (it requires a decoded JSON
			// object), so it is carried as a literal ARN value, not rejected.
			name:    "arn string merely resembling marker text",
			address: "a",
			payload: `{"address":"a","attributes":{"arn":"redacted:hmac-sha256:` +
				strings.Repeat("0", 64) + `"}}`,
			wantOK: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row, ok, failureClass := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", tc.address, []byte(tc.payload))
			if ok != tc.wantOK {
				t.Fatalf("awsRuntimeStateRowFromPayload(%s) ok = %v (row=%#v), want %v", tc.payload, ok, row, tc.wantOK)
			}
			if !tc.wantOK && failureClass != tc.wantFailure {
				t.Fatalf("awsRuntimeStateRowFromPayload(%s) failureClass = %q, want %q", tc.payload, failureClass, tc.wantFailure)
			}
		})
	}
}
