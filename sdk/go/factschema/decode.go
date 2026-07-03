// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

//go:generate go run ./internal/schemagen/cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// Fact kind identifiers this module knows how to decode. A fact kind string
// is namespaced and stable across schema-version majors; only the payload
// shape changes between majors, handled by the switch inside each
// kind-specific Decode function (Contract System v1 §3.2).
const (
	// FactKindAWSResource is the sample fact kind this scaffold decodes end
	// to end.
	FactKindAWSResource = "aws.resource"
)

// Classification values a DecodeError carries. These are this module's own
// string constants, matched by value rather than imported from
// go/internal/projector's dead-letter triage classes, so the contracts
// module stays free of go/internal imports. The reducer maps
// ClassificationInputInvalid to its own TriageClassInputInvalid by string
// value.
const (
	// ClassificationInputInvalid marks a payload that failed required-field
	// validation on decode: a required key was absent, or the payload could
	// not be unmarshaled into the target struct's shape at all. A reducer
	// handler receiving this classification MUST dead-letter the fact
	// rather than proceed with a zero-value struct.
	ClassificationInputInvalid = "input_invalid"
)

// ErrUnsupportedSchemaMajor is returned (wrapped in a *DecodeError) when an
// envelope's SchemaVersion major has no known decode path for the fact
// kind. Test with errors.Is.
var ErrUnsupportedSchemaMajor = errors.New("factschema: unsupported schema version major")

// DecodeError is the classified, typed error decodeAndValidate and the
// kind-keyed Decode functions return for any payload that fails decode or
// required-field validation. Callers MUST check for *DecodeError (for
// example with errors.As) rather than treating a non-nil error generically,
// so the classification and missing field name survive to the reducer's
// dead-letter path.
type DecodeError struct {
	// FactKind is the fact kind being decoded when the error occurred.
	FactKind string
	// Classification is one of this package's Classification* constants.
	Classification string
	// Field is the JSON payload key that was missing or invalid. Empty when
	// the error is not attributable to a single field (for example an
	// unsupported schema major).
	Field string
	// Err is the underlying cause, when one exists (for example a
	// json.Unmarshal error). May be nil.
	Err error
}

// Error implements the error interface.
func (e *DecodeError) Error() string {
	var b strings.Builder
	b.WriteString("factschema: ")
	b.WriteString(e.Classification)
	b.WriteString(": fact kind ")
	b.WriteString(strconv.Quote(e.FactKind))
	if e.Field != "" {
		b.WriteString(": missing required field ")
		b.WriteString(strconv.Quote(e.Field))
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the underlying cause, if any, so errors.Is/errors.As can
// see through a *DecodeError to a sentinel like ErrUnsupportedSchemaMajor.
func (e *DecodeError) Unwrap() error {
	return e.Err
}

// major returns the leading semver major component of a schema version
// string (for example "1" from "1.2.3"). It returns an empty string if the
// version is malformed.
func major(schemaVersion string) string {
	idx := strings.IndexByte(schemaVersion, '.')
	if idx < 0 {
		return ""
	}
	return schemaVersion[:idx]
}

// requiredFields lists, per fact kind, the JSON payload keys that
// decodeAndValidate treats as required — the same set the schema generator
// derives from the struct's pointer/omitempty shape (see aws/v1/resource.go
// and internal/schemagen). Keeping this list beside decodeAndValidate rather
// than deriving it via reflection keeps the missing-field check independent
// of any future encoding/json behavior change.
//
// Two tests keep this list from drifting out of that agreement:
// TestRequiredFieldsMatchStructShape (decode_test.go) recomputes the required
// set from the awsv1.Resource struct by reflection and asserts it equals this
// map's entry, so adding a required struct field without updating this map is
// a test failure rather than a silently unvalidated field; and
// TestAWSResourceSchemaHasNoDrift (schema_gen_test.go) keeps the generated
// schema in lockstep with the struct. Struct → this map, and struct → schema,
// are each independently test-locked.
var requiredFields = map[string][]string{
	FactKindAWSResource: {"account_id", "resource_id", "region", "resource_type"},
}

// decodeAndValidate unmarshals payload into a new T, first checking that
// every JSON key requiredFields[factKind] lists is present in payload (an
// absent key, not merely an empty value). A missing required key returns a
// classified *DecodeError naming the field and the zero value of T, never a
// partially populated struct.
func decodeAndValidate[T any](factKind string, payload map[string]any) (T, error) {
	var zero T

	for _, field := range requiredFields[factKind] {
		if _, ok := payload[field]; !ok {
			return zero, &DecodeError{
				FactKind:       factKind,
				Classification: ClassificationInputInvalid,
				Field:          field,
			}
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return zero, &DecodeError{
			FactKind:       factKind,
			Classification: ClassificationInputInvalid,
			Err:            fmt.Errorf("marshal payload: %w", err),
		}
	}

	var decoded T
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return zero, &DecodeError{
			FactKind:       factKind,
			Classification: ClassificationInputInvalid,
			Err:            fmt.Errorf("unmarshal payload: %w", err),
		}
	}

	return decoded, nil
}

// encodeToPayload marshals a typed payload struct into the map[string]any
// shape Envelope.Payload carries, using the same JSON tags decodeAndValidate
// reads. It is the emit-side counterpart collectors (and this module's own
// round-trip tests) use to build an envelope payload from a typed struct.
func encodeToPayload[T any](value T) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("factschema: marshal payload: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("factschema: unmarshal payload to map: %w", err)
	}

	return payload, nil
}

// DecodeAWSResource decodes env.Payload into the latest awsv1.Resource
// struct for the "aws.resource" fact kind, dispatching on env.SchemaVersion
// major per Contract System v1 §3.2. Callers (reducer handlers) receive
// either the decoded struct or a classified *DecodeError; they must never
// substitute a zero-value struct on error.
func DecodeAWSResource(env Envelope) (awsv1.Resource, error) {
	switch major(env.SchemaVersion) {
	case "1":
		return decodeAndValidate[awsv1.Resource](FactKindAWSResource, env.Payload)
	default:
		return awsv1.Resource{}, &DecodeError{
			FactKind:       FactKindAWSResource,
			Classification: ClassificationInputInvalid,
			Err:            fmt.Errorf("%w: %q", ErrUnsupportedSchemaMajor, env.SchemaVersion),
		}
	}
}

// EncodeAWSResource marshals an awsv1.Resource into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeAWSResource
// for schema-version-1 payloads, used by collectors emitting this fact kind
// and by this module's round-trip tests.
func EncodeAWSResource(resource awsv1.Resource) (map[string]any, error) {
	return encodeToPayload(resource)
}
