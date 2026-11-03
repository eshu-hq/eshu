// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
)

// filledImpactPayload returns a typed payload with every field populated, so a
// per-field mutation always has a value to change.
func filledImpactPayload(t *testing.T) reducerderivedv1.SupplyChainImpactFinding {
	t.Helper()
	var payload reducerderivedv1.SupplyChainImpactFinding
	fillValue(reflect.ValueOf(&payload).Elem())
	payload.Provenance = map[string]any{"selected_severity_label": "high", "other": "x"}
	payload.Suppression = map[string]any{"expires_at": "2027-01-01T00:00:00Z"}
	return payload
}

func fillValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("v")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64:
		v.SetInt(3)
	case reflect.Float64:
		v.SetFloat(7.5)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillValue(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillValue(v.Index(0))
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		key := reflect.New(v.Type().Key()).Elem()
		key.SetString("k")
		elem := reflect.New(v.Type().Elem()).Elem()
		if elem.Kind() == reflect.String {
			elem.SetString("v")
		}
		v.SetMapIndex(key, elem)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fillValue(v.Field(i))
		}
	}
}

// digestAfterMutating round-trips the payload through JSON, mutates one
// top-level key, and returns the content digest of the mutated payload.
func digestAfterMutating(t *testing.T, base reducerderivedv1.SupplyChainImpactFinding, key string) string {
	t.Helper()
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	switch value := fields[key].(type) {
	case string:
		fields[key] = value + "-changed"
	case bool:
		fields[key] = !value
	case float64:
		fields[key] = value + 1
	case []any:
		if len(value) > 0 {
			if _, isMap := value[0].(map[string]any); isMap {
				fields[key] = append(value, map[string]any{"changed": "changed"})
				break
			}
		}
		fields[key] = append(value, "changed")
	case map[string]any:
		value["changed"] = "changed"
	default:
		t.Fatalf("field %q has unmutable value %#v", key, fields[key])
	}
	mutated, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal mutated payload: %v", err)
	}
	var out reducerderivedv1.SupplyChainImpactFinding
	if err := json.Unmarshal(mutated, &out); err != nil {
		t.Fatalf("decode mutated payload: %v", err)
	}
	return contentDigestOf(out)
}

// TestContentDigestCoversEveryPayloadFieldExceptTheExcludedOnes is the digest
// side of the #9805 soundness argument. The write-on-change capture filter
// skips a row whose digest equals the stored digest, which is only sound when
// no payload field the winners select reads is outside the digest. This test
// mutates every top-level payload field and requires the digest to change for
// all of them except the four the digest excludes by design. The matching
// select-side guard lives in the postgres package.
func TestContentDigestCoversEveryPayloadFieldExceptTheExcludedOnes(t *testing.T) {
	t.Parallel()

	base := filledImpactPayload(t)
	baseDigest := contentDigestOf(base)
	excluded := map[string]bool{}
	for _, key := range DigestExcludedPayloadKeys() {
		excluded[key] = true
	}
	typ := reflect.TypeOf(base)
	covered := 0
	for i := 0; i < typ.NumField(); i++ {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		got := digestAfterMutating(t, base, key)
		if excluded[key] {
			if got != baseDigest {
				t.Errorf("excluded field %q changed the digest; DigestExcludedPayloadKeys is out of date", key)
			}
			continue
		}
		covered++
		if got == baseDigest {
			t.Errorf("mutating payload field %q left the digest unchanged: a rewrite that changes it would be skipped by the capture filter", key)
		}
	}
	if covered == 0 {
		t.Fatal("no payload field was checked")
	}
}

// TestContentDigestCoversNestedWinnerInputs covers the two nested values the
// winners select reads through a path: the selected severity label inside
// provenance and the suppression expiry. The digest must change with them and
// must ignore only the content digest the provenance carries itself.
func TestContentDigestCoversNestedWinnerInputs(t *testing.T) {
	t.Parallel()

	base := filledImpactPayload(t)
	baseDigest := contentDigestOf(base)

	label := filledImpactPayload(t)
	label.Provenance["selected_severity_label"] = "critical"
	if contentDigestOf(label) == baseDigest {
		t.Error("provenance.selected_severity_label does not move the digest")
	}
	expiry := filledImpactPayload(t)
	expiry.Suppression["expires_at"] = "2028-01-01T00:00:00Z"
	if contentDigestOf(expiry) == baseDigest {
		t.Error("suppression.expires_at does not move the digest")
	}
	withDigest := filledImpactPayload(t)
	withDigest.Provenance[contentDigestProvenanceKey] = "an-older-digest"
	if contentDigestOf(withDigest) != baseDigest {
		t.Error("the digest a payload carries about itself must not feed the digest")
	}
}
