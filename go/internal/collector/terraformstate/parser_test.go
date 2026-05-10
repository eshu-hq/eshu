package terraformstate_test

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestParserStreamsTerraformStateIntoRedactedFacts(t *testing.T) {
	t.Parallel()

	key, err := redact.NewKey([]byte("test-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	rules, err := redact.NewRuleSet("test-schema", []string{"password"})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope(
		"repo-scope-123",
		"s3",
		"s3://tfstate-prod/services/api/terraform.tfstate",
		nil,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	observedAt := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	generation, err := scope.NewTerraformStateSnapshotGeneration(
		scopeValue.ScopeID,
		17,
		"lineage-123",
		observedAt,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}

	state := `{
		"format_version": "1.0",
		"terraform_version": "1.9.8",
		"serial": 17,
		"lineage": "lineage-123",
		"outputs": {
			"db_password": {
				"sensitive": true,
				"value": "super-secret"
			}
		},
		"resources": [{
			"mode": "managed",
			"type": "aws_db_instance",
			"name": "main",
			"provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
			"instances": [{
				"attributes": {
					"arn": "arn:aws:rds:us-east-1:123456789012:db:main",
					"password": "plain-db-password",
					"tags": {"Name": "main"}
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), terraformstate.ParseOptions{
		Scope:          scopeValue,
		Generation:     generation,
		Source:         terraformstate.StateKey{BackendKind: terraformstate.BackendS3, Locator: "s3://tfstate-prod/services/api/terraform.tfstate"},
		ObservedAt:     observedAt,
		RedactionKey:   key,
		RedactionRules: rules,
		FencingToken:   42,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	requireFactKinds(t, result.Facts,
		facts.TerraformStateSnapshotFactKind,
		facts.TerraformStateOutputFactKind,
		facts.TerraformStateResourceFactKind,
		facts.TerraformStateWarningFactKind,
	)
	assertNoRawSecret(t, result.Facts, "super-secret")
	assertNoRawSecret(t, result.Facts, "plain-db-password")
	assertNoRawSecret(t, result.Facts, "s3://tfstate-prod/services/api/terraform.tfstate")
	assertNoRawSecretInRefs(t, result.Facts, "s3://tfstate-prod/services/api/terraform.tfstate")

	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	if got, want := resource.CollectorKind, string(scope.CollectorTerraformState); got != want {
		t.Fatalf("resource CollectorKind = %q, want %q", got, want)
	}
	if got, want := resource.SourceConfidence, facts.SourceConfidenceObserved; got != want {
		t.Fatalf("resource SourceConfidence = %q, want %q", got, want)
	}
	if got, want := resource.SchemaVersion, facts.TerraformStateResourceSchemaVersion; got != want {
		t.Fatalf("resource SchemaVersion = %q, want %q", got, want)
	}
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	password, ok := attributes["password"].(map[string]any)
	if !ok {
		t.Fatalf("attributes[password] = %#v, want redaction marker map", attributes["password"])
	}
	if marker, ok := password["marker"].(string); !ok || !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("password marker = %#v, want redacted marker", password["marker"])
	}
	if _, ok := attributes["tags"]; ok {
		t.Fatalf("attributes[tags] present = %#v, want dropped composite", attributes["tags"])
	}

	output := factByKind(t, result.Facts, facts.TerraformStateOutputFactKind)
	value, ok := output.Payload["value"].(map[string]any)
	if !ok {
		t.Fatalf("output value = %#v, want redaction marker map", output.Payload["value"])
	}
	if marker, ok := value["marker"].(string); !ok || !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("output marker = %#v, want redacted marker", value["marker"])
	}
}

func TestParserFactKeysAreStableAcrossResourceOrder(t *testing.T) {
	t.Parallel()

	first := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"id":"i-1"}}]},
		{"mode":"managed","type":"aws_instance","name":"worker","instances":[{"attributes":{"id":"i-2"}}]}
	]}`
	second := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"worker","instances":[{"attributes":{"id":"i-2"}}]},
		{"mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"id":"i-1"}}]}
	]}`

	firstFacts := parseFixtureFacts(t, first)
	secondFacts := parseFixtureFacts(t, second)

	if got, want := stableKeysByKind(firstFacts, facts.TerraformStateResourceFactKind), stableKeysByKind(secondFacts, facts.TerraformStateResourceFactKind); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("resource stable keys changed with order:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestParserRejectsInvalidEnvelopeContext(t *testing.T) {
	t.Parallel()

	key, err := redact.NewKey([]byte("test-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	_, err = terraformstate.Parse(context.Background(), strings.NewReader(`{"serial":17,"lineage":"lineage-123"}`), terraformstate.ParseOptions{
		RedactionKey: key,
	})
	if err == nil {
		t.Fatal("Parse() error = nil, want non-nil")
	}
}

func TestParserRejectsSerialLineageMismatch(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	_, err := terraformstate.Parse(context.Background(), strings.NewReader(`{"serial":16,"lineage":"lineage-123"}`), options)
	if err == nil {
		t.Fatal("Parse() serial mismatch error = nil, want non-nil")
	}
	_, err = terraformstate.Parse(context.Background(), strings.NewReader(`{"serial":17,"lineage":"lineage-456"}`), options)
	if err == nil {
		t.Fatal("Parse() lineage mismatch error = nil, want non-nil")
	}
}

func TestParserHashesInstanceIndexKeysInPersistedIdentity(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[{
		"mode":"managed",
		"type":"aws_instance",
		"name":"tenant",
		"instances":[{
			"index_key":"tenant@example.com",
			"attributes":{"id":"i-1"}
		}]
	}]}`

	result := parseFixtureFacts(t, state)
	assertNoRawSecret(t, result, "tenant@example.com")
	assertNoRawSecretInRefs(t, result, "tenant@example.com")
}

func TestParserSkipsLargeIgnoredTopLevelFields(t *testing.T) {
	t.Parallel()

	state := `{"checks":[` + strings.Repeat(`{"status":"pass","payload":["x","y","z"]},`, 1024) + `{"status":"pass"}],"serial":17,"lineage":"lineage-123"}`
	result := parseFixtureFacts(t, state)

	requireFactKinds(t, result, facts.TerraformStateSnapshotFactKind)
}

func TestParserRejectsTrailingBytes(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123"} trailing`
	_, err := terraformstate.Parse(context.Background(), strings.NewReader(state), parseFixtureOptions(t))
	if err == nil {
		t.Fatal("Parse() trailing bytes error = nil, want non-nil")
	}
}

func TestParserSurfacesReadLimitAfterCompleteJSONObject(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123"}`
	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket:   "tfstate-prod",
		Key:      "state.tfstate",
		Region:   "us-east-1",
		MaxBytes: int64(len(state)),
		Client: &recordingS3Client{
			output: terraformstate.S3GetObjectOutput{
				Body: io.NopCloser(strings.NewReader(state + strings.Repeat(" ", 8))),
				Size: int64(len(state)),
			},
		},
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}
	reader, _, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer closeReader(t, reader)

	_, err = terraformstate.Parse(context.Background(), reader, parseFixtureOptions(t))
	if !errors.Is(err, terraformstate.ErrStateTooLarge) {
		t.Fatalf("Parse() error = %v, want ErrStateTooLarge", err)
	}
}

func TestParserRejectsInstancesBeforeResourceIdentity(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[{
		"instances":[{"attributes":{"id":"i-1"}}],
		"mode":"managed",
		"type":"aws_instance",
		"name":"api"
	}]}`
	_, err := terraformstate.Parse(context.Background(), strings.NewReader(state), parseFixtureOptions(t))
	if err == nil {
		t.Fatal("Parse() instances-before-identity error = nil, want non-nil")
	}
}

func TestParserUsesIndexHashWhenAttributesPrecedeIndexKey(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[{
		"mode":"managed",
		"type":"aws_instance",
		"name":"tenant",
		"instances":[{
			"attributes":{"id":"i-1","password":"secret"},
			"index_key":"tenant@example.com"
		}]
	}]}`

	result := parseFixtureFacts(t, state)
	resource := factByKind(t, result, facts.TerraformStateResourceFactKind)
	address, ok := resource.Payload["address"].(string)
	if !ok {
		t.Fatalf("resource address = %#v, want string", resource.Payload["address"])
	}
	if !strings.Contains(address, "[key:") {
		t.Fatalf("resource address = %q, want hashed index key", address)
	}
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	password, ok := attributes["password"].(map[string]any)
	if !ok {
		t.Fatalf("attributes[password] = %#v, want redaction marker map", attributes["password"])
	}
	source, ok := password["source"].(string)
	if !ok {
		t.Fatalf("password source = %#v, want string", password["source"])
	}
	if !strings.Contains(source, address) {
		t.Fatalf("password source = %q, want resource address %q", source, address)
	}
	assertNoRawSecret(t, result, "tenant@example.com")
	assertNoRawSecretInRefs(t, result, "tenant@example.com")
}

func TestParserRejectsMalformedResourceIdentity(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[{
		"mode":"managed",
		"type":"",
		"name":"api",
		"instances":[{"attributes":{"id":"i-1"}}]
	}]}`
	_, err := terraformstate.Parse(context.Background(), strings.NewReader(state), parseFixtureOptions(t))
	if err == nil {
		t.Fatal("Parse() malformed resource identity error = nil, want non-nil")
	}
}

func TestParserRejectsMalformedState(t *testing.T) {
	t.Parallel()

	_, err := terraformstate.Parse(context.Background(), strings.NewReader(`{"resources":[`), parseFixtureOptions(t))
	if err == nil {
		t.Fatal("Parse() error = nil, want non-nil")
	}
}

func parseFixtureFacts(t *testing.T, state string) []facts.Envelope {
	t.Helper()

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), parseFixtureOptions(t))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	return result.Facts
}

func parseFixtureOptions(t testing.TB) terraformstate.ParseOptions {
	t.Helper()

	key, err := redact.NewKey([]byte("test-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	rules, err := redact.NewRuleSet("test-schema", []string{"password"})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope("repo-scope-123", "s3", "s3://tfstate-prod/services/api/terraform.tfstate", nil)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	observedAt := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	generation, err := scope.NewTerraformStateSnapshotGeneration(scopeValue.ScopeID, 17, "lineage-123", observedAt)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	return terraformstate.ParseOptions{
		Scope:          scopeValue,
		Generation:     generation,
		Source:         terraformstate.StateKey{BackendKind: terraformstate.BackendS3, Locator: "s3://tfstate-prod/services/api/terraform.tfstate"},
		ObservedAt:     observedAt,
		RedactionKey:   key,
		RedactionRules: rules,
		FencingToken:   42,
	}
}

func TestParserRejectsMissingRedactionKey(t *testing.T) {
	t.Parallel()

	state := `{"outputs":{"secret":{"sensitive":true,"value":"secret"}}}`

	_, err := terraformstate.Parse(context.Background(), strings.NewReader(state), terraformstate.ParseOptions{})
	if err == nil {
		t.Fatal("Parse() error = nil, want non-nil")
	}
}

func requireFactKinds(t *testing.T, got []facts.Envelope, want ...string) {
	t.Helper()

	seen := map[string]bool{}
	for _, envelope := range got {
		seen[envelope.FactKind] = true
	}
	for _, kind := range want {
		if !seen[kind] {
			t.Fatalf("missing fact kind %q in %#v", kind, got)
		}
	}
}

func factByKind(t *testing.T, envelopes []facts.Envelope, kind string) facts.Envelope {
	t.Helper()

	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			return envelope
		}
	}
	t.Fatalf("missing fact kind %q", kind)
	return facts.Envelope{}
}

func assertNoRawSecret(t *testing.T, envelopes []facts.Envelope, secret string) {
	t.Helper()

	for _, envelope := range envelopes {
		if strings.Contains(envelope.FactID, secret) ||
			strings.Contains(envelope.StableFactKey, secret) ||
			payloadContains(envelope.Payload, secret) {
			t.Fatalf("secret %q leaked in envelope %#v", secret, envelope)
		}
	}
}

func assertNoRawSecretInRefs(t *testing.T, envelopes []facts.Envelope, secret string) {
	t.Helper()

	for _, envelope := range envelopes {
		if strings.Contains(envelope.SourceRef.SourceURI, secret) ||
			strings.Contains(envelope.SourceRef.SourceRecordID, secret) {
			t.Fatalf("secret %q leaked in source ref %#v", secret, envelope.SourceRef)
		}
	}
}

func stableKeysByKind(envelopes []facts.Envelope, kind string) []string {
	keys := []string{}
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			keys = append(keys, envelope.StableFactKey)
		}
	}
	sort.Strings(keys)
	return keys
}

func payloadContains(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case map[string]any:
		for _, nested := range typed {
			if payloadContains(nested, needle) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if payloadContains(nested, needle) {
				return true
			}
		}
	}
	return false
}
