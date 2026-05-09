package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
