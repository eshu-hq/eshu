package terraformstate_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// stubCompositeCaptureRecorder counts recorded composite-capture skips so
// tests can verify the parser exercises the slog+counter telemetry pair the
// ADR commits to without standing up a real OTEL meter.
type stubCompositeCaptureRecorder struct {
	calls int64
	last  terraformstate.CompositeCaptureSkip
}

func (s *stubCompositeCaptureRecorder) Record(_ context.Context, skip terraformstate.CompositeCaptureSkip) {
	atomic.AddInt64(&s.calls, 1)
	s.last = skip
}

// TestParserCapturesSchemaKnownCompositeAttribute is the load-bearing positive
// test for the streaming nested walker. With a SchemaResolver that recognizes
// the SSE composite chain on aws_s3_bucket, the parser captures the nested
// dot-path value that the loader's flattenStateAttributes
// (storage/postgres/tfstate_drift_evidence.go) flattens into
// "server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm".
//
// Without composite capture, the value is dropped at readScalarOrSkip in
// json_token.go and bucket E (attribute_drift) in the Tier-2 verifier cannot
// fire because the drift handler has no state-side leaf to compare.
func TestParserCapturesSchemaKnownCompositeAttribute(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "server_side_encryption_configuration"},
	)

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"logs",
			"instances":[{
				"attributes":{
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"AES256"}]}]}
					]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	composite, ok := attributes["server_side_encryption_configuration"].([]any)
	if !ok {
		t.Fatalf("attributes[server_side_encryption_configuration] = %#v, want []any (nested-singleton-array shape)", attributes["server_side_encryption_configuration"])
	}
	if got, want := len(composite), 1; got != want {
		t.Fatalf("len(composite) = %d, want %d (singleton block)", got, want)
	}
	rule, ok := composite[0].(map[string]any)
	if !ok {
		t.Fatalf("composite[0] = %#v, want map[string]any", composite[0])
	}
	ruleList, ok := rule["rule"].([]any)
	if !ok || len(ruleList) != 1 {
		t.Fatalf("rule[rule] = %#v, want []any of length 1", rule["rule"])
	}
	ruleMap, ok := ruleList[0].(map[string]any)
	if !ok {
		t.Fatalf("ruleList[0] = %#v, want map[string]any", ruleList[0])
	}
	applyList, ok := ruleMap["apply_server_side_encryption_by_default"].([]any)
	if !ok || len(applyList) != 1 {
		t.Fatalf("ruleMap[apply_server_side_encryption_by_default] = %#v, want []any of length 1", ruleMap["apply_server_side_encryption_by_default"])
	}
	applyMap, ok := applyList[0].(map[string]any)
	if !ok {
		t.Fatalf("applyList[0] = %#v, want map[string]any", applyList[0])
	}
	if got, want := applyMap["sse_algorithm"], "AES256"; got != want {
		t.Fatalf("applyMap[sse_algorithm] = %#v, want %q", got, want)
	}
}

// TestParserSkipsUnknownCompositeAttribute is the load-bearing negative test
// for the walker. When a composite key is NOT registered with the
// SchemaResolver, the parser must keep skipping it via skipNested so the
// fail-closed default holds for non-schema-known paths and the streaming
// memory invariant is preserved.
func TestParserSkipsUnknownCompositeAttribute(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "acl"},
	)

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"logs",
			"instances":[{
				"attributes":{
					"acl":"private",
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"AES256"}]}]}
					]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	if _, present := attributes["server_side_encryption_configuration"]; present {
		t.Fatalf("attributes[server_side_encryption_configuration] should be absent for SchemaUnknown composite, got %#v", attributes["server_side_encryption_configuration"])
	}
	if got, want := attributes["acl"], "private"; got != want {
		t.Fatalf("attributes[acl] = %#v, want %q (sibling scalar preserved)", got, want)
	}
}

// TestParserRedactsSensitiveLeafInsideSchemaKnownComposite is the per-leaf
// sensitive-key proof committed to by the ADR's "Nested sensitive key inside
// a SchemaKnown composite" edge case. The walker must call Classify on each
// scalar leaf so a "password" segment inside aws_iam_user.login_profile is
// redacted at the leaf instead of the whole composite being dropped.
//
// The walker uses redact.RuleSet.isSensitiveSource segment matching, which
// already handles dotted source paths (redact/policy.go:160-178).
func TestParserRedactsSensitiveLeafInsideSchemaKnownComposite(t *testing.T) {
	t.Parallel()

	// The walker uses redact.RuleSet.isSensitiveSource segment matching:
	// the leaf source resources.<addr>.attributes.login_profile.password
	// matches "password" via segment regardless of nesting depth, so the
	// whole composite passes through but the leaf is HMAC-stamped.
	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_iam_user", "login_profile"},
	)

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_iam_user",
			"name":"app",
			"instances":[{
				"attributes":{
					"login_profile":[
						{"password":"plain-text-password","password_reset_required":false}
					]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	assertNoRawSecret(t, result.Facts, "plain-text-password")

	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	loginProfile, ok := attributes["login_profile"].([]any)
	if !ok || len(loginProfile) != 1 {
		t.Fatalf("attributes[login_profile] = %#v, want []any of length 1", attributes["login_profile"])
	}
	leafMap, ok := loginProfile[0].(map[string]any)
	if !ok {
		t.Fatalf("loginProfile[0] = %#v, want map[string]any", loginProfile[0])
	}
	marker, ok := leafMap["password"].(map[string]any)
	if !ok {
		t.Fatalf("leafMap[password] = %#v, want redaction marker map", leafMap["password"])
	}
	if value, _ := marker["marker"].(string); !strings.HasPrefix(value, "redacted:hmac-sha256:") {
		t.Fatalf("password marker = %#v, want HMAC redaction marker", marker["marker"])
	}
	if got, want := leafMap["password_reset_required"], false; got != want {
		t.Fatalf("leafMap[password_reset_required] = %#v, want %v (non-sensitive sibling preserved)", got, want)
	}
}

// TestParserDropsSensitiveCompositeBeforeWalking guards the parser boundary
// before the streaming nested walker runs. A schema-known composite whose
// source path is classified as sensitive must be dropped from the decoder
// stream without first materializing the raw subtree. The source-path form here
// matches the parser's pre-address composite source because the final resource
// address is not available until the whole instance has been read.
func TestParserDropsSensitiveCompositeBeforeWalking(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	rules, err := redact.NewRuleSet("test-schema", []string{
		"resources.*.attributes.secret_block",
	})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}
	options.RedactionRules = rules
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_iam_user", "secret_block"},
	)

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_iam_user",
			"name":"app",
			"instances":[{
				"attributes":{
					"secret_block":{"token":"plain-text-token"}
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	assertNoRawSecret(t, result.Facts, "plain-text-token")

	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	if _, present := attributes["secret_block"]; present {
		t.Fatalf("attributes[secret_block] present = %#v, want dropped sensitive composite", attributes["secret_block"])
	}
	if got := result.RedactionsApplied[redact.ReasonKnownSensitiveKey]; got == 0 {
		t.Fatalf("RedactionsApplied[%s] = 0, want sensitive composite drop recorded", redact.ReasonKnownSensitiveKey)
	}
}

// TestParserPreservesMultiElementCompositeForDownstreamFlattenerTruncation is
// the ambiguous-case proof. When a schema-known repeated block has multiple
// elements (e.g., aws_security_group.ingress), the walker emits the full array
// and lets PR #198's loader-level first-wins truncation handle the
// multi-element case downstream. The parser does not pre-truncate; the
// truncation log fires from storage/postgres/tfstate_drift_evidence_state_row.go
// with multi_element.source="state_flatten".
//
// This regression locks in the layered-truncation contract called out as
// resolved question Q5 in the ADR.
func TestParserPreservesMultiElementCompositeForDownstreamFlattenerTruncation(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_security_group", "ingress"},
	)

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_security_group",
			"name":"web",
			"instances":[{
				"attributes":{
					"ingress":[
						{"from_port":80,"to_port":80},
						{"from_port":443,"to_port":443}
					]
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	ingress, ok := attributes["ingress"].([]any)
	if !ok {
		t.Fatalf("attributes[ingress] = %#v, want []any (multi-element repeated block)", attributes["ingress"])
	}
	if got, want := len(ingress), 2; got != want {
		t.Fatalf("len(ingress) = %d, want %d (parser preserves all elements; loader truncates downstream)", got, want)
	}
}

// TestParserRecordsCounterOnSchemaUnknownComposite confirms the observability
// surface called out in the ADR's risks section: when state JSON ships a
// composite (object or array) for an attribute the loaded
// ProviderSchemaResolver does not know about, the parser must record the skip
// via the CompositeCaptureRecorder so operators can see "schema bundle is
// behind reality" without scanning logs. Without this counter, a new
// provider-version nested block would silently regress bucket E
// (attribute_drift) for that attribute until somebody refreshed the bundle.
func TestParserRecordsCounterOnSchemaUnknownComposite(t *testing.T) {
	t.Parallel()

	recorder := &stubCompositeCaptureRecorder{}
	options := parseFixtureOptions(t)
	// Resolver knows acl but NOT server_side_encryption_configuration; the
	// SSE composite still arrives in the state JSON, triggering the
	// schema-drift telemetry path.
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "acl"},
	)
	options.CompositeCaptureMetrics = recorder

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"logs",
			"instances":[{
				"attributes":{
					"acl":"private",
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"AES256"}]}]}
					]
				}
			}]
		}]
	}`

	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options); err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if got := atomic.LoadInt64(&recorder.calls); got == 0 {
		t.Fatalf("recorder.calls = 0, want >= 1 (parser must record SchemaUnknown composite skip)")
	}
	if got, want := recorder.last.ResourceType, "aws_s3_bucket"; got != want {
		t.Fatalf("recorder.last.ResourceType = %q, want %q", got, want)
	}
	if got, want := recorder.last.AttributeKey, "server_side_encryption_configuration"; got != want {
		t.Fatalf("recorder.last.AttributeKey = %q, want %q", got, want)
	}
	if got, want := recorder.last.Reason, terraformstate.CompositeCaptureSkipReasonSchemaUnknown; got != want {
		t.Fatalf("recorder.last.Reason = %q, want %q", got, want)
	}
}

func TestParserRecordsWalkerErrorReasonOnMalformedSchemaKnownComposite(t *testing.T) {
	t.Parallel()

	recorder := &stubCompositeCaptureRecorder{}
	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "server_side_encryption_configuration"},
	)
	options.CompositeCaptureMetrics = recorder

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"logs",
			"instances":[{
				"attributes":{
					"server_side_encryption_configuration":[
						{"rule":[{"apply_server_side_encryption_by_default":[{"sse_algorithm":"AES256"}]}]}
				}
			}]
		}]
	}`

	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options); err == nil {
		t.Fatal("Parse() error = nil, want malformed composite error")
	}
	if got := atomic.LoadInt64(&recorder.calls); got == 0 {
		t.Fatalf("recorder.calls = 0, want walker error recorded")
	}
	if got, want := recorder.last.Reason, terraformstate.CompositeCaptureSkipReasonWalkerError; got != want {
		t.Fatalf("recorder.last.Reason = %q, want %q", got, want)
	}
}
