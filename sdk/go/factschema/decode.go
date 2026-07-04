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
)

// Fact kind identifiers this module knows how to decode. A fact kind string
// is namespaced and stable across schema-version majors; only the payload
// shape changes between majors, handled by the switch inside each
// kind-specific Decode function (Contract System v1 §3.2).
//
// Every value is the exact wire fact-kind string the collector emits and the
// reducer loads (go/internal/facts.*FactKind). The contracts module cannot
// import go/internal/facts, so the values are duplicated here; the reducer-side
// drift lock TestFactSchemaKindsMatchWireFactKinds asserts each stays byte-equal
// to its facts.*FactKind counterpart so a constant can never silently diverge
// from its wire kind.
const (
	// FactKindAWSResource is the "aws_resource" fact kind.
	FactKindAWSResource = "aws_resource"
	// FactKindAWSRelationship is the "aws_relationship" fact kind.
	FactKindAWSRelationship = "aws_relationship"
	// FactKindAWSSecurityGroupRule is the "aws_security_group_rule" fact kind.
	FactKindAWSSecurityGroupRule = "aws_security_group_rule"
	// FactKindEC2InstancePosture is the "ec2_instance_posture" fact kind.
	FactKindEC2InstancePosture = "ec2_instance_posture"
	// FactKindS3BucketPosture is the "s3_bucket_posture" fact kind.
	FactKindS3BucketPosture = "s3_bucket_posture"
	// FactKindAWSIAMPermission is the "aws_iam_permission" fact kind.
	FactKindAWSIAMPermission = "aws_iam_permission"
	// FactKindAWSResourcePolicyPermission is the
	// "aws_resource_policy_permission" fact kind.
	FactKindAWSResourcePolicyPermission = "aws_resource_policy_permission"
	// FactKindAWSIAMPrincipal is the "aws_iam_principal" fact kind.
	FactKindAWSIAMPrincipal = "aws_iam_principal"
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
	// Field names the required JSON payload key that was missing or present
	// as an explicit null; Error formats it as a missing-required-field
	// message. Empty when the error is not attributable to a single field
	// (for example an unsupported schema major).
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
// derives from each struct's pointer/omitempty shape (see aws/v1, iam/v1, and
// internal/schemagen). Keeping this list beside decodeAndValidate rather than
// deriving it via reflection keeps the missing-field check independent of any
// future encoding/json behavior change.
//
// The map is populated per family, not as one central literal: each
// decode_<family>.go registers its own kinds through registerRequiredFields in
// an init function. This keeps a new fact kind's registration local to its
// family file, so the parallel family wave that copies this template adds kinds
// without editing a shared central map (no merge conflicts on one hot literal).
//
// Two families of tests keep this map from drifting out of agreement with the
// structs: TestRequiredFieldsMatchStructShape (decode_test.go) recomputes the
// required set from each typed struct by reflection and asserts it equals this
// map's entry, so adding a required struct field without registering it is a test
// failure rather than a silently unvalidated field; and the schema drift tests
// (schema_gen_test.go) keep the generated schemas in lockstep with the structs.
// Struct → this map, and struct → schema, are each independently test-locked.
//
// The required set for every kind is grounded in its collector emitter's
// non-empty validation (the awscloud / secretsiam New*Envelope builders), never
// in a single reducer handler's defensive skip: a field the emitter always
// validates non-empty is required, and an either-or identity (instance_id OR
// arn; bucket_arn OR bucket_name) leaves BOTH sides optional so a fact
// identified by only one side is not dead-lettered.
var requiredFields = map[string][]string{}

// registerRequiredFields records the required payload keys for one fact kind. It
// is called from each decode_<family>.go's init function so a family owns its
// own registration. It panics on a duplicate registration for the same fact kind
// because that is a programming error (two files claiming one kind) that must
// surface at package load, not silently let one registration win.
func registerRequiredFields(factKind string, fields ...string) {
	if _, exists := requiredFields[factKind]; exists {
		panic("factschema: duplicate required-fields registration for fact kind " + strconv.Quote(factKind))
	}
	requiredFields[factKind] = fields
}

// decodeAndValidate unmarshals payload into a new T, first checking that
// every JSON key requiredFields[factKind] lists is present in payload with a
// non-null value. An absent key, or a key present with an explicit JSON null
// (Go nil in the map), returns a classified *DecodeError naming the field and
// the zero value of T, never a partially populated struct. A present, non-nil
// but empty value (for example the empty string) is a valid observed value
// and decodes normally.
func decodeAndValidate[T any](factKind string, payload map[string]any) (T, error) {
	var zero T

	for _, field := range requiredFields[factKind] {
		// Reject both an absent key and an explicit JSON null (Go nil in
		// the decoded map): null would otherwise pass a presence-only
		// check and json.Unmarshal would turn it into a zero value with no
		// error, the silent-zero-value identity this validation exists to
		// prevent.
		if value, ok := payload[field]; !ok || value == nil {
			return zero, &DecodeError{
				FactKind:       factKind,
				Classification: ClassificationInputInvalid,
				Field:          field,
			}
		}
	}

	var decoded T
	if err := decodeMapInto(payload, &decoded); err != nil {
		return zero, &DecodeError{
			FactKind:       factKind,
			Classification: ClassificationInputInvalid,
			Err:            fmt.Errorf("decode payload: %w", err),
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

// decodeLatestMajor is the shared dispatch body every kind-specific Decode
// function delegates to: it validates the schema-version major is supported
// (only major 1 today) and decodes through decodeAndValidate, returning a
// classified *DecodeError for an unsupported major rather than a best-effort
// decode. When a payload majors, this is where the version shim (design §3.2)
// is added — the reducer keeps calling the same Decode* function and codes
// against the latest struct only.
func decodeLatestMajor[T any](factKind string, env Envelope) (T, error) {
	var zero T
	switch major(env.SchemaVersion) {
	case "1":
		return decodeAndValidate[T](factKind, env.Payload)
	default:
		return zero, &DecodeError{
			FactKind:       factKind,
			Classification: ClassificationInputInvalid,
			Err:            fmt.Errorf("%w: %q", ErrUnsupportedSchemaMajor, env.SchemaVersion),
		}
	}
}
