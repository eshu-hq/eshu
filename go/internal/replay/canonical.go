// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// Fixed sentinels for the canonical form. Volatile fields collapse to these so a
// re-record does not churn the fixture, and configured secrets collapse to the
// redaction sentinel so a recorded fixture never carries live credentials.
const (
	// SentinelObservedAt replaces observed_at timestamps. It is a valid RFC3339
	// instant so a canonicalized cassette still parses as a timestamped document.
	SentinelObservedAt = "2000-01-01T00:00:00Z"
	// GenerationIDPrefix prefixes the deterministic per-scope generation id that
	// replaces a run-specific generation_id. See DerivedKeys: the suffix is a
	// stable hash of the scope's own identity, so the value is unique per scope
	// (generation_id is a primary key) yet does not churn on re-record.
	GenerationIDPrefix = "canonical-generation"
	// RedactedSentinel replaces the value of any configured secret key.
	RedactedSentinel = "<redacted>"
	// defaultIndent is the JSON indent used when CanonicalOptions.Indent is empty.
	defaultIndent = "  "
	// derivedHashLen is the hex width of the stable suffix on a derived value.
	derivedHashLen = 16
)

// CanonicalOptions configures the canonical serialization of recorded replay
// data. The zero value performs no normalization beyond sorting object keys;
// use DefaultCanonicalOptions for the fact-envelope defaults. Every field is
// keyed by JSON object key name so the core stays flavor-agnostic: a cassette,
// a parser fixture, or an input tape passes its own keys without the core
// importing any flavor.
type CanonicalOptions struct {
	// VolatileKeys maps an object key to the fixed sentinel its value is replaced
	// with wherever that key appears in the document tree. Use this only for
	// fields that are genuinely run-specific AND not required to stay unique; a
	// field that is a primary key (generation_id) must use DerivedKeys instead,
	// or two records would collapse to the same id and collide on commit.
	VolatileKeys map[string]string
	// DerivedKeys maps an object key whose value is run-specific but must stay
	// unique to the sibling key whose (stable) value seeds its deterministic
	// replacement. The value becomes "<key-prefix>-<hash(sibling value)>", which
	// is stable across re-records (the sibling is stable) yet unique per distinct
	// sibling. The canonical default derives generation_id from scope_id so a
	// multi-scope cassette keeps one generation id per scope — generation_id is
	// the scope_generations primary key, so a single fixed sentinel would make
	// later scope commits collide with or overwrite earlier scope truth. Note:
	// this keys generation_id off scope identity, so a cassette that records
	// multiple generations of the SAME scope_id (delta/multi-generation, a future
	// flavor) needs an extended derivation; the current corpus is one generation
	// per scope.
	DerivedKeys map[string]string
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
	// OpaqueValueKeys names object keys whose value subtree is opaque,
	// collector-emitted data — a fact payload. Within such a subtree keys are
	// still sorted for determinism and configured secrets are still redacted, but
	// VolatileKeys and DerivedKeys normalization is NOT applied: a payload field
	// that happens to be named observed_at or generation_id must stay exactly as
	// the collector emitted it. Without this, normalizing by key name at any depth
	// would rewrite real payload values and break the recorder's verbatim-payload
	// contract. Once a subtree is entered it stays opaque all the way down.
	OpaqueValueKeys map[string]struct{}
	// Indent is the JSON indent string. Empty means two spaces.
	Indent string
}

// withoutVolatileDerived returns a copy of o with volatile and derived
// normalization disabled (secret redaction and array sorting are retained). It
// is applied when descending into an OpaqueValueKeys subtree so collector
// payloads are preserved verbatim.
func (o CanonicalOptions) withoutVolatileDerived() CanonicalOptions {
	o.VolatileKeys = nil
	o.DerivedKeys = nil
	return o
}

// DefaultCanonicalOptions returns the canonical defaults for fact-envelope
// recordings (cassettes): observed_at collapses to a fixed sentinel,
// generation_id is derived deterministically from each scope's scope_id (so it
// stays unique per scope without churning on re-record), scopes order by
// scope_id, and facts order by stable_fact_key. No secret keys are configured by
// default; a recorder adds them with WithRedactedKeys for the fields its source
// is known to carry.
func DefaultCanonicalOptions() CanonicalOptions {
	return CanonicalOptions{
		VolatileKeys: map[string]string{
			"observed_at": SentinelObservedAt,
		},
		DerivedKeys: map[string]string{
			"generation_id": "scope_id",
		},
		SecretKeys: map[string]string{},
		SortArrays: map[string]string{
			"scopes": "scope_id",
			"facts":  "stable_fact_key",
		},
		OpaqueValueKeys: map[string]struct{}{
			// A fact's payload is opaque collector data: preserve it verbatim
			// (never collapse a payload-level observed_at / generation_id).
			"payload": {},
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
	// Require EOF after the first value. dec.More reports false outside an array
	// or object, so a stray trailing token (a second value, or a bare `]`/`}`
	// after a corrupted recording) would slip through More; reading the next
	// token and requiring io.EOF rejects it instead of silently dropping the tail.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode replay document: unexpected trailing content after JSON value")
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
			out[key] = transformChild(typed, key, child, opts)
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

// transformChild resolves one object key/value within parent: secret redaction,
// deterministic derivation, and volatile normalization each replace the value
// outright (no descent); otherwise the child is transformed and, when the key
// names a configured sortable array, stably ordered. Derivation reads a sibling
// from parent, so it must run on the original (pre-transform) parent map.
func transformChild(parent map[string]any, key string, child any, opts CanonicalOptions) any {
	if sentinel, ok := opts.SecretKeys[key]; ok {
		return sentinel
	}
	if sibling, ok := opts.DerivedKeys[key]; ok {
		return deriveValue(key, parent[sibling])
	}
	if sentinel, ok := opts.VolatileKeys[key]; ok {
		return sentinel
	}
	childOpts := opts
	if _, opaque := opts.OpaqueValueKeys[key]; opaque {
		childOpts = opts.withoutVolatileDerived()
	}
	transformed := transform(child, childOpts)
	if field, ok := opts.SortArrays[key]; ok {
		if arr, isArr := transformed.([]any); isArr {
			sortArray(arr, field)
		}
	}
	return transformed
}

// deriveValue returns a deterministic replacement for a run-specific key whose
// value must stay unique. The result is "<prefix>-<hash(seed)>", stable across
// re-records because seed is a stable sibling (scope_id) rather than the
// run-specific value being replaced — so re-canonicalizing yields the same
// value (idempotent) while distinct seeds yield distinct values (no collision
// on a primary-key field). A missing or non-string seed falls back to the bare
// prefix; that only collapses uniqueness for malformed input that lacks the
// seed key entirely.
func deriveValue(key string, seed any) string {
	prefix := GenerationIDPrefix
	if key != "generation_id" {
		prefix = "canonical-" + key
	}
	s, ok := seed.(string)
	if !ok || s == "" {
		return prefix
	}
	sum := sha256.Sum256([]byte(s))
	return prefix + "-" + hex.EncodeToString(sum[:])[:derivedHashLen]
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
