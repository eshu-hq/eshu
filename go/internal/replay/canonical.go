// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Fixed sentinels for the canonical form. Volatile fields collapse to these so a
// re-record does not churn the fixture, and configured secrets collapse to the
// redaction sentinel so a recorded fixture never carries live credentials.
const (
	// SentinelObservedAt replaces observed_at timestamps. It is a valid RFC3339
	// instant so a canonicalized cassette still parses as a timestamped document.
	SentinelObservedAt = "2000-01-01T00:00:00Z"
	// SentinelGenerationID replaces run-specific generation identifiers.
	SentinelGenerationID = "canonical-generation"
	// RedactedSentinel replaces the value of any configured secret key.
	RedactedSentinel = "<redacted>"
	// defaultIndent is the JSON indent used when CanonicalOptions.Indent is empty.
	defaultIndent = "  "
)

// CanonicalOptions configures the canonical serialization of recorded replay
// data. The zero value performs no normalization beyond sorting object keys;
// use DefaultCanonicalOptions for the fact-envelope defaults. Every field is
// keyed by JSON object key name so the core stays flavor-agnostic: a cassette,
// a parser fixture, or an input tape passes its own keys without the core
// importing any flavor.
type CanonicalOptions struct {
	// VolatileKeys maps an object key to the fixed sentinel its value is replaced
	// with wherever that key appears in the document tree.
	VolatileKeys map[string]string
	// SecretKeys maps an object key to the redaction sentinel its value is
	// replaced with wherever that key appears. Matching is by key name at any
	// depth so a secret cannot leak by being nested differently than expected.
	SecretKeys map[string]string
	// SortArrays maps an object key holding an array to the string field each
	// element is stably ordered by. Elements that are not objects, or that lack
	// the field, sort ahead of those that have it; ties break on the element's
	// canonical bytes so ordering is total and deterministic. Ordering is keyed
	// by the parent object key, so a document whose root is a bare array is not
	// reordered; record replay documents as objects (the cassette format is
	// {schema_version, scopes: [...]}) so their arrays are orderable.
	SortArrays map[string]string
	// Indent is the JSON indent string. Empty means two spaces.
	Indent string
}

// DefaultCanonicalOptions returns the canonical defaults for fact-envelope
// recordings (cassettes): observed_at and generation_id collapse to fixed
// sentinels, scopes order by scope_id, and facts order by stable_fact_key. No
// secret keys are configured by default; a recorder adds them with
// WithRedactedKeys for the fields its source is known to carry.
func DefaultCanonicalOptions() CanonicalOptions {
	return CanonicalOptions{
		VolatileKeys: map[string]string{
			"observed_at":   SentinelObservedAt,
			"generation_id": SentinelGenerationID,
		},
		SecretKeys: map[string]string{},
		SortArrays: map[string]string{
			"scopes": "scope_id",
			"facts":  "stable_fact_key",
		},
		Indent: defaultIndent,
	}
}

// WithRedactedKeys returns a copy of o with each named object key marked for
// secret redaction using RedactedSentinel. The receiver is not mutated, so a
// shared DefaultCanonicalOptions value is safe to extend per call site.
func (o CanonicalOptions) WithRedactedKeys(keys ...string) CanonicalOptions {
	secrets := make(map[string]string, len(o.SecretKeys)+len(keys))
	for k, v := range o.SecretKeys {
		secrets[k] = v
	}
	for _, k := range keys {
		secrets[k] = RedactedSentinel
	}
	o.SecretKeys = secrets
	return o
}

// Canonicalize decodes data as JSON and returns its canonical serialization
// under opts. It is the shared core every replay recorder and validator uses so
// recorded fixtures are stable, reviewable, and byte-identical when re-derived
// from equivalent input. Canonicalize is idempotent: Canonicalize(Canonicalize(x))
// equals Canonicalize(x).
func Canonicalize(data []byte, opts CanonicalOptions) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	// Preserve numeric literals so a re-record does not churn integers or
	// fractional values through a float64 round-trip.
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode replay document: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("decode replay document: unexpected trailing content")
	}
	return CanonicalizeValue(value, opts)
}

// CanonicalizeValue returns the canonical serialization of an already-decoded
// JSON value. Callers that already hold a decoded tree (a recorder building the
// document in memory) use this to avoid a round-trip through bytes. The value
// must contain only JSON-decoded types (map[string]any, []any, json.Number,
// string, bool, nil); feeding a non-marshalable value (a chan, func, or raw
// numeric type) returns a marshal error rather than a partial document.
func CanonicalizeValue(value any, opts CanonicalOptions) ([]byte, error) {
	indent := opts.Indent
	if indent == "" {
		indent = defaultIndent
	}
	transformed := transform(value, opts)
	out, err := marshalIndent(transformed, indent)
	if err != nil {
		return nil, err
	}
	// A trailing newline keeps the fixture POSIX-friendly; the decoder ignores
	// it, so re-canonicalizing appends exactly one newline again (idempotent).
	return append(out, '\n'), nil
}

// transform walks a decoded JSON value, applying volatile/secret normalization
// to object values and stable ordering to configured arrays. Object keys are
// emitted in sorted order by json.Marshal, so no explicit key sort is needed.
func transform(value any, opts CanonicalOptions) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = transformChild(key, child, opts)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = transform(typed[i], opts)
		}
		return out
	default:
		return typed
	}
}

// transformChild resolves one object key/value: secret redaction and volatile
// normalization win over recursion (the value is replaced, not descended into);
// otherwise the child is transformed and, when the key names a configured
// sortable array, stably ordered.
func transformChild(key string, child any, opts CanonicalOptions) any {
	if sentinel, ok := opts.SecretKeys[key]; ok {
		return sentinel
	}
	if sentinel, ok := opts.VolatileKeys[key]; ok {
		return sentinel
	}
	transformed := transform(child, opts)
	if field, ok := opts.SortArrays[key]; ok {
		if arr, isArr := transformed.([]any); isArr {
			sortArray(arr, field)
		}
	}
	return transformed
}

// sortArray stably orders arr by each element's string field, breaking ties on
// the element's canonical bytes so the order is total regardless of input
// order. It sorts in place; the slice is freshly allocated by transform.
func sortArray(arr []any, field string) {
	keys := make([]string, len(arr))
	tiebreak := make([]string, len(arr))
	for i, elem := range arr {
		keys[i] = elementField(elem, field)
		if b, err := marshalIndent(elem, defaultIndent); err == nil {
			tiebreak[i] = string(b)
		}
	}
	idx := make([]int, len(arr))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		if keys[ia] != keys[ib] {
			return keys[ia] < keys[ib]
		}
		return tiebreak[ia] < tiebreak[ib]
	})
	sorted := make([]any, len(arr))
	for i, j := range idx {
		sorted[i] = arr[j]
	}
	copy(arr, sorted)
}

// elementField returns the string value of field on a JSON object element, or
// the empty string when the element is not an object or lacks a string field.
func elementField(elem any, field string) string {
	obj, ok := elem.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := obj[field].(string)
	return s
}

// marshalIndent marshals v with sorted object keys and the given indent,
// without HTML escaping so canonical bytes match the human-authored fixtures
// reviewers read.
func marshalIndent(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("marshal canonical replay document: %w", err)
	}
	// json.Encoder.Encode appends a newline; trim it so callers control the
	// trailing byte and nested marshals (the sort tiebreaker) stay newline-free.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
