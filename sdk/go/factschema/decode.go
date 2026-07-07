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

// DecodeOption configures an opt-in behavior for the kind-specific Decode*
// functions that accept variadic options (for example
// DecodeGCPCloudRelationship). Options are purely additive: a Decode call
// passing no options behaves exactly as it did before options existed, so an
// existing caller is never affected.
type DecodeOption func(*decodeConfig)

// decodeConfig holds the resolved DecodeOption set for one Decode call.
type decodeConfig struct {
	// skipAttributesRemainder, when set by WithoutAttributesRemainder, tells
	// the decode to leave a polymorphic struct's Attributes pass-through field
	// at its zero value (nil) rather than rebuilding the remainder map of every
	// non-named payload key. Named struct fields decode identically regardless.
	skipAttributesRemainder bool
}

// resolveDecodeConfig folds a DecodeOption slice into a decodeConfig. A nil
// option is ignored so a caller building an option slice conditionally never
// panics.
func resolveDecodeConfig(opts []DecodeOption) decodeConfig {
	var cfg decodeConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// WithoutAttributesRemainder opts a decode out of building the polymorphic
// Attributes pass-through remainder — the fresh map[string]any of every
// payload key with no named struct field, which the decode otherwise rebuilds
// on every call. It is for named-field-only hot callers that read only named
// struct fields and never touch .Attributes (issue #4865): for those the
// remainder map is allocated and immediately discarded, and on a
// wide-Attributes payload that rebuild dominates the decode cost.
//
// With this option the returned struct's named fields are identical to a
// default decode's; only the Attributes field differs, left nil instead of
// populated. A caller that reads .Attributes MUST NOT pass this option. A
// struct type that declares no Attributes field is unaffected either way.
func WithoutAttributesRemainder() DecodeOption {
	return func(c *decodeConfig) { c.skipAttributesRemainder = true }
}

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

// decodeAndValidate unmarshals payload into a new T, first checking that every
// required JSON key T declares is present in payload with a non-null value. The
// required set is derived reflectively from T's own struct tags by
// requiredPayloadKeys (fields.go) — the single source of truth, shared with the
// schema generator's rule so the decode validator and the generated JSON Schema
// cannot disagree about which fields are required. There is no per-kind map to
// keep in sync: a new fact kind's required set is exactly what its struct
// declares.
//
// An absent key, or a key present with an explicit JSON null (Go nil in the
// map), returns a classified *DecodeError naming the field and the zero value
// of T, never a partially populated struct. A present, non-nil but empty value
// (for example the empty string) is a valid observed value and decodes
// normally.
//
// factKind is used only for error attribution (DecodeError.FactKind); the
// required set comes from T, not from factKind.
//
// opts carries any caller-supplied DecodeOption (for example
// WithoutAttributesRemainder). Passing no option preserves the historical
// decode exactly, so every existing caller is unaffected.
func decodeAndValidate[T any](factKind string, payload map[string]any, opts ...DecodeOption) (T, error) {
	var zero T

	for _, field := range requiredPayloadKeys[T]() {
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
	if err := decodeMapIntoWith(payload, &decoded, resolveDecodeConfig(opts)); err != nil {
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
func decodeLatestMajor[T any](factKind string, env Envelope, opts ...DecodeOption) (T, error) {
	var zero T
	switch major(env.SchemaVersion) {
	case "1":
		return decodeAndValidate[T](factKind, env.Payload, opts...)
	default:
		return zero, &DecodeError{
			FactKind:       factKind,
			Classification: ClassificationInputInvalid,
			Err:            fmt.Errorf("%w: %q", ErrUnsupportedSchemaMajor, env.SchemaVersion),
		}
	}
}
