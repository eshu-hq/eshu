// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"testing"
)

// Identity-side coverage for the terraform-state redaction contract. Kept apart
// from the value-attribute tests because the failure modes differ in kind: a
// redacted VALUE costs one comparison, while a redacted JOIN KEY removes the
// declared row from correlation entirely (#5859, #5870).
// TestStateRowFromPayloadRejectsARedactedARN covers the identity half of the
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
// It matches no AWS-observed ARN, so the declared side does not merely lose its
// comparable attributes -- it vanishes from the join, and every cloud resource
// under that broken bundle reclassifies as orphaned_cloud_resource. A false
// "this resource is unmanaged" across an entire account is worse than a missing
// value comparison, and nothing surfaces it (#5870).
//
// Rejecting the row is the correct answer rather than passing the marker
// through: an unusable join key is exactly the condition the empty-ARN guard
// already exists for.
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
		name: "aws_route",
		payload: map[string]any{
			"address":    "module.ecs.aws_instance.supply-chain-demo",
			"type":       "aws_instance",
			"attributes": map[string]any{"arn": redactedARN, "ami": "ami-declared"},
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
					"a garbage ARN matches no observed resource, so the declared row leaves the join "+
					"entirely and every cloud resource under it reclassifies as orphaned", row.ARN)
			}
		})
	}
}
