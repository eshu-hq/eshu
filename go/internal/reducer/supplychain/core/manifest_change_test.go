// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

// TestManifestChangeScopeFlipAtTheSameVersionIsAChange is the #9874 F5
// regression: a dependency that moves from a development to a runtime scope
// at the same version changes the finding (the consumption carries
// DependencyScope), so its holders must be reopened.
func TestManifestChangeScopeFlipAtTheSameVersionIsAChange(t *testing.T) {
	t.Parallel()

	withScope := func(factID, scope string) facts.Envelope {
		row := depRow(factID, "package-lock.json", "lodash", "4.17.21")
		row.Payload["entity_metadata"].(map[string]any)["dependency_scope"] = scope
		return row
	}
	change := changedManifestDependencyKeys(ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Current:    ManifestDependencyView{Rows: []facts.Envelope{withScope("new", "runtime")}},
		HasPrior:   true,
		Prior:      ManifestDependencyView{Rows: []facts.Envelope{withScope("old", "development")}},
	})
	if change.packages != 1 {
		t.Fatalf("changed packages = %d, want 1 for a scope flip at the same version", change.packages)
	}
}

// TestManifestChangeIgnoresFactIdentityAndObservationTime proves the two
// fields a re-index always rewrites do not make an unchanged pin look changed.
func TestManifestChangeIgnoresFactIdentityAndObservationTime(t *testing.T) {
	t.Parallel()

	old := depRow("old", "package-lock.json", "lodash", "4.17.21")
	old.ObservedAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	current := depRow("new", "package-lock.json", "lodash", "4.17.21")
	current.ObservedAt = time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	change := changedManifestDependencyKeys(ManifestDependencyGeneration{
		Activation: ManifestGenerationActive,
		Current:    ManifestDependencyView{Rows: []facts.Envelope{current}},
		HasPrior:   true,
		Prior:      ManifestDependencyView{Rows: []facts.Envelope{old}},
	})
	if change.packages != 0 {
		t.Fatalf("changed packages = %d, want 0 when only the fact id and time differ", change.packages)
	}
}

// TestDependencySignatureCoversEveryFindingField enumerates
// correlation.PackageManifestDependency by reflection: changing any field
// except FactID and ObservedAt must change the signature, so a field added to
// the struct later cannot be silently left out of change detection.
func TestDependencySignatureCoversEveryFindingField(t *testing.T) {
	t.Parallel()

	ignored := map[string]bool{"FactID": true, "ObservedAt": true}
	base := correlation.PackageManifestDependency{}
	baseSignature := dependencySignature(base)
	structType := reflect.TypeOf(base)
	for i := range structType.NumField() {
		field := structType.Field(i)
		changed := base
		value := reflect.ValueOf(&changed).Elem().Field(i)
		switch value.Kind() {
		case reflect.String:
			value.SetString("x")
		case reflect.Bool:
			value.SetBool(true)
		case reflect.Int:
			value.SetInt(7)
		case reflect.Slice:
			value.Set(reflect.ValueOf([]string{"x"}))
		case reflect.Pointer:
			truth := true
			value.Set(reflect.ValueOf(&truth))
		case reflect.Struct:
			value.Set(reflect.ValueOf(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)))
		default:
			t.Fatalf("field %s has kind %s the test cannot vary; extend the test", field.Name, value.Kind())
		}
		differs := dependencySignature(changed) != baseSignature
		if ignored[field.Name] && differs {
			t.Fatalf("field %s changed the signature; it must not", field.Name)
		}
		if !ignored[field.Name] && !differs {
			t.Fatalf("field %s is not part of the dependency signature", field.Name)
		}
	}
}
