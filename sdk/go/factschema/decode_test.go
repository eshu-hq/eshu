// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

func testEnvelope(payload map[string]any) Envelope {
	return Envelope{
		FactKind:         FactKindAWSResource,
		SchemaVersion:    "1.0.0",
		StableFactKey:    "arn:aws:s3:::example-bucket",
		ScopeID:          "aws-account:111111111111",
		GenerationID:     "gen-1",
		CollectorKind:    "aws-cloud-collector",
		SourceConfidence: "observed",
		ObservedAt:       time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		IsTombstone:      false,
		SourceRef:        "s3://example-bucket",
		Payload:          payload,
	}
}

func fullAWSResourcePayload() map[string]any {
	return map[string]any{
		"account_id":    "111111111111",
		"resource_id":   "arn:aws:s3:::example-bucket",
		"region":        "us-east-1",
		"resource_type": "aws.s3.bucket",
		"name":          "example-bucket",
		"tags":          map[string]any{"env": "prod"},
	}
}

// TestDecodeAWSResource_MissingRequiredField proves that a payload missing a
// required field ("region" is absent from the map, not merely empty) yields
// a classified error naming the field, never a zero-value struct. This is
// the accuracy backstop Contract System v1 §3.2 describes: a missing
// required field becomes an input_invalid dead letter, never a silent
// empty-string graph identity.
func TestDecodeAWSResource_MissingRequiredField(t *testing.T) {
	t.Parallel()

	payload := fullAWSResourcePayload()
	delete(payload, "region") // absent, not empty-string present

	got, err := DecodeAWSResource(testEnvelope(payload))
	if err == nil {
		t.Fatalf("DecodeAWSResource() error = nil, want non-nil for missing required field")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodeAWSResource() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	if classified.Field != "region" {
		t.Fatalf("Field = %q, want %q", classified.Field, "region")
	}

	var zero awsv1.Resource
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeAWSResource() returned non-zero struct %+v on error, want zero value", got)
	}
}

// TestDecodeAWSResource_MissingRequiredField_DistinguishesAbsentFromEmpty
// proves the "missing" classification fires only when the required JSON key
// is absent from the payload map, not merely present with an empty value —
// an empty string is a valid (if unusual) observed value and must decode
// successfully.
func TestDecodeAWSResource_MissingRequiredField_DistinguishesAbsentFromEmpty(t *testing.T) {
	t.Parallel()

	payload := fullAWSResourcePayload()
	payload["region"] = "" // present, but empty

	got, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil for present-but-empty required field", err)
	}
	if got.Region != "" {
		t.Fatalf("Region = %q, want empty string", got.Region)
	}
}

// TestDecodeAWSResource_NullRequiredField proves that a required key present
// with an explicit JSON null (Go nil in the payload map) is rejected as a
// classified error, not silently unmarshaled to a zero value. Without this,
// json.Unmarshal turns null into "" for a string field with no error — the
// exact silent-zero-value identity this module exists to prevent. This is
// distinct from an empty string, which is a valid observed value (see
// TestDecodeAWSResource_MissingRequiredField_DistinguishesAbsentFromEmpty).
func TestDecodeAWSResource_NullRequiredField(t *testing.T) {
	t.Parallel()

	payload := fullAWSResourcePayload()
	payload["region"] = nil // present, but explicit JSON null

	got, err := DecodeAWSResource(testEnvelope(payload))
	if err == nil {
		t.Fatalf("DecodeAWSResource() error = nil, want non-nil for null required field")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodeAWSResource() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	if classified.Field != "region" {
		t.Fatalf("Field = %q, want %q", classified.Field, "region")
	}

	var zero awsv1.Resource
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeAWSResource() returned non-zero struct %+v on error, want zero value", got)
	}
}

// TestDecodeAWSResource_RoundTrip proves that a typed struct encoded into an
// envelope payload map decodes back, via the kind-keyed seam, to a
// deep-equal copy of the original struct.
func TestDecodeAWSResource_RoundTrip(t *testing.T) {
	t.Parallel()

	name := "example-bucket"
	tags := map[string]string{"env": "prod"}
	original := awsv1.Resource{
		AccountID:    "111111111111",
		ResourceID:   "arn:aws:s3:::example-bucket",
		Region:       "us-east-1",
		ResourceType: "aws.s3.bucket",
		Name:         &name,
		Tags:         &tags,
	}

	payload, err := EncodeAWSResource(original)
	if err != nil {
		t.Fatalf("EncodeAWSResource() error = %v, want nil", err)
	}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSResource() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeAWSResource_RoundTrip_ObservedEmptyTags proves the "observed, no
// tags" state survives a round trip: a non-nil pointer to an empty map
// marshals as "tags":{} and decodes back to a non-nil pointer to an empty
// map, never collapsing to nil (which would be indistinguishable from "not
// observed"). This is the state the Tags pointer type exists to preserve.
func TestDecodeAWSResource_RoundTrip_ObservedEmptyTags(t *testing.T) {
	t.Parallel()

	emptyTags := map[string]string{}
	original := awsv1.Resource{
		AccountID:    "111111111111",
		ResourceID:   "arn:aws:s3:::example-bucket",
		Region:       "us-east-1",
		ResourceType: "aws.s3.bucket",
		Tags:         &emptyTags,
	}

	payload, err := EncodeAWSResource(original)
	if err != nil {
		t.Fatalf("EncodeAWSResource() error = %v, want nil", err)
	}
	if _, ok := payload["tags"]; !ok {
		t.Fatalf("EncodeAWSResource() omitted an observed empty tags map; payload = %v", payload)
	}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}
	if decoded.Tags == nil {
		t.Fatalf("Tags = nil, want non-nil pointer to empty map (observed empty must not collapse to not-observed)")
	}
	if len(*decoded.Tags) != 0 {
		t.Fatalf("*Tags = %v, want empty map", *decoded.Tags)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSResource() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeAWSResource_RoundTrip_OptionalFieldsAbsent proves the round trip
// also holds when optional fields are omitted entirely, leaving the decoded
// struct's pointer/map fields nil rather than defaulted.
func TestDecodeAWSResource_RoundTrip_OptionalFieldsAbsent(t *testing.T) {
	t.Parallel()

	original := awsv1.Resource{
		AccountID:    "111111111111",
		ResourceID:   "arn:aws:s3:::example-bucket",
		Region:       "us-east-1",
		ResourceType: "aws.s3.bucket",
	}

	payload, err := EncodeAWSResource(original)
	if err != nil {
		t.Fatalf("EncodeAWSResource() error = %v, want nil", err)
	}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}
	if decoded.Name != nil {
		t.Fatalf("Name = %v, want nil", decoded.Name)
	}
	if decoded.Tags != nil {
		t.Fatalf("Tags = %v, want nil", decoded.Tags)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSResource() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeAWSResource_UnsupportedMajor proves an unsupported schema-version
// major is a classified decode error, not a silent best-effort decode.
func TestDecodeAWSResource_UnsupportedMajor(t *testing.T) {
	t.Parallel()

	env := testEnvelope(fullAWSResourcePayload())
	env.SchemaVersion = "2.0.0"

	_, err := DecodeAWSResource(env)
	if err == nil {
		t.Fatalf("DecodeAWSResource() error = nil, want non-nil for unsupported major")
	}
	if !errors.Is(err, ErrUnsupportedSchemaMajor) {
		t.Fatalf("DecodeAWSResource() error = %v, want errors.Is ErrUnsupportedSchemaMajor", err)
	}
}

// TestRequiredFieldsAreNonPointerAndOptionalFieldsArePointerOrOmitEmpty
// asserts, by reflection over the struct tags, the required/optional
// contract documented on awsv1.Resource: required fields are non-pointer
// with no omitempty tag, optional fields are pointer or omitempty.
func TestRequiredFieldsAreNonPointerAndOptionalFieldsArePointerOrOmitEmpty(t *testing.T) {
	t.Parallel()

	wantRequired := map[string]bool{
		"account_id":    true,
		"resource_id":   true,
		"region":        true,
		"resource_type": true,
		"name":          false,
		"tags":          false,
	}

	typ := reflect.TypeOf(awsv1.Resource{})
	seen := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		jsonName, hasOmitEmpty := parseJSONTag(tag)
		seen[jsonName] = true

		isPointerOrMap := field.Type.Kind() == reflect.Ptr || field.Type.Kind() == reflect.Map
		required := !isPointerOrMap && !hasOmitEmpty

		want, ok := wantRequired[jsonName]
		if !ok {
			t.Fatalf("unexpected field %q on awsv1.Resource, update the test's expectations", jsonName)
		}
		if required != want {
			t.Fatalf("field %q required = %v, want %v", jsonName, required, want)
		}
	}
	for name := range wantRequired {
		if !seen[name] {
			t.Fatalf("expected field %q not found on awsv1.Resource", name)
		}
	}
}

// TestRequiredFieldsMatchStructShape locks decode.go's requiredFields map to
// awsv1.Resource's actual struct shape. requiredFields drives decodeAndValidate's
// missing-field check; if it drifts from the struct — for example a new
// required (non-pointer, non-omitempty) field is added to the struct but not
// to requiredFields — decodeAndValidate silently skips validating that field
// and decodes its absence to a zero value, the exact silent-zero-value
// failure this module exists to prevent. This test computes the required set
// by reflection over the struct and asserts it equals the map entry, so that
// drift is a test failure rather than a latent accuracy hole.
func TestRequiredFieldsMatchStructShape(t *testing.T) {
	t.Parallel()

	want := map[string]bool{}
	typ := reflect.TypeOf(awsv1.Resource{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonName, hasOmitEmpty := parseJSONTag(field.Tag.Get("json"))
		isPointerOrMap := field.Type.Kind() == reflect.Ptr || field.Type.Kind() == reflect.Map
		if !isPointerOrMap && !hasOmitEmpty {
			want[jsonName] = true
		}
	}

	got := map[string]bool{}
	for _, name := range requiredFields[FactKindAWSResource] {
		if got[name] {
			t.Fatalf("requiredFields[%q] lists %q more than once", FactKindAWSResource, name)
		}
		got[name] = true
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requiredFields[%q] = %v, want %v (derived from awsv1.Resource struct shape); update decode.go's requiredFields to match the struct", FactKindAWSResource, sortedKeys(got), sortedKeys(want))
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestParseJSONTag covers the tag-splitting helper the reflection lock tests
// rely on, including multi-option tags where omitempty is not the last option
// (json.Marshal accepts options in any order) and the skip tag "-".
func TestParseJSONTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tag           string
		wantName      string
		wantOmitEmpty bool
	}{
		{tag: "name", wantName: "name", wantOmitEmpty: false},
		{tag: "name,omitempty", wantName: "name", wantOmitEmpty: true},
		{tag: "name,omitempty,string", wantName: "name", wantOmitEmpty: true},
		{tag: "name,string,omitempty", wantName: "name", wantOmitEmpty: true},
		{tag: "-", wantName: "", wantOmitEmpty: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()

			name, omitEmpty := parseJSONTag(tc.tag)
			if name != tc.wantName {
				t.Fatalf("parseJSONTag(%q) name = %q, want %q", tc.tag, name, tc.wantName)
			}
			if omitEmpty != tc.wantOmitEmpty {
				t.Fatalf("parseJSONTag(%q) omitEmpty = %v, want %v", tc.tag, omitEmpty, tc.wantOmitEmpty)
			}
		})
	}
}

// parseJSONTag splits a struct json tag into its field name and whether it
// carries the omitempty option. The tag is a comma-separated list whose first
// element is the field name and whose remaining elements are options in any
// order (json.Marshal does not require omitempty to be last). The skip tag "-"
// yields an empty name.
func parseJSONTag(tag string) (name string, omitEmpty bool) {
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "-" {
		name = ""
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty
}
