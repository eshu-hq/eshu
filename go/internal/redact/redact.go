// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	markerPrefix = "redacted:hmac-sha256:"
	unknown      = "unknown"
)

// Key is deployment-scoped secret material used to produce redaction markers.
type Key struct {
	material []byte
}

// NewKey constructs a redaction key from deployment-scoped secret material.
//
// The material is copied. Blank key material is rejected because unkeyed
// redaction markers are vulnerable to offline guessing for low-entropy values.
func NewKey(material []byte) (Key, error) {
	if len(strings.TrimSpace(string(material))) == 0 {
		return Key{}, fmt.Errorf("redaction key material must not be blank")
	}
	copied := make([]byte, len(material))
	copy(copied, material)
	return Key{material: copied}, nil
}

// IsZero reports whether the key has no material.
func (k Key) IsZero() bool {
	return len(k.material) == 0
}

// Value is a redaction result that can replace a sensitive scalar in facts,
// maps, logs, or spans without retaining raw secret material.
//
// Marker is deterministic for the same key, raw value, reason, and source.
// Reason and Source should be stable classification labels such as
// "sensitive_output" or "aws_db_instance.password"; callers must not place raw
// secret values in either field.
type Value struct {
	// Marker is the deterministic replacement string safe for persistence.
	Marker string `json:"marker"`
	// Reason is the normalized classification explaining why redaction happened.
	Reason string `json:"reason"`
	// Source is the normalized caller-provided field or source label.
	Source string `json:"source"`
}

// String redacts a sensitive string into a deterministic non-secret marker.
//
// Empty strings still produce a marker. Blank reason or source values are
// normalized to "unknown" so callers fail closed instead of passing raw input
// through.
func String(raw string, reason string, source string, key Key) Value {
	return Bytes([]byte(raw), reason, source, key)
}

// Bytes redacts sensitive bytes into a deterministic non-secret marker.
//
// The marker digest includes the raw bytes, normalized reason, and normalized
// source. Only the digest is returned; raw bytes are not retained.
func Bytes(raw []byte, reason string, source string, key Key) Value {
	normalizedReason := normalizeContext(reason)
	normalizedSource := normalizeContext(source)
	return Value{
		Marker: marker(raw, normalizedReason, normalizedSource, key),
		Reason: normalizedReason,
		Source: normalizedSource,
	}
}

// Scalar redacts a sensitive scalar into a deterministic non-secret marker.
//
// Supported scalar inputs are nil, strings, bytes, booleans, integers, unsigned
// integers, floats, and values implementing encoding.TextMarshaler. Unsupported
// values still produce a marker from their type class and context without
// serializing the value, so accidental structs, slices, or maps do not leak.
func Scalar(raw any, reason string, source string, key Key) Value {
	bytes, ok := scalarBytes(raw)
	if !ok {
		bytes = []byte("unsupported")
	}
	return Bytes(bytes, reason, source, key)
}

// IsRedactedValue reports whether v is the JSON round-trip shape of a Value
// produced by String, Bytes, or Scalar: a map[string]any carrying a "marker"
// field that starts with markerPrefix.
//
// Producers embed Value into fact payloads as a plain map (see for example
// the terraform-state collector's redactionMap and the AWS-cloud collector's
// RedactString/ClassifyStackOutput helpers) rather than the typed Value
// struct, because the struct itself never survives a Postgres JSON
// round-trip: callers that later decode an attributes object generically as
// `any` only ever see map[string]any, never Value. IsRedactedValue lets those
// callers recognize a still-redacted leaf after that round-trip and treat it
// as absent, rather than formatting, comparing, or joining on the marker
// string as if it were genuine data.
//
// The check is shape-based, not a textual prefix match on a raw string
// value: only a decoded JSON object with a "marker" field qualifies, so a
// genuine scalar string that merely happens to start with the same text
// (vanishingly unlikely, but not impossible for free-form input) is never
// misclassified as redacted.
//
// The cost of that choice is a deliberate blind spot, and callers must know
// it: this reports false for a BARE marker string persisted on its own,
// without the surrounding "reason"/"source" object. Several collectors do
// exactly that for fingerprint fields — gcpcloud and azurecloud store
// String(...).Marker directly into keys such as "container_name_fingerprint",
// "etag_fingerprint", and redacted tag/DNS values. Those are opaque
// fingerprints meant to be carried and compared as values, not placeholders
// meant to be treated as absent, so answering false for them is correct
// behavior rather than a gap. A caller that needs to recognize one of those
// needs its own check against the producer's shape; do not widen this
// function to a prefix match, which would reintroduce the false positive the
// shape check exists to prevent.
func IsRedactedValue(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	marker, ok := m["marker"].(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(marker, markerPrefix)
}

func marker(raw []byte, reason string, source string, key Key) string {
	sum := hmac.New(sha256.New, key.material)
	writeField(sum, []byte("redact.v1"))
	writeField(sum, []byte(reason))
	writeField(sum, []byte(source))
	writeField(sum, raw)
	return markerPrefix + hex.EncodeToString(sum.Sum(nil))
}

func normalizeContext(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return unknown
	}
	return value
}

type textMarshaler interface {
	MarshalText() ([]byte, error)
}

func scalarBytes(raw any) ([]byte, bool) {
	switch typed := raw.(type) {
	case nil:
		return []byte(""), true
	case string:
		return []byte(typed), true
	case []byte:
		return typed, true
	case bool:
		return []byte(strconv.FormatBool(typed)), true
	case int:
		return []byte(strconv.FormatInt(int64(typed), 10)), true
	case int8:
		return []byte(strconv.FormatInt(int64(typed), 10)), true
	case int16:
		return []byte(strconv.FormatInt(int64(typed), 10)), true
	case int32:
		return []byte(strconv.FormatInt(int64(typed), 10)), true
	case int64:
		return []byte(strconv.FormatInt(typed, 10)), true
	case uint:
		return []byte(strconv.FormatUint(uint64(typed), 10)), true
	case uint8:
		return []byte(strconv.FormatUint(uint64(typed), 10)), true
	case uint16:
		return []byte(strconv.FormatUint(uint64(typed), 10)), true
	case uint32:
		return []byte(strconv.FormatUint(uint64(typed), 10)), true
	case uint64:
		return []byte(strconv.FormatUint(typed, 10)), true
	case float32:
		return []byte(strconv.FormatFloat(float64(typed), 'g', -1, 32)), true
	case float64:
		if math.IsNaN(typed) {
			return []byte("NaN"), true
		}
		return []byte(strconv.FormatFloat(typed, 'g', -1, 64)), true
	case json.Number:
		return []byte(typed.String()), true
	case textMarshaler:
		encoded, err := typed.MarshalText()
		return encoded, err == nil
	default:
		return nil, false
	}
}

type fieldWriter interface {
	Write([]byte) (int, error)
}

func writeField(writer fieldWriter, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}
